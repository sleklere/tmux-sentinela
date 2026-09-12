package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Pane is one tmux pane as reported by list-panes -a.
type Pane struct {
	ID          string
	PID         int
	Session     string
	WindowID    string
	WindowIndex int
	WindowName  string
	PaneIndex   int
	Path        string
	Visible     bool // active pane of the active window of an attached session
	Current     bool // in the active window of an attached session
	Sidebar     bool // pane running our sidebar
}

func tmux(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	// Only newlines: a trailing tab is an empty last field, not noise.
	return strings.TrimRight(string(out), "\n"), err
}

const paneFormat = "#{pane_id}\t#{pane_pid}\t#{session_name}\t#{window_id}\t#{window_index}\t#{window_name}\t#{pane_index}\t#{pane_current_path}\t#{pane_active}\t#{window_active}\t#{session_attached}\t#{@sentinela_sidebar}"

// listPanes returns every pane of every session, in tmux order.
func listPanes() ([]Pane, error) {
	out, err := tmux("list-panes", "-a", "-F", paneFormat)
	if err != nil {
		return nil, err
	}
	var panes []Pane
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 12 {
			continue
		}
		pid, _ := strconv.Atoi(f[1])
		wi, _ := strconv.Atoi(f[4])
		pi, _ := strconv.Atoi(f[6])
		attached, _ := strconv.Atoi(f[10])
		panes = append(panes, Pane{
			ID: f[0], PID: pid, Session: f[2], WindowID: f[3], WindowIndex: wi,
			WindowName: f[5], PaneIndex: pi, Path: f[7],
			Visible: f[8] == "1" && f[9] == "1" && attached > 0,
			Current: f[9] == "1" && attached > 0,
			Sidebar: f[11] != "",
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

// ensureSidebars opens a sidebar in every window that lacks one.
// Focus stays where it was.
func ensureSidebars(bin string) error {
	panes, err := listPanes()
	if err != nil {
		return err
	}
	width := globalOptions()["@sentinela_width"]
	if width == "" {
		width = defaultWidth
	}
	has := map[string]bool{}
	for _, p := range panes {
		if p.Sidebar {
			has[p.WindowID] = true
		}
	}
	seen := map[string]bool{}
	for _, p := range panes {
		if has[p.WindowID] || seen[p.WindowID] {
			continue
		}
		seen[p.WindowID] = true
		if err := openSidebar(bin, p.WindowID, width); err != nil {
			return err
		}
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
	paintSidebar(id)
	return nil
}

// paintSidebar gives the pane an opaque background (@sentinela_bg, else
// @th_base) so it stands out from transparent terminal panes.
func paintSidebar(paneID string) {
	o := globalOptions()
	bg := o["@sentinela_bg"]
	if bg == "" {
		bg = o["@th_base"]
	}
	if bg == "" {
		return
	}
	// Same options select-pane -P sets, without stealing focus like it does.
	for _, opt := range []string{"window-style", "window-active-style"} {
		tmux("set-option", "-p", "-t", paneID, opt, "bg="+bg)
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
			return err
		}
	}
	width := globalOptions()["@sentinela_width"]
	if width == "" {
		width = defaultWidth
	}
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
