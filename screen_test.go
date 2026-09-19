package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func readScreenFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDetectOpenCodeScreen(t *testing.T) {
	idle := readScreenFixture(t, "opencode-idle.txt")
	busy := readScreenFixture(t, "opencode-busy.txt")
	tests := []struct {
		name, title, screen, wantName string
		wantStatus                    Status
		wantOK                        bool
	}{
		{"real idle capture", "OpenCode", idle, "OpenCode", Idle, true},
		{"real busy capture", "OC | Refactor sidebar", busy, "Refactor sidebar", Busy, true},
		{"permission", "OpenCode", "△ Permission required\n↑↓ select  enter confirm  esc dismiss", "OpenCode", Blocked, true},
		{"form permission", "OC | Deploy", "Choose scope\n⇆ tab  enter submit  esc dismiss", "Deploy", Blocked, true},
		{"ssh shell mentions interrupt", "user@host: ~", "$ printf 'esc to interrupt'\nesc to interrupt\n$", "", Idle, false},
		{"stale permission in shell", "user@host: ~", "△ Permission required\n" + strings.Repeat("old output\n", 12) + "$", "", Idle, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := detectOpenCodeScreen(tt.title, tt.screen)
			if ok != tt.wantOK || got.Name != tt.wantName || got.Status != tt.wantStatus ||
				(ok && got.Kind != "opencode") {
				t.Fatalf("detectOpenCodeScreen() = %+v, %v; want name=%q status=%v ok=%v",
					got, ok, tt.wantName, tt.wantStatus, tt.wantOK)
			}
		})
	}
}

func TestDetectClaudeScreen(t *testing.T) {
	idle := readScreenFixture(t, "claude-idle.txt")
	tests := []struct {
		name, title, screen string
		wantStatus          Status
		wantOK              bool
	}{
		{"real idle capture", "✳ Claude Code", idle, Idle, true},
		{"busy live turn", "◐ Refactoring", "✢ Working… (12s · esc to interrupt)\n⏵⏵ auto mode on (shift+tab to cycle)", Busy, true},
		{"busy background agents", "✳ Claude Code", "✻ Waiting for 2 background agents to finish", Busy, true},
		{"bash permission", "user@host: ~", "Bash command\nDo you want to proceed?\n❯ 1. Yes\n  2. Yes, don't ask again\n  3. No\nEsc to cancel", Blocked, true},
		{"generic permission", "user@host: ~", "Do you want to proceed?\n❯ 1. Yes\n  2. No\nEsc to cancel", Blocked, true},
		{"form permission", "✳ Claude Code", "Select an option\n❯ choice\nEnter to select · Esc to cancel", Blocked, true},
		{"ssh shell mentions Claude", "user@host: ~", "$ printf 'Claude Code'\nClaude Code\n$", Idle, false},
		{"generic prompt", "user@host: ~", "────────────────\n❯\n────────────────", Idle, false},
		{"stale permission in shell", "user@host: ~", "Bash command\nDo you want to proceed?\n❯ 1. Yes\nEsc to cancel\n" + strings.Repeat("old output\n", 12) + "$", Idle, false},
		{"stale idle frame with shell prompt", "user@remote: ~", idle + "\nuser@remote:~/workspace/project$ ", Idle, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := detectClaudeScreen(tt.title, tt.screen)
			if ok != tt.wantOK || got.Status != tt.wantStatus ||
				(ok && (got.Kind != "claude" || got.Name != "Claude Code")) {
				t.Fatalf("detectClaudeScreen() = %+v, %v; want status=%v ok=%v",
					got, ok, tt.wantStatus, tt.wantOK)
			}
		})
	}
}

