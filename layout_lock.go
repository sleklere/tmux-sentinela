package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// pinSidebar keeps the sidebar on the full-height left edge. Layout hooks
// also fire for our own join/resize commands, so the geometry check must be
// idempotent. The lock serializes hooks and the Space binding per window.
func pinSidebar(windowID string, next, force bool) error {
	if windowID == "" {
		var err error
		windowID, err = tmux("display-message", "-p", "#{window_id}")
		if err != nil {
			return err
		}
	}
	socket, err := tmux("display-message", "-p", "#{socket_path}")
	if err != nil {
		return fmt.Errorf("cannot locate tmux socket for layout lock: %w", err)
	}
	if socket == "" {
		return fmt.Errorf("cannot locate tmux socket for layout lock")
	}
	windowHash := sha256.Sum256([]byte(windowID))
	path := filepath.Join(filepath.Dir(socket), fmt.Sprintf(".tmux-sentinela-layout-%x.lock", windowHash[:8]))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)

	enabled := globalOptions()["@sentinela_lock_sidebar"] != "off"
	panes, err := listPanes()
	if err != nil {
		return err
	}
	var sidebar Pane
	var work string
	for _, p := range panes {
		if p.WindowID != windowID {
			continue
		}
		if p.Sidebar {
			sidebar = p
		} else if work == "" {
			work = p.ID
		}
	}
	// The binding still cycles layouts in windows without a sidebar, and
	// disabling the option leaves tmux's usual layout behavior untouched.
	if next {
		if !enabled || sidebar.ID == "" || sidebar.Zoomed {
			_, err = tmux("next-layout", "-t", windowID)
			return err
		}
		// Capture the desired width before next-layout changes the geometry.
		width := sidebar.Width
		if _, err = tmux("next-layout", "-t", windowID); err != nil {
			return err
		}
		return restoreSidebar(windowID, sidebar.ID, work, width, sidebar.WindowWidth, sidebar.ID == activePane(windowID))
	}
	if !enabled || sidebar.ID == "" || work == "" || sidebar.Zoomed {
		return nil
	}
	width, _ := strconv.Atoi(globalOptions()["@sentinela_width"])
	if width <= 0 {
		width, _ = strconv.Atoi(defaultWidth)
	}
	if sidebar.Left == 0 && sidebar.Top == 0 && sidebar.Height == sidebar.WindowHeight &&
		(!force || sidebar.Width == sidebarWidth(width, sidebar.WindowWidth)) {
		return nil
	}
	return restoreSidebar(windowID, sidebar.ID, work, width, sidebar.WindowWidth, sidebar.ID == activePane(windowID))
}

// sidebarResized records a deliberate resize-pane operation. Resizing the
// adjacent work pane can move the sidebar border too. The hook passes the
// target geometry so later layout changes cannot turn a stale notification
// into a new shared width.
func sidebarResized(id, rawWidth, rawWindowWidth, marked, internal, zoomed string) error {
	if internal == "1" || zoomed == "1" {
		return nil
	}
	width, err := strconv.Atoi(rawWidth)
	if err != nil || width < 1 {
		return nil
	}
	windowWidth, err := strconv.Atoi(rawWindowWidth)
	if err != nil || windowWidth < 1 {
		return nil
	}
	panes, err := listPanes()
	if err != nil {
		return err
	}
	var target Pane
	for _, pane := range panes {
		if pane.ID == id {
			target = pane
			break
		}
	}
	if target.ID == "" || target.Sidebar != (marked == "1") || target.Zoomed ||
		target.Width != width || target.WindowWidth != windowWidth {
		return nil // a later layout change superseded this resize event
	}
	for _, pane := range panes {
		if pane.WindowID != target.WindowID || !pane.Sidebar || pane.Zoomed || pane.Left != 0 ||
			pane.Top != 0 || pane.Height != pane.WindowHeight {
			continue
		}
		return publishSidebarWidth(pane)
	}
	return nil
}

// Mouse drags change the layout without running another resize-pane command.
// Keep the polling leader from restoring the old width until mouse release.
func sidebarDragStart(windowID string) error {
	if windowID == "" {
		return nil
	}
	_, err := tmux("set-option", "-w", "-t", windowID, "@sentinela_dragging", strconv.FormatInt(time.Now().Unix(), 10))
	return err
}

func sidebarDragEnd(windowID string) error {
	if windowID == "" {
		return nil
	}
	defer tmux("set-option", "-wu", "-t", windowID, "@sentinela_dragging")
	panes, err := listPanes()
	if err != nil {
		return err
	}
	for _, pane := range panes {
		if pane.WindowID == windowID && pane.Sidebar && !pane.Zoomed && pane.Left == 0 &&
			pane.Top == 0 && pane.Height == pane.WindowHeight {
			return publishSidebarWidth(pane)
		}
	}
	return nil
}

func publishSidebarWidth(pane Pane) error {
	observed, err := tmux("show-option", "-pqv", "-t", pane.ID, "@sentinela_observed_width")
	if err != nil || observed == strconv.Itoa(pane.Width) {
		return err // unchanged width (including unzoom) is not a manual resize
	}
	_, err = tmux("set-option", "-g", "@sentinela_width", strconv.Itoa(sidebarWidth(pane.Width, pane.WindowWidth)))
	return err
}

func activePane(windowID string) string {
	id, _ := tmux("display-message", "-p", "-t", windowID, "#{pane_id}")
	return id
}

func restoreSidebar(windowID, sidebar, work string, width, windowWidth int, wasActive bool) error {
	if work == "" || sidebar == "" {
		return nil
	}
	effective := sidebarWidth(width, windowWidth)
	if _, err := tmux("join-pane", "-hbdf", "-l", strconv.Itoa(effective), "-s", sidebar, "-t", work); err != nil {
		return fmt.Errorf("restore sidebar in %s: %w", windowID, err)
	}
	if err := resizeSidebar(sidebar, effective); err != nil {
		return err
	}
	if wasActive {
		_, err := tmux("select-pane", "-t", sidebar)
		return err
	}
	return nil
}
