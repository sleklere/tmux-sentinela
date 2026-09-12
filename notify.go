package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// notify announces an agent that just became blocked. mode is @sentinela_notify:
// tmux (default) | desktop | both | off.
func notify(a Agent, mode string) {
	msg := fmt.Sprintf("%s is waiting for you (%s:%d)", a.Name, a.Pane.Session, a.Pane.WindowIndex)
	switch mode {
	case "", "tmux", "both":
		clients, _ := tmux("list-clients", "-F", "#{client_name}")
		for _, c := range strings.Split(clients, "\n") {
			if c != "" { // ## escapes format expansion
				tmux("display-message", "-c", c, "-d", "4000", "tmux-sentinela: "+strings.ReplaceAll(msg, "#", "##"))
			}
		}
	}
	switch mode {
	case "desktop", "both":
		if runtime.GOOS == "darwin" {
			exec.Command("osascript", "-e", fmt.Sprintf(`display notification %q with title "tmux-sentinela"`, msg)).Run()
		} else {
			exec.Command("notify-send", "tmux-sentinela", msg).Run()
		}
	}
}
