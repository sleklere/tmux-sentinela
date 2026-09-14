package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPaneOfFindsAgentInHiddenWindow(t *testing.T) {
	want := Pane{ID: "%2", PID: 100, Visible: false, Current: false}
	panes := map[int]Pane{want.PID: want}
	parents := map[int]int{300: 200, 200: want.PID}

	got, ok := paneOf(300, parents, panes)
	if !ok {
		t.Fatal("paneOf() did not find the hidden pane")
	}
	if got != want {
		t.Fatalf("paneOf() = %+v, want %+v", got, want)
	}
}

func TestPaneOfIgnoresUnrelatedProcesses(t *testing.T) {
	for _, parents := range []map[int]int{
		{},
		{300: 1},
		{300: 200, 200: 300},
	} {
		if pane, ok := paneOf(300, parents, map[int]Pane{100: {ID: "%1", PID: 100}}); ok || pane != (Pane{}) {
			t.Fatalf("parents=%v: paneOf() = %+v, %v", parents, pane, ok)
		}
	}
}

func TestSharedState(t *testing.T) {
	isolateState(t)
	key := "claude:100"
	if got := readSelection(); got != "" {
		t.Fatalf("missing selection = %q", got)
	}
	writeSelection(key)
	if got := readSelection(); got != key {
		t.Fatalf("selection = %q, want %q", got, key)
	}
	if _, ok := readSeen(key); ok {
		t.Fatal("missing seen timestamp was accepted")
	}
	writeTestFile(t, seenFile(key), []byte("corrupt"))
	if _, ok := readSeen(key); ok {
		t.Fatal("corrupt seen timestamp was accepted")
	}
	at := time.Unix(100, 123456789)
	writeSeen(key, at)
	if got, ok := readSeen(key); !ok || !got.Equal(at) {
		t.Fatalf("seen = %v, %v, want %v, true", got, ok, at)
	}
	removeSeen(key)
	if _, ok := readSeen(key); ok {
		t.Fatal("removed seen timestamp was returned")
	}
}

func TestReadClaudeSessionsChoosesNewestSource(t *testing.T) {
	for _, mirrorNewer := range []bool{false, true} {
		t.Run(fmt.Sprintf("mirrorNewer=%v", mirrorNewer), func(t *testing.T) {
			isolateState(t)
			native := claudeSession{PID: os.Getpid(), SessionID: "session", Name: "native", UpdatedAt: 20}
			mirror := native
			mirror.Name, mirror.UpdatedAt = "mirror", 10
			want := native
			if mirrorNewer {
				mirror.UpdatedAt = 30
				want = mirror
			}
			writeJSON(t, filepath.Join(claudeSessionsDir(), "native.json"), native)
			if err := writeClaudeSession(mirror); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(claudeSessionsDir(), "partial.json"), []byte("{"))
			writeJSON(t, filepath.Join(claudeSessionsDir(), "no-pid.json"), claudeSession{SessionID: "invalid"})
			if got := readClaudeSessions(); !reflect.DeepEqual(got, []claudeSession{want}) {
				t.Fatalf("sessions = %+v, want %+v", got, want)
			}
		})
	}
}

func TestStateReadersRemoveDeadMirrors(t *testing.T) {
	isolateState(t)
	// No process can have this PID on supported Linux/macOS hosts.
	const deadPID = 1<<31 - 1
	if pidAlive(deadPID) {
		t.Fatal("dead PID is unexpectedly alive")
	}
	for _, kind := range []string{"claude", "opencode"} {
		key := fmt.Sprintf("%s:%d", kind, deadPID)
		path := filepath.Join(stateDir(), kind, fmt.Sprintf("%d.json", deadPID))
		writeJSON(t, path, map[string]any{"pid": deadPID})
		writeSeen(key, time.Now())
		if kind == "claude" {
			if got := readClaudeSessions(); len(got) != 0 {
				t.Fatalf("dead Claude session returned: %+v", got)
			}
		} else if got := readOpencodeStates(); len(got) != 0 {
			t.Fatalf("dead OpenCode session returned: %+v", got)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale %s file not removed: %v", kind, err)
		}
		if _, ok := readSeen(key); ok {
			t.Fatalf("stale %s seen timestamp not removed", kind)
		}
	}
}

func TestCollectPanes(t *testing.T) {
	for _, tt := range []struct {
		name           string
		claudeStatus   string
		opencodeStatus string
		blocked        bool
		fallback       bool
		wantClaude     Status
		wantOpenCode   Status
	}{
		{"busy and blocked", "busy", "blocked", false, false, Busy, Blocked},
		{"permission overrides busy", "busy", "busy", true, false, Blocked, Busy},
		{"permission overrides idle", "idle", "idle", true, false, Blocked, Idle},
		{"idle and process fallback", "idle", "idle", false, true, Idle, Idle},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateState(t)
			pid := os.Getpid()
			updated := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
			claude := claudeSession{PID: pid, SessionID: "session", Status: tt.claudeStatus,
				Name: "renamed", CWD: "/work/claude-project", StatusUpdatedAt: updated.UnixMilli()}
			oc := opencodeState{PID: pid, Pane: "%open", Name: "New session - date", Status: tt.opencodeStatus,
				CWD: "/work/open-project", Updated: updated.UnixMilli()}
			panes := []Pane{{ID: "%open", PID: -1}, {ID: "%claude", PID: pid}}
			if tt.fallback {
				claude.Name, oc.Pane = "", ""
			}
			writeJSON(t, filepath.Join(claudeSessionsDir(), "session.json"), claude)
			writeJSON(t, filepath.Join(stateDir(), "opencode", "session.json"), oc)
			writeJSON(t, filepath.Join(stateDir(), "opencode", "no-pid.json"), opencodeState{})
			writeTestFile(t, filepath.Join(stateDir(), "opencode", "partial.json"), []byte("{"))
			if tt.blocked {
				writeTestFile(t, filepath.Join(stateDir(), "claude-blocked", claude.SessionID), nil)
			}
			wantClaude := Agent{Kind: "claude", Name: "renamed", Status: tt.wantClaude, PID: pid,
				Pane: panes[1], Key: fmt.Sprintf("claude:%d", pid), Since: updated}
			wantOpen := Agent{Kind: "opencode", Name: "open-project", Status: tt.wantOpenCode, PID: pid,
				Pane: panes[0], Key: fmt.Sprintf("opencode:%d", pid), Since: updated}
			if tt.blocked {
				_, wantClaude.Since = claudeBlocked(claude.SessionID)
			}
			want := []Agent{wantOpen, wantClaude}
			if tt.fallback {
				wantClaude.Name, wantOpen.Pane = "claude-project", panes[1]
				want = []Agent{wantClaude, wantOpen}
			}
			if got := collectPanes(panes); !reflect.DeepEqual(got, want) {
				t.Fatalf("agents = %+v, want %+v", got, want)
			}
			if got := collectPanes(nil); len(got) != 0 {
				t.Fatalf("agents outside tmux must be ignored: %+v", got)
			}
		})
	}
}
