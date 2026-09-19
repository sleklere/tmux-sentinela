package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Pane is one tmux pane as reported by list-panes -a.
type Pane struct {
	ID          string
	PID         int
	Command     string
	Title       string
	Session     string
	WindowID    string
	WindowIndex int
	WindowName  string
	PaneIndex   int
	Path        string
	Visible     bool // active pane of the active window of an attached session
	Current     bool // in the active window of an attached session
	Sidebar     bool // pane running our sidebar
	Width       int
	WindowWidth int
	Zoomed      bool
}

func tmux(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	// Only newlines: a trailing tab is an empty last field, not noise.
	return strings.TrimRight(string(out), "\n"), err
}

const paneFormat = "#{pane_id}\t#{pane_pid}\t#{session_name}\t#{window_id}\t#{window_index}\t#{window_name}\t#{pane_index}\t#{pane_current_path}\t#{pane_active}\t#{window_active}\t#{session_attached}\t#{@sentinela_sidebar}\t#{pane_width}\t#{window_width}\t#{window_zoomed_flag}\t#{pane_current_command}\t#{pane_title}"

// listPanes returns every pane of every session, in tmux order.
func listPanes() ([]Pane, error) {
	out, err := tmux("list-panes", "-a", "-F", paneFormat)
	if err != nil {
		return nil, err
	}
	var panes []Pane
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 17 {
			continue
		}
		pid, _ := strconv.Atoi(f[1])
		wi, _ := strconv.Atoi(f[4])
		pi, _ := strconv.Atoi(f[6])
		attached, _ := strconv.Atoi(f[10])
		width, _ := strconv.Atoi(f[12])
		windowWidth, _ := strconv.Atoi(f[13])
		panes = append(panes, Pane{
			ID: f[0], PID: pid, Session: f[2], WindowID: f[3], WindowIndex: wi,
			WindowName: f[5], PaneIndex: pi, Path: f[7],
			Visible: f[8] == "1" && f[9] == "1" && attached > 0,
			Current: f[9] == "1" && attached > 0,
			Sidebar: f[11] != "",
			Width:   width, WindowWidth: windowWidth, Zoomed: f[14] == "1", Command: f[15], Title: f[16],
		})
	}
	return panes, nil
}

// globalOptions returns every global user option (@name → value).
func globalOptions() map[string]string {
	opts := map[string]string{}
	out, err := tmux("show-options", "-g")
	if err != nil {
		return opts
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "@") {
			continue
		}
		name, val, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		opts[name] = strings.Trim(val, `"`)
	}
	return opts
}

// autocreateEnabled returns whether @sentinela_autocreate is on (default: on).
func autocreateEnabled() bool {
	val := globalOptions()["@sentinela_autocreate"]
	return val != "off"
}

// userClosedOption returns the window option name for the user-closed marker.
const userClosedOption = "@sentinela_user_closed"

// isUserClosed returns whether the user deliberately closed the sidebar in this window.
func isUserClosed(windowID string) bool {
	val, err := tmux("show-option", "-wqv", "-t", windowID, userClosedOption)
	if err != nil {
		return false
	}
	return val == "1"
}

// setUserClosed sets or clears the user-closed marker for a window.
func setUserClosed(windowID string, closed bool) {
	val := "0"
	if closed {
		val = "1"
	}
	tmux("set-option", "-w", "-t", windowID, userClosedOption, val)
}

// isWindowZoomed returns whether the given window is currently zoomed.
func isWindowZoomed(windowID string) bool {
	val, err := tmux("display-message", "-p", "-t", windowID, "#{window_zoomed_flag}")
	if err != nil {
		return false
	}
	return val == "1"
}

