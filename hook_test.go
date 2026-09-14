package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeHookMirrorsCustomConfigDir(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "custom-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	session := claudeSession{PID: os.Getpid(), SessionID: "session-1", Status: "idle"}
	writeJSON(t, filepath.Join(configDir, "sessions", fmt.Sprintf("%d.json", session.PID)), session)

	runClaudeHook(t, `{"session_id":"session-1","hook_event_name":"UserPromptSubmit"}`)
	mirrored := readMirroredSession(t, session.PID)
	if mirrored.Status != "busy" {
		t.Fatalf("status after prompt = %q, want busy", mirrored.Status)
	}
	if mirrored.StatusUpdatedAt == 0 || mirrored.UpdatedAt < mirrored.StatusUpdatedAt {
		t.Fatalf("missing or inconsistent timestamps: %+v", mirrored)
	}

	runClaudeHook(t, `{"session_id":"session-1","hook_event_name":"Stop"}`)
	mirrored = readMirroredSession(t, session.PID)
	if mirrored.Status != "idle" {
		t.Fatalf("status after stop = %q, want idle", mirrored.Status)
	}

	key := fmt.Sprintf("claude:%d", session.PID)
	writeSeen(key, time.Now())
	runClaudeHook(t, `{"session_id":"session-1","hook_event_name":"Notification","notification_type":"permission_prompt"}`)
	runClaudeHook(t, `{"session_id":"session-1","hook_event_name":"SessionEnd"}`)
	if _, err := os.Stat(filepath.Join(stateDir(), "claude", fmt.Sprintf("%d.json", session.PID))); !os.IsNotExist(err) {
		t.Fatalf("mirrored session still exists after SessionEnd: %v", err)
	}
	if _, ok := readSeen(key); ok {
		t.Fatal("seen state still exists after SessionEnd")
	}
	if blocked, _ := claudeBlocked(session.SessionID); blocked {
		t.Fatal("permission marker still exists after SessionEnd")
	}
}

func TestClaudeHookBlockedTransitions(t *testing.T) {
	for _, tt := range []struct {
		name    string
		payload string
		blocked bool
	}{
		{"permission", `{"hook_event_name":"Notification","notification_type":"permission_prompt"}`, true},
		{"question", `{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion"}`, true},
		{"other notification", `{"hook_event_name":"Notification","notification_type":"idle_prompt"}`, false},
		{"other tool", `{"hook_event_name":"PreToolUse","tool_name":"Bash"}`, false},
		{"tool completed", `{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion"}`, false},
		{"prompt", `{"hook_event_name":"UserPromptSubmit"}`, false},
		{"stop", `{"hook_event_name":"Stop"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateState(t)
			payload := `{"session_id":"target",` + tt.payload[1:]
			for _, initiallyBlocked := range []bool{false, true} {
				marker := filepath.Join(stateDir(), "claude-blocked", "target")
				if initiallyBlocked {
					writeTestFile(t, marker, nil)
				}
				runClaudeHook(t, payload)
				blocked, at := claudeBlocked("target")
				if blocked != tt.blocked || (blocked && at.IsZero()) {
					t.Fatalf("initial=%v: blocked=%v at=%v, want blocked=%v", initiallyBlocked, blocked, at, tt.blocked)
				}
			}
		})
	}
}

func TestClaudeHookIgnoresInvalidPayload(t *testing.T) {
	for _, payload := range []string{"", "{", "null", `{}`, `{"hook_event_name":"Stop"}`} {
		t.Run(payload, func(t *testing.T) {
			isolateState(t)
			marker := filepath.Join(stateDir(), "claude-blocked", "existing")
			writeTestFile(t, marker, nil)
			before, _ := claudeBlocked("existing")
			runClaudeHook(t, payload)
			after, _ := claudeBlocked("existing")
			if !before || !after {
				t.Fatal("invalid payload removed an existing marker")
			}
		})
	}
}

func runClaudeHook(t *testing.T, payload string) {
	t.Helper()
	if err := claudeHook(strings.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
}

func readMirroredSession(t *testing.T, pid int) claudeSession {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(stateDir(), "claude", fmt.Sprintf("%d.json", pid)))
	if err != nil {
		t.Fatal(err)
	}
	var session claudeSession
	if err := json.Unmarshal(b, &session); err != nil {
		t.Fatal(err)
	}
	return session
}
