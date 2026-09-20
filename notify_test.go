package main

import (
	"reflect"
	"testing"
)

func TestEmbeddedCompletionSound(t *testing.T) {
	if len(completionSound) < 12 || string(completionSound[:4]) != "RIFF" ||
		string(completionSound[8:12]) != "WAVE" {
		t.Fatal("embedded completion sound is not a WAV file")
	}
}

func TestAudioPlayers(t *testing.T) {
	path := "/tmp/done.wav"
	linux := audioPlayers("linux", path)
	wantPrograms := []string{"paplay", "pw-play", "aplay", "ffplay", "mpv"}
	programs := make([]string, len(linux))
	for i, player := range linux {
		programs[i] = player.program
		if player.args[len(player.args)-1] != path {
			t.Fatalf("%s does not receive the sound path: %v", player.program, player.args)
		}
	}
	if !reflect.DeepEqual(programs, wantPrograms) {
		t.Fatalf("Linux players = %v, want %v", programs, wantPrograms)
	}

	mac := audioPlayers("darwin", path)
	if len(mac) != 1 || mac[0].program != "afplay" || !reflect.DeepEqual(mac[0].args, []string{path}) {
		t.Fatalf("macOS players = %+v", mac)
	}
}

func TestDesktopNotificationCommands(t *testing.T) {
	linux := desktopNotificationCommands("linux", "title", "body", "utilities-terminal", "dialog-warning")
	if len(linux) != 1 || linux[0].program != "notify-send" ||
		!reflect.DeepEqual(linux[0].args, []string{
			"--icon=utilities-terminal", "--app-icon=dialog-warning", "--", "title", "body",
		}) {
		t.Fatalf("Linux notification command = %+v", linux)
	}

	mac := desktopNotificationCommands("darwin", "title", "body", "utilities-terminal", "dialog-warning")
	if len(mac) != 2 || mac[0].program != "terminal-notifier" || mac[1].program != "/usr/bin/osascript" {
		t.Fatalf("macOS notification commands = %+v", mac)
	}
	if got := mac[1].args[len(mac[1].args)-2:]; !reflect.DeepEqual(got, []string{"title", "body"}) {
		t.Fatalf("osascript does not receive title/body as argv: %v", mac[1].args)
	}
}

func TestAgentNotificationsUseAgentNameAndOutcomeIcons(t *testing.T) {
	a := Agent{Name: "agent", Pane: Pane{Session: "work", WindowIndex: 2}}
	blocked := blockedNotification(a)
	finished := completionNotification(a)

	if blocked.title != "agent" {
		t.Fatalf("blocked notification title = %q", blocked.title)
	}
	if finished.title != "agent" {
		t.Fatalf("finished notification title = %q", finished.title)
	}
	if blocked.body != "agent is waiting for you (work:2)" {
		t.Fatalf("blocked notification body = %q", blocked.body)
	}
	if finished.body != "agent finished (work:2)" {
		t.Fatalf("finished notification body = %q", finished.body)
	}
	if blocked.icon != "utilities-terminal" || blocked.appIcon != "dialog-warning" {
		t.Fatalf("blocked notification icons = %q, %q", blocked.icon, blocked.appIcon)
	}
	if finished.icon != "utilities-terminal" || finished.appIcon != "emblem-default" {
		t.Fatalf("finished notification icons = %q, %q", finished.icon, finished.appIcon)
	}
}
