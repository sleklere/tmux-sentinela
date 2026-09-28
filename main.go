// tmux-sentinela: a tmux sidebar showing every Claude Code / OpenCode agent
// running in any session, its state, and a way to jump to it.
package main

import (
	"fmt"
	"os"
)

const usage = `usage: tmux-sentinela <command>

  sidebar      run the sidebar TUI (used inside the sidebar pane)
  ensure       open a sidebar in every window that has none
  toggle [@id] open/close the sidebar of a window (default: current)
  next-layout [@id] cycle layouts while keeping the sidebar on the left
  pin [@id]     restore the sidebar after an external layout change
  pin-layout [@id] restore position and width after selecting a layout
  sidebar-resized <pane> <width> <window-width> record a manual width change
  sidebar-drag-start <window> mark a mouse border drag
  sidebar-drag-end <window> save the width after mouse release
  prune        close sidebars left alone in their window
  refresh      wake sidebars after a state or focus change
  status [--json] print local agents as versioned JSON, or text diagnostics
  claude-hook  Claude Code hook entrypoint (JSON on stdin)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	bin, _ := os.Executable()
	var err error
	switch os.Args[1] {
	case "sidebar":
		err = runSidebar(bin)
	case "ensure":
		err = ensureSidebars(bin)
	case "toggle":
		err = toggleSidebar(bin, arg(2))
	case "next-layout":
		err = pinSidebar(arg(2), true, false)
	case "pin":
		err = pinSidebar(arg(2), false, false)
	case "pin-layout":
		err = pinSidebar(arg(2), false, true)
	case "sidebar-resized":
		err = sidebarResized(arg(2), arg(3), arg(4), arg(5), arg(6), arg(7))
	case "sidebar-drag-start":
		err = sidebarDragStart(arg(2))
	case "sidebar-drag-end":
		err = sidebarDragEnd(arg(2))
	case "prune":
		err = pruneSidebars()
	case "refresh":
		err = publishRefresh()
	case "status":
		if arg(2) == "--json" && len(os.Args) == 3 {
			err = printStatusJSON(os.Stdout)
		} else if len(os.Args) == 2 {
			err = printStatus()
		} else {
			err = fmt.Errorf("usage: tmux-sentinela status [--json]")
		}
	case "claude-hook":
		err = claudeHook(os.Stdin)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tmux-sentinela:", err)
		os.Exit(1)
	}
}

func arg(i int) string {
	if len(os.Args) > i {
		return os.Args[i]
	}
	return ""
}
