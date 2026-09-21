package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

const refreshFileName = "refresh"

type refreshMsg struct{}
type refreshWatchStoppedMsg struct{}

func refreshFile() string { return filepath.Join(stateDir(), refreshFileName) }

// publishRefresh wakes every sidebar watching the shared state directory.
// Polling remains as a fallback when the watcher or an event is unavailable.
func publishRefresh() error {
	if err := os.MkdirAll(stateDir(), 0o755); err != nil {
		return err
	}
	return atomicWriteFile(refreshFile(), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0o600)
}

func newRefreshWatcher() (*fsnotify.Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	for _, dir := range watchedStateDirs() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			w.Close()
			return nil, err
		}
		if err := w.Add(dir); err != nil {
			w.Close()
			return nil, err
		}
	}
	return w, nil
}

func watchedStateDirs() []string {
	return []string{
		stateDir(),
		filepath.Join(stateDir(), "claude"),
		filepath.Join(stateDir(), "claude-blocked"),
		filepath.Join(stateDir(), "opencode"),
		filepath.Join(stateDir(), "pi"),
	}
}

func isRefreshEvent(path string) bool {
	path = filepath.Clean(path)
	if path == refreshFile() || path == selectionFile() {
		return true
	}
	base, dir := filepath.Base(path), filepath.Dir(path)
	if strings.HasPrefix(base, ".") || strings.HasSuffix(base, ".tmp") {
		return false
	}
	switch dir {
	case filepath.Join(stateDir(), "claude"), filepath.Join(stateDir(), "opencode"), filepath.Join(stateDir(), "pi"):
		return strings.HasSuffix(base, ".json")
	case filepath.Join(stateDir(), "claude-blocked"):
		return true
	default:
		return false
	}
}

func watchRefresh(w *fsnotify.Watcher) tea.Cmd {
	return func() tea.Msg {
		events, errs := w.Events, w.Errors
		for events != nil {
			select {
			case event, ok := <-events:
				if !ok {
					return refreshWatchStoppedMsg{}
				}
				if isRefreshEvent(event.Name) {
					return refreshMsg{}
				}
			case _, ok := <-errs:
				if !ok {
					errs = nil
				}
			}
		}
		return refreshWatchStoppedMsg{}
	}
}
