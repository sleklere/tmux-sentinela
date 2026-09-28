package main

import (
	"encoding/json"
	"io"
	"time"
)

// Wire status intentionally excludes visually inferred agents: only local
// integrations can provide an authoritative status for remote consumers.
const statusVersion = 1

type statusDocument struct {
	Version int           `json:"version"`
	Agents  []statusAgent `json:"agents"`
}

type statusAgent struct {
	Key      string    `json:"key"`
	Kind     string    `json:"kind"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Since    time.Time `json:"since"`
	PID      int       `json:"pid"`
	PaneID   string    `json:"pane_id"`
	Session  string    `json:"session"`
	Window   int       `json:"window"`
	Pane     int       `json:"pane"`
	Visible  bool      `json:"visible"`
	Current  bool      `json:"current"`
	Duration int64     `json:"duration_seconds"`
}

func localStatus(agents []Agent, now time.Time) statusDocument {
	out := statusDocument{Version: statusVersion, Agents: []statusAgent{}}
	statuses := [...]string{"idle", "busy", "blocked"}
	for _, a := range agents {
		if a.Visual {
			continue
		}
		duration := int64(0)
		if !a.Since.IsZero() && now.After(a.Since) {
			duration = int64(now.Sub(a.Since).Seconds())
		}
		out.Agents = append(out.Agents, statusAgent{
			Key: a.Key, Kind: a.Kind, Name: a.Name, Status: statuses[a.Status],
			Since: a.Since, PID: a.PID, PaneID: a.Pane.ID, Session: a.Pane.Session,
			Window: a.Pane.WindowIndex, Pane: a.Pane.PaneIndex,
			Visible: a.Pane.Visible, Current: a.Pane.Current, Duration: duration,
		})
	}
	return out
}

func printStatusJSON(w io.Writer) error {
	// collectPanes does not need an attached client, so this also works over SSH.
	panes, err := listPanes()
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(localStatus(collectPanesWithVisual(panes, false), time.Now()))
}