func TestCollectScreenAgentsCapturesEachUnclaimedPaneOnce(t *testing.T) {
	panes := []Pane{
		{ID: "%hermes", Command: "python"},
		{ID: "%open", Command: "ssh", Title: "OpenCode"},
		{ID: "%claude", Command: "ssh", Title: "✳ Claude Code"},
		{ID: "%shell", Command: "ssh", Title: "user@host: ~"},
		{ID: "%native", Command: "opencode"},
		{ID: "%sidebar", Command: "tmux-sentinela", Sidebar: true},
		{ID: "%failed", Command: "ssh"},
	}
	screens := map[string]string{
		"%hermes": "⚕ model\n────────────\nΨ ask\n────────────",
		"%open":   readScreenFixture(t, "opencode-idle.txt"),
		"%claude": readScreenFixture(t, "claude-idle.txt"),
		"%shell":  "$ echo ready\nready\n$",
	}
	captured := map[string]int{}
	capture := func(paneID string) (string, error) {
		captured[paneID]++
		if paneID == "%failed" {
			return "", errors.New("pane disappeared")
		}
		return screens[paneID], nil
	}

	got := collectScreenAgents(panes, map[string]bool{"%native": true}, capture, "test-server")
	want := []Agent{
		{Kind: "hermes", Name: "Hermes", Pane: panes[0], Key: "hermes:test-server:%hermes", Visual: true},
		{Kind: "opencode", Name: "OpenCode", Pane: panes[1], Key: "opencode-screen:test-server:%open", Visual: true},
		{Kind: "claude", Name: "Claude Code", Pane: panes[2], Key: "claude-screen:test-server:%claude", Visual: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agents = %+v, want %+v", got, want)
	}
	for _, pane := range []string{"%hermes", "%open", "%claude", "%shell", "%failed"} {
		if captured[pane] != 1 {
			t.Errorf("pane %s captured %d times, want once", pane, captured[pane])
		}
	}
	if captured["%native"] != 0 || captured["%sidebar"] != 0 {
		t.Fatalf("authoritative/sidebar panes were captured: %v", captured)
	}
}

func TestClaudeAndOpenCodeVisualDetectionRequiresSSH(t *testing.T) {
	for _, pane := range []Pane{
		{ID: "%open", Command: "opencode", Title: "OpenCode"},
		{ID: "%claude", Command: "claude", Title: "✳ Claude Code"},
	} {
		if got, ok := detectScreenAgent(pane, "⏵⏵ auto mode on (shift+tab to cycle)\nctrl+p commands\nBuild · model"); ok {
			t.Fatalf("local pane %s was visually detected as %+v", pane.ID, got)
		}
	}
}

func TestHermesVisualDetectionWorksLocallyAndOverSSH(t *testing.T) {
	screen := "⚕ model\n────────────\nΨ ask\n────────────"
	for _, command := range []string{"python", "ssh"} {
		got, ok := detectScreenAgent(Pane{Command: command}, screen)
		if !ok || got.Kind != "hermes" || got.Status != Idle {
			t.Fatalf("command %q: detectScreenAgent() = %+v, %v", command, got, ok)
		}
	}
}

func TestClaudeTitleWorkingBoundaries(t *testing.T) {
	for _, tt := range []struct {
		title string
		want  bool
	}{
		{"⟿ task", false},
		{"⠀ task", true},
		{"⣿ task", true},
		{"⤀ task", false},
		{"● task", false},
		{"◐ task", true},
		{"◓ task", true},
		{"◔ task", false},
	} {
		if got := claudeTitleWorking(tt.title); got != tt.want {
			t.Errorf("claudeTitleWorking(%q) = %v, want %v", tt.title, got, tt.want)
		}
	}
}

func TestBottomNonEmptyLines(t *testing.T) {
	for _, tt := range []struct {
		name, screen string
		count        int
		want         string
	}{
		{"empty", "\n \n", 3, ""},
		{"exact", "one\n\ntwo", 2, "one\ntwo"},
		{"last content", "old\n\nolder\n\nlive\nfooter\n", 2, "live\nfooter"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := bottomNonEmptyLines(tt.screen, tt.count); got != tt.want {
				t.Fatalf("bottomNonEmptyLines() = %q, want %q", got, tt.want)
			}
		})
	}
}
