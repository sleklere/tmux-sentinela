package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type claudeHookPayload struct {
	SessionID        string `json:"session_id"`
	Event            string `json:"hook_event_name"`
	NotificationType string `json:"notification_type"`
	ToolName         string `json:"tool_name"`
}

func claudeHook(r io.Reader) error {
	var p claudeHookPayload
	if err := json.NewDecoder(r).Decode(&p); err != nil || p.SessionID == "" {
		return nil // never break Claude over a bad payload
	}
	dir := filepath.Join(stateDir(), "claude-blocked")
	marker := filepath.Join(dir, p.SessionID)
	if p.Event == "SessionEnd" {
		removeMirroredClaudeSession(p.SessionID)
		os.Remove(marker)
		return nil
	}
	if err := mirrorClaudeSession(p); err != nil {
		return err
	}

	blocked := (p.Event == "Notification" && p.NotificationType == "permission_prompt") ||
		(p.Event == "PreToolUse" && p.ToolName == "AskUserQuestion")
	if blocked {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(marker, nil, 0o644)
	}
	os.Remove(marker)
	return nil
}

func mirrorClaudeSession(p claudeHookPayload) error {
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		configDir = filepath.Dir(claudeSessionsDir())
	}
	files, _ := filepath.Glob(filepath.Join(configDir, "sessions", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s claudeSession
		if json.Unmarshal(b, &s) != nil || s.SessionID != p.SessionID || s.PID == 0 {
			continue
		}
		now := time.Now().UnixMilli()
		switch p.Event {
		case "UserPromptSubmit", "PreToolUse", "PostToolUse":
			s.Status, s.StatusUpdatedAt = "busy", now
		case "Stop":
			s.Status, s.StatusUpdatedAt = "idle", now
		}
		s.UpdatedAt = now
		return writeClaudeSession(s)
	}
	return nil
}

func writeClaudeSession(s claudeSession) error {
	dir := filepath.Join(stateDir(), "claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", s.PID)), b, 0o600)
}

func removeMirroredClaudeSession(sessionID string) {
	files, _ := filepath.Glob(filepath.Join(stateDir(), "claude", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s claudeSession
		if json.Unmarshal(b, &s) == nil && s.SessionID == sessionID {
			os.Remove(f)
			removeSeen(fmt.Sprintf("claude:%d", s.PID))
		}
	}
}
