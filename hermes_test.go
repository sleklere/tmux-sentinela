package main

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestDetectHermesScreen(t *testing.T) {
	idle, err := os.ReadFile("testdata/hermes-idle.txt")
	if err != nil {
		t.Fatal(err)
	}
	base := " ⚕ test-model │ ~10K/100K │ ~10%%\n────────────────────────\n%s\n────────────────────────"
	tests := []struct {
		name       string
		screen     string
		wantName   string
		wantStatus Status
		wantOK     bool
	}{
		{"real idle capture", string(idle), "Draft indexing design", Idle, true},
		{"busy", fmt.Sprintf(base, "⚕ Ψ msg=interrupt · /queue · /bg · /steer · Ctrl+C cancel"), "Hermes", Busy, true},
		{"approval", fmt.Sprintf(base, "⚠ Ψ"), "Hermes", Blocked, true},
		{"clarification", fmt.Sprintf(base, "? Ψ"), "Hermes", Blocked, true},
		{"free text clarification", fmt.Sprintf(base, "✎ Ψ type your answer here"), "Hermes", Blocked, true},
		{"sudo", fmt.Sprintf(base, "🔐 Ψ type password"), "Hermes", Blocked, true},
		{"secret", fmt.Sprintf(base, "🔑 Ψ type secret"), "Hermes", Blocked, true},
		{"command hint", fmt.Sprintf(base, "⠋ command in progress · input temporarily disabled"), "Hermes", Busy, true},
		{"command placeholder", fmt.Sprintf(base, "⠋ Processing command..."), "Hermes", Busy, true},
		{"battery status bar", "🔋 80% │ ⚕ model\n────────────\n❯ ask\n────────────", "Hermes", Idle, true},
		{"skin name fallback", "╭─ Ψ Poseidon ─────────╮\nReady\n╰───────────────────────╯\n⚕ model\n────────────\nΨ ask\n────────────", "Poseidon", Idle, true},
		{"ssh shell mentioning Hermes", "$ printf '⚕ Hermes'\n⚕ Hermes\n$", "", Idle, false},
		{"old response only", "╭─ ⚕ Hermes ───╮\nDone\n╰──────────────╯\n$", "", Idle, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := detectHermesScreen(tt.screen)
			if ok != tt.wantOK || got.Name != tt.wantName || got.Status != tt.wantStatus {
				t.Fatalf("detectHermesScreen() = %+v, %v, want {%q %v}, %v",
					got, ok, tt.wantName, tt.wantStatus, tt.wantOK)
			}
		})
	}
}

func TestCollectHermesAgentsOnlyCapturesSSHPanes(t *testing.T) {
	panes := []Pane{
		{ID: "%ssh", PID: 10, Command: "ssh"},
		{ID: "%shell", PID: 20, Command: "zsh"},
		{ID: "%failed", PID: 30, Command: "ssh"},
	}
	captured := map[string]int{}
	capture := func(paneID string) (string, error) {
		captured[paneID]++
		if paneID == "%failed" {
			return "", errors.New("pane disappeared")
		}
		return "⚕ model\n────────────\n❯ ask\n────────────", nil
	}

	got := collectHermesAgents(panes, capture)
	if len(got) != 1 {
		t.Fatalf("agents = %+v, want one", got)
	}
	if got[0].Kind != "hermes" || got[0].Name != "Hermes" || got[0].Status != Idle ||
		got[0].Key != "hermes:%ssh" || got[0].Pane != panes[0] {
		t.Fatalf("agent = %+v", got[0])
	}
	if captured["%ssh"] != 1 || captured["%failed"] != 1 || captured["%shell"] != 0 {
		t.Fatalf("captures = %v", captured)
	}
}
