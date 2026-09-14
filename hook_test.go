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

	runClaudeHook(t, `{"session_id":"session-1","hook_event_name":"Stop"}`)
	mirrored = readMirroredSession(t, session.PID)
	if mirrored.Status != "idle" {
		t.Fatalf("status after stop = %q, want idle", mirrored.Status)
	}

	key := fmt.Sprintf("claude:%d", session.PID)
	writeSeen(key, time.Now())
	runClaudeHook(t, `{"session_id":"session-1","hook_event_name":"SessionEnd"}`)
	if _, err := os.Stat(filepath.Join(stateDir(), "claude", fmt.Sprintf("%d.json", session.PID))); !os.IsNotExist(err) {
		t.Fatalf("mirrored session still exists after SessionEnd: %v", err)
	}
	if _, ok := readSeen(key); ok {
		t.Fatal("seen state still exists after SessionEnd")
	}
}

func runClaudeHook(t *testing.T, payload string) {
	t.Helper()
	if err := claudeHook(strings.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
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
