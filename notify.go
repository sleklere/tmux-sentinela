package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

//go:embed assets/sounds/done.wav
var completionSound []byte

type commandSpec struct {
	program string
	args    []string
}

type notification struct {
	title   string
	body    string
	icon    string
	appIcon string
}

// notify announces an agent that just became blocked. mode is @sentinela_notify:
// tmux (default) | desktop | both | off.
func notify(a Agent, mode string) {
	notifyMessage(blockedNotification(a), mode)
}

func blockedNotification(a Agent) notification {
	return notification{
		title:   a.Name,
		body:    fmt.Sprintf("%s is waiting for you (%s:%d)", a.Name, a.Pane.Session, a.Pane.WindowIndex),
		icon:    "utilities-terminal",
		appIcon: "dialog-warning",
	}
}

func notifyCompletion(a Agent, mode string) {
	notifyMessage(completionNotification(a), mode)
}

func completionNotification(a Agent) notification {
	return notification{
		title:   a.Name,
		body:    fmt.Sprintf("%s finished (%s:%d)", a.Name, a.Pane.Session, a.Pane.WindowIndex),
		icon:    "utilities-terminal",
		appIcon: "emblem-default",
	}
}

func notifyMessage(notification notification, mode string) {
	switch mode {
	case "", "tmux", "both":
		clients, _ := tmux("list-clients", "-F", "#{client_name}")
		for _, c := range strings.Split(clients, "\n") {
			if c != "" { // ## escapes format expansion
				tmux("display-message", "-c", c, "-d", "4000", "tmux-sentinela: "+strings.ReplaceAll(notification.body, "#", "##"))
			}
		}
	}
	switch mode {
	case "desktop", "both":
		desktopNotify(notification.title, notification.body, notification.icon, notification.appIcon)
	}
}

func desktopNotify(title, body, icon, appIcon string) {
	for _, command := range desktopNotificationCommands(runtime.GOOS, title, body, icon, appIcon) {
		if exec.Command(command.program, command.args...).Run() == nil {
			return
		}
	}
}

func desktopNotificationCommands(goos, title, body, icon, appIcon string) []commandSpec {
	if goos == "darwin" {
		return []commandSpec{
			{program: "terminal-notifier", args: []string{"-title", title, "-message", body}},
			{program: "/usr/bin/osascript", args: []string{
				"-e", "on run argv",
				"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
				"-e", "end run", title, body,
			}},
		}
	}
	return []commandSpec{{program: "notify-send", args: []string{
		"--icon=" + icon,
		"--app-icon=" + appIcon,
		"--", title, body,
	}}}
}

// playCompletionSound writes the embedded sound to a temporary file because
// the system players accept paths. Missing players are intentionally ignored.
func playCompletionSound() {
	file, err := os.CreateTemp("", "tmux-sentinela-done-*.wav")
	if err != nil {
		return
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err = file.Write(completionSound); err != nil {
		file.Close()
		return
	}
	if file.Close() != nil {
		return
	}

	for _, command := range audioPlayers(runtime.GOOS, path) {
		if _, err := exec.LookPath(command.program); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := exec.CommandContext(ctx, command.program, command.args...).Run()
		cancel()
		if err == nil {
			return
		}
	}
}

func audioPlayers(goos, path string) []commandSpec {
	if goos == "darwin" {
		return []commandSpec{{program: "afplay", args: []string{path}}}
	}
	return []commandSpec{
		{program: "paplay", args: []string{path}},
		{program: "pw-play", args: []string{path}},
		{program: "aplay", args: []string{path}},
		{program: "ffplay", args: []string{"-nodisp", "-autoexit", "-loglevel", "quiet", path}},
		{program: "mpv", args: []string{"--no-video", "--really-quiet", path}},
	}
}
