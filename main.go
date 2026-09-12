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
  prune        close sidebars left alone in their window
  status       print every pane and detected agent (debug / scripting)
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
	case "prune":
		err = pruneSidebars()
	case "status":
		err = printStatus()
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
