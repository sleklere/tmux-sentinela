package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
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

func TestAtomicWriteNeverExposesPartialContent(t *testing.T) {
	isolateState(t)
	path := filepath.Join(stateDir(), "atomic")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	a := bytes.Repeat([]byte("a"), 32*1024)
	b := bytes.Repeat([]byte("b"), 32*1024)
	if err := atomicWriteFile(path, a, 0o600); err != nil {
		t.Fatal(err)
	}

	var writers sync.WaitGroup
	for i := range 4 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			value := a
			if i%2 == 1 {
				value = b
			}
			for range 50 {
				if err := atomicWriteFile(path, value, 0o600); err != nil {
					t.Errorf("atomicWriteFile: %v", err)
					return
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		writers.Wait()
		close(done)
	}()
	for {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, a) && !bytes.Equal(data, b) {
			t.Fatalf("reader observed partial content: %d bytes", len(data))
		}
		select {
		case <-done:
			return
		default:
		}
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