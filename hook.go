package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

type claudeHookPayload struct {
	SessionID        string `json:"session_id"`
	Event            string `json:"hook_event_name"`
	NotificationType string `json:"notification_type"`
	ToolName         string `json:"tool_name"`
}

// claudeHook is called by Claude Code hooks (JSON on stdin). It only tracks
// the "blocked" marker: busy/idle come from ~/.claude/sessions.
func claudeHook(r io.Reader) error {
	var p claudeHookPayload
	if err := json.NewDecoder(r).Decode(&p); err != nil || p.SessionID == "" {
		return nil // never break Claude over a bad payload
	}
	dir := filepath.Join(stateDir(), "claude-blocked")
	marker := filepath.Join(dir, p.SessionID)

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
