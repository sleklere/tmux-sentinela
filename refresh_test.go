package main

import (
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPublishRefreshWakesWatcher(t *testing.T) {
	isolateState(t)
	watcher, err := newRefreshWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	result := make(chan any, 1)
	go func() { result <- watchRefresh(watcher)() }()
	if err := publishRefresh(); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-result:
		if _, ok := msg.(refreshMsg); !ok {
			t.Fatalf("watch result = %T, want refreshMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh event was not delivered")
	}
}

func TestAgentStateWakesWatcher(t *testing.T) {
	for _, kind := range []string{"opencode", "pi"} {
		t.Run(kind, func(t *testing.T) {
			isolateState(t)
			watcher, err := newRefreshWatcher()
			if err != nil {
				t.Fatal(err)
			}
			defer watcher.Close()

			result := make(chan any, 1)
			go func() { result <- watchRefresh(watcher)() }()
			path := filepath.Join(stateDir(), kind, "123.json")
			if err := atomicWriteFile(path, []byte(`{"pid":123}`), 0o600); err != nil {
				t.Fatal(err)
			}
			select {
			case msg := <-result:
				if _, ok := msg.(refreshMsg); !ok {
					t.Fatalf("watch result = %T, want refreshMsg", msg)
				}
			case <-time.After(time.Second):
				t.Fatal("agent state event was not delivered")
			}
		})
	}
}

func TestClosedWatcherStopsRefreshCommand(t *testing.T) {
	isolateState(t)
	watcher, err := newRefreshWatcher()
	if err != nil {
		t.Fatal(err)
	}
	if err := watcher.Close(); err != nil {
		t.Fatal(err)
	}
	msg := watchRefresh(watcher)()
	if _, ok := msg.(refreshWatchStoppedMsg); !ok {
		t.Fatalf("watch result = %T, want refreshWatchStoppedMsg", msg)
	}
	m := testModel()
	m.watchRefresh = func() tea.Msg { return nil }
	updated, _ := m.Update(msg)
	if updated.(model).watchRefresh != nil {
		t.Fatal("closed watcher remained armed")
	}
}
