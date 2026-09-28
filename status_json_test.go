package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestLocalStatusWire(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pane := Pane{ID: "%1", Session: "work", WindowIndex: 2, PaneIndex: 3}
	agents := []Agent{
		{Key: "pi:one:42", Kind: "pi", Name: "test", Status: Blocked, Since: now.Add(-90 * time.Second), PID: 42, Pane: pane},
		{Key: "claude-screen:one:%2", Visual: true, Pane: Pane{ID: "%2"}},
	}
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(localStatus(agents, now)); err != nil {
		t.Fatal(err)
	}
	var got statusDocument
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Agents) != 1 {
		t.Fatalf("wire document: %s", b.String())
	}
	a := got.Agents[0]
	if a.Status != "blocked" || a.Duration != 90 || a.PaneID != "%1" || a.Session != "work" || a.Window != 2 || a.Pane != 3 || a.Key != "pi:one:42" {
		t.Fatalf("wire agent: %+v", a)
	}
}

func TestStatusJSONOnSeparateTmuxServer(t *testing.T) {
	isolatedTmux(t)
	var b bytes.Buffer
	if err := printStatusJSON(&b); err != nil {
		t.Fatal(err)
	}
	var got statusDocument
	if err := json.Unmarshal(b.Bytes(), &got); err != nil || got.Version != 1 || got.Agents == nil {
		t.Fatalf("status --json: %s; error: %v", b.String(), err)
	}
}
