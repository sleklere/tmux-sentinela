package main

import "strconv"

type sidebarLayout struct {
	panes []Pane
	width int
}

type paneResize struct {
	id    string
	width int
}

// Only the leader synchronizes layout. Its post-resize snapshot distinguishes
// a user's next resize from changes it just applied to the other sidebars.
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
		if _, err := tmux("resize-pane", "-t", resize.id, "-x", strconv.Itoa(resize.width)); err != nil {
			return sidebarLayout{}, err
		}
	}
	if len(resizes) > 0 {
		if panes, err = listPanes(); err != nil {
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
	width := configured
	if configured == previous.width {
		for _, p := range panes {
			before, known := old[p.ID]
			if p.Sidebar && known && !p.Zoomed && !before.Zoomed &&
				p.WindowWidth == before.WindowWidth && p.Width != before.Width {
				width = p.Width
				break
			}
		}
	}
	var resizes []paneResize
	for _, p := range panes {
		if !p.Sidebar || p.Zoomed || p.Width == width {
			continue
		}
		before, known := old[p.ID]
		// tmux may clamp the requested width in a small window. Don't retry or
		// publish that clamped width until the layout or desired width changes.
		if known && !before.Zoomed && width == previous.width &&
			p.Width == before.Width && p.WindowWidth == before.WindowWidth {
			continue
		}
		resizes = append(resizes, paneResize{id: p.ID, width: width})
	}
	return width, resizes
}
