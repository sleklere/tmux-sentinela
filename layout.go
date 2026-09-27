package main

import "strconv"

// A sidebar may occupy at most half the window. Keep the configured width
// unchanged when the window is small so it can grow back with the window.
func sidebarWidth(width, windowWidth int) int {
	if width < 1 {
		width = 1
	}
	if windowWidth > 0 {
		limit := windowWidth / 2
		if limit < 1 {
			limit = 1
		}
		if width > limit {
			width = limit
		}
	}
	return width
}

type sidebarLayout struct {
	panes []Pane
	width int
}

type paneResize struct {
	id    string
	width int
}

// The after-resize-pane hook must not mistake our own corrections for a user
// dragging the sidebar. Hook conditions are evaluated before resize-pane exits.
func resizeSidebar(id string, width int) error {
	if _, err := tmux("set-option", "-p", "-t", id, "@sentinela_internal_resize", "1"); err != nil {
		return err
	}
	defer tmux("set-option", "-pu", "-t", id, "@sentinela_internal_resize")
	_, err := tmux("resize-pane", "-t", id, "-x", strconv.Itoa(width))
	return err
}

// Only the leader synchronizes widths. It records the last observed size so
// zooming back out cannot publish the old size as a manual resize.
func syncSidebarWidths(panes []Pane, previous sidebarLayout) (sidebarLayout, error) {
	raw, err := tmux("show-option", "-gqv", "@sentinela_width")
	if err != nil {
		return sidebarLayout{}, err
	}
	configured, _ := strconv.Atoi(raw)
	if configured <= 0 {
		configured, _ = strconv.Atoi(defaultWidth)
	}
	width, resizes := planSidebarWidths(panes, previous, configured)
	if width != configured {
		if _, err := tmux("set-option", "-g", "@sentinela_width", strconv.Itoa(width)); err != nil {
			return sidebarLayout{}, err
		}
	}
	for _, resize := range resizes {
		if err := resizeSidebar(resize.id, resize.width); err != nil {
			return sidebarLayout{}, err
		}
	}
	if len(resizes) > 0 {
		if panes, err = listPanes(); err != nil {
			return sidebarLayout{}, err
		}
	}
	observed := make(map[string]Pane, len(previous.panes))
	for _, pane := range previous.panes {
		if pane.Sidebar {
			observed[pane.ID] = pane
		}
	}
	for _, pane := range panes {
		if !pane.Sidebar || pane.Zoomed {
			continue
		}
		before, known := observed[pane.ID]
		if known && !before.Zoomed && before.Width == pane.Width {
			continue
		}
		if _, err := tmux("set-option", "-p", "-t", pane.ID,
			"@sentinela_observed_width", strconv.Itoa(pane.Width)); err != nil {
			return sidebarLayout{}, err
		}
	}
	return sidebarLayout{panes: panes, width: width}, nil
}

func planSidebarWidths(panes []Pane, previous sidebarLayout, configured int) (int, []paneResize) {
	old := make(map[string]Pane, len(previous.panes))
	for _, p := range previous.panes {
		if p.Sidebar {
			old[p.ID] = p
		}
	}
	// Only the explicit resize-pane hook changes the shared preference.
	// Layout commands can also resize a pane, and polling cannot tell those
	// changes apart from manual resizing before the pin hook runs.
	width := configured
	var resizes []paneResize
	for _, p := range panes {
		if !p.Sidebar || p.Zoomed {
			continue
		}
		effective := sidebarWidth(width, p.WindowWidth)
		if p.Width == effective {
			continue
		}
		before, known := old[p.ID]
		// tmux may clamp the requested width in a small window. Don't retry or
		// publish that clamped width until the layout or desired width changes.
		if known && !before.Zoomed && width == previous.width &&
			p.Width == before.Width && p.WindowWidth == before.WindowWidth &&
			p.Width <= effective {
			continue
		}
		resizes = append(resizes, paneResize{id: p.ID, width: effective})
	}
	return width, resizes
}