// jumpTo focuses a pane, switching session and window as needed.
func jumpTo(paneID string) error {
	for _, args := range [][]string{
		{"switch-client", "-t", paneID},
		{"select-window", "-t", paneID},
		{"select-pane", "-t", paneID},
	} {
		if _, err := tmux(args...); err != nil {
			return fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

// ensureLockPath returns the path to the ensure lock file for the current tmux server.
func ensureLockPath() string {
	socket, _ := tmux("display-message", "-p", "#{socket_path}")
	if socket == "" {
		return ""
	}
	// Hash the socket path for a filesystem-safe name
	sum := sha256.Sum256([]byte(socket))
	return filepath.Join(os.TempDir(), fmt.Sprintf("tmux-sentinela-ensure-%x.lock", sum[:4]))
}

// ensureLock acquires a per-server lock for ensure operations.
// Uses atomic directory creation as a mutex. Returns true if lock acquired.
func ensureLock() bool {
	path := ensureLockPath()
	if path == "" {
		return false
	}
	// mkdir is atomic on POSIX
	err := os.Mkdir(path, 0o700)
	return err == nil
}

// ensureUnlock releases the ensure lock.
func ensureUnlock() {
	path := ensureLockPath()
	if path != "" {
		os.Remove(path)
	}
}

// waitForLock waits for the ensure lock to be released, with timeout.
func waitForLock(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		path := ensureLockPath()
		if path == "" {
			return true
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// ensureSidebars opens a sidebar in every window that lacks one.
// Focus stays where it was.
// Concurrent calls are serialized per tmux server.
// Errors for individual windows are accumulated; the function returns
// an error if any window failed, but continues processing all windows.
func ensureSidebars(bin string) error {
	if !autocreateEnabled() {
		return nil
	}

	// Try to acquire lock; if busy, wait briefly for the other ensure to finish.
	if !ensureLock() {
		if !waitForLock(2 * time.Second) {
			return fmt.Errorf("ensure: timeout waiting for concurrent ensure to finish")
		}
		// Lock is free now, but another ensure just finished.
		// Re-check if sidebars are already present to avoid redundant work.
		if !ensureLock() {
			return fmt.Errorf("ensure: failed to acquire lock after wait")
		}
	}
	defer ensureUnlock()

	panes, err := listPanes()
	if err != nil {
		return err
	}
	width := globalOptions()["@sentinela_width"]
	if width == "" {
		width = defaultWidth
	}

	// Build initial set of windows that already have a sidebar.
	has := map[string]bool{}
	for _, p := range panes {
		if p.Sidebar {
			has[p.WindowID] = true
		}
	}

	var errs []string
	seen := map[string]bool{}
	for _, p := range panes {
		if has[p.WindowID] || seen[p.WindowID] {
			continue
		}
		// Respect user's deliberate toggle off.
		if isUserClosed(p.WindowID) {
			seen[p.WindowID] = true
			continue
		}
		// Don't unzoom a window the user left without a sidebar.
		if isWindowZoomed(p.WindowID) {
			seen[p.WindowID] = true
			continue
		}
		seen[p.WindowID] = true

		// Re-verify the window still lacks a sidebar (idempotent check)
		// in case another process created one while we held the lock.
		check, _ := tmux("list-panes", "-t", p.WindowID, "-F", "#{@sentinela_sidebar}")
		if strings.Contains(check, "1") {
			has[p.WindowID] = true
			continue
		}

		if err := openSidebar(bin, p.WindowID, width); err != nil {
			errs = append(errs, fmt.Sprintf("window %s: %v", p.WindowID, err))
			// Continue with other windows
		} else {
			has[p.WindowID] = true
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("ensure: %d window(s) failed: %s", len(errs), strings.Join(errs, "; "))
	}
	return nil
}

func openSidebar(bin, windowID, width string) error {
	// -f: full window height at the left edge, not a split of the active pane.
	id, err := tmux("split-window", "-hbdf", "-l", width, "-t", windowID,
		"-P", "-F", "#{pane_id}", bin+" sidebar")
	if err != nil {
		return fmt.Errorf("split-window: %w", err)
	}
	if _, err = tmux("set-option", "-p", "-t", id, "@sentinela_sidebar", "1"); err != nil {
		return err
	}
	paintSidebar(id, string(loadTheme(globalOptions()).background))
	return configureSidebarName(id)
}

const workPaneName = "#{P:#{?pane_last,#{E:@sentinela_rename_format},}}"
const sidebarRenameFormat = "#{?#{@sentinela_sidebar},#{?" + workPaneName + "," + workPaneName + ",#{window_name}},#{E:@sentinela_rename_format}}"

// Keep the user's format, but evaluate it on the last work pane while the
// sidebar has focus. Unlike rename-window, this leaves automatic-rename alone.
func configureSidebarName(paneID string) error {
	format, err := tmux("display-message", "-p", "-t", paneID, "#{automatic-rename-format}")
	if err != nil || format == sidebarRenameFormat {
		return err
	}
	if _, err := tmux("set-option", "-w", "-t", paneID, "@sentinela_rename_format", format); err != nil {
		return err
	}
	_, err = tmux("set-option", "-w", "-t", paneID, "automatic-rename-format", sidebarRenameFormat)
	return err
}

// An empty background restores inheritance from the window's styles.
func paintSidebar(paneID, background string) {
	// Same options select-pane -P sets, without stealing focus like it does.
	for _, opt := range []string{"window-style", "window-active-style"} {
		if background == "" {
			tmux("set-option", "-pu", "-t", paneID, opt)
		} else {
			tmux("set-option", "-p", "-t", paneID, opt, "bg="+background)
		}
	}
}

const defaultWidth = "32"

// toggleSidebar closes the sidebar of the given window, or opens one.
// Without an id, falls back to tmux's notion of the current window.
func toggleSidebar(bin, windowID string) error {
	cur := windowID
	if cur == "" {
		var err error
		if cur, err = tmux("display-message", "-p", "#{window_id}"); err != nil {
			return err
		}
	}
	panes, err := listPanes()
	if err != nil {
		return err
	}
	for _, p := range panes {
		if p.WindowID == cur && p.Sidebar {
			_, err := tmux("kill-pane", "-t", p.ID)
			if err != nil {
				return err
			}
			// Remember the user's deliberate choice to close.
			setUserClosed(cur, true)
			return nil
		}
	}
	width := globalOptions()["@sentinela_width"]
	if width == "" {
		width = defaultWidth
	}
	// User opened manually: clear any previous "closed" marker.
	setUserClosed(cur, false)
	return openSidebar(bin, cur, width)
}

// pruneSidebars kills sidebars left alone in their window, so closing the
// last real pane closes the window as it would without the plugin.
func pruneSidebars() error {
	panes, err := listPanes()
	if err != nil {
		return err
	}
	others := map[string]int{}
	for _, p := range panes {
		if !p.Sidebar {
			others[p.WindowID]++
		}
	}
	for _, p := range panes {
		if p.Sidebar && others[p.WindowID] == 0 {
			tmux("kill-pane", "-t", p.ID)
		}
	}
	return nil
}

func selfPane() string { return os.Getenv("TMUX_PANE") }

func siblingSidebar(paneID string) (bool, error) {
	panes, err := listPanes()
	if err != nil {
		return false, err
	}
	var win string
	for _, p := range panes {
		if p.ID == paneID {
			win = p.WindowID
		}
	}
	for _, p := range panes {
		if p.WindowID == win && p.ID != paneID && p.Sidebar {
			return true, nil
		}
	}
	return false, nil
}

// serverIdentity returns a short, stable identifier for the current tmux server.
// It uses the tmux socket path, hashed to a short string safe for filenames.
func serverIdentity() string {
	socket, err := tmux("display-message", "-p", "#{socket_path}")
	if err != nil || socket == "" {
		// Fallback: parse TMUX env var (format: socket,window,pane)
		if tmuxEnv := os.Getenv("TMUX"); tmuxEnv != "" {
			if idx := strings.Index(tmuxEnv, ","); idx > 0 {
				socket = tmuxEnv[:idx]
			} else {
				socket = tmuxEnv
			}
		}
	}
	if socket == "" {
		return "unknown"
	}
	// Short hash for filesystem-safe keys
	sum := sha256.Sum256([]byte(socket))
	return fmt.Sprintf("%x", sum[:4]) // 8 hex chars
}
