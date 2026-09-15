package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Status int

const (
	Idle Status = iota
	Busy
	Blocked // waiting for permission / user answer
)

type Agent struct {
	Kind   string // claude | opencode
	Name   string
	Status Status
	Since  time.Time // last status change
	PID    int
	Pane   Pane
	Key    string // stable identity across polls
}

// stateDir holds markers written by hooks and the OpenCode plugin.
func stateDir() string {
	return filepath.Join(cacheHome(), "tmux-sentinela")
}

func cacheHome() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache")
}

func claudeSessionsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "sessions")
}

// --- Claude Code: ~/.claude/sessions/<pid>.json, written by Claude itself.

type claudeSession struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	Name            string `json:"name"`
	Status          string `json:"status"` // idle | busy
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
	UpdatedAt       int64  `json:"updatedAt"`
	CWD             string `json:"cwd"`
}

func readClaudeSessions() []claudeSession {
	dirs := []string{claudeSessionsDir(), filepath.Join(stateDir(), "claude")}
	byPID := map[int]claudeSession{}
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var s claudeSession
			if json.Unmarshal(b, &s) != nil || s.PID == 0 {
				continue
			}
			if dir != claudeSessionsDir() && !pidAlive(s.PID) {
				os.Remove(f)
				removeSeen("claude:" + strconv.Itoa(s.PID))
				continue
			}
			if current, ok := byPID[s.PID]; !ok || s.UpdatedAt > current.UpdatedAt {
				byPID[s.PID] = s
			}
		}
	}
	out := make([]claudeSession, 0, len(byPID))
	for _, s := range byPID {
		out = append(out, s)
	}
	return out
}

// The selected agent is shared by every sidebar, so all of them mark the same
// row and a jump never lands on a window whose bar is somewhere else.
func selectionFile() string { return filepath.Join(stateDir(), "selected") }

func readSelection() string {
	b, err := os.ReadFile(selectionFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeSelection(key string) {
	if err := os.MkdirAll(stateDir(), 0o755); err != nil {
		return
	}
	atomicWriteFile(selectionFile(), []byte(key), 0o644)
}

func seenFile(key string) string { return filepath.Join(stateDir(), "seen", key) }

func readSeen(key string) (time.Time, bool) {
	b, err := os.ReadFile(seenFile(key))
	if err != nil {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, n), true
}

func writeSeen(key string, at time.Time) {
	dir := filepath.Join(stateDir(), "seen")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	atomicWriteFile(seenFile(key), []byte(strconv.FormatInt(at.UnixNano(), 10)), 0o600)
}

func removeSeen(key string) { os.Remove(seenFile(key)) }

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err = tmp.Chmod(perm); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func claudeBlocked(sessionID string) (bool, time.Time) {
	st, err := os.Stat(filepath.Join(stateDir(), "claude-blocked", sessionID))
	if err != nil {
		return false, time.Time{}
	}
	return true, st.ModTime()
}

// --- OpenCode: ~/.cache/tmux-sentinela/opencode/<pid>.json, written by the plugin.

type opencodeState struct {
	PID     int    `json:"pid"`
	Pane    string `json:"pane"`
	Name    string `json:"name"`
	Status  string `json:"status"` // idle | busy | blocked
	Updated int64  `json:"updated"`
	CWD     string `json:"cwd"`
}

func readOpencodeStates() []opencodeState {
	files, _ := filepath.Glob(filepath.Join(stateDir(), "opencode", "*.json"))
	var out []opencodeState
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s opencodeState
		if json.Unmarshal(b, &s) != nil || s.PID == 0 {
			continue
		}
		if !pidAlive(s.PID) {
			os.Remove(f)
			removeSeen("opencode:" + strconv.Itoa(s.PID))
			continue
		}
		out = append(out, s)
	}
	return out
}

// --- process tree (portable: signal 0 and ps work on Linux and macOS)

func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM) // EPERM: alive, not ours
}

// parentMap builds pid → ppid for every process.
func parentMap() map[int]int {
	out, _ := exec.Command("ps", "-eo", "pid,ppid").Output()
	m := map[int]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0]) // header line fails here
		if err != nil {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])
		m[pid] = ppid
	}
	return m
}

// paneOf walks up from pid until it meets a pane's shell pid.
func paneOf(pid int, parents map[int]int, byPID map[int]Pane) (Pane, bool) {
	for i := 0; i < 64 && pid > 1; i++ {
		if p, ok := byPID[pid]; ok {
			return p, true
		}
		pid = parents[pid]
	}
	return Pane{}, false
}

func printStatus() error {
	panes, err := listPanes()
	if err != nil {
		return err
	}
	for _, p := range panes {
		fmt.Printf("pane\t%s\t%s:%d.%d\tpid=%d\tsidebar=%v\tvisible=%v\n",
			p.ID, p.Session, p.WindowIndex, p.PaneIndex, p.PID, p.Sidebar, p.Visible)
	}
	agents, err := collect()
	if err != nil {
		return err
	}
	names := map[Status]string{Idle: "idle", Busy: "busy", Blocked: "blocked"}
	for _, a := range agents {
		fmt.Printf("agent\t%s\t%s\t%s\t%s\tpid=%d\tsince=%s\n",
			a.Pane.ID, a.Kind, names[a.Status], a.Name, a.PID, a.Since.Format("15:04:05"))
	}
	return nil
}

// collect gathers every agent visible in tmux, in tmux pane order.
func collect() ([]Agent, error) {
	panes, err := listPanes()
	if err != nil {
		return nil, err
	}
	return collectPanes(panes), nil
}

func collectPanes(panes []Pane) []Agent {
	byPID := map[int]Pane{}
	byID := map[string]Pane{}
	order := map[string]int{}
	for i, p := range panes {
		byPID[p.PID] = p
		byID[p.ID] = p
		order[p.ID] = i
	}
	parents := parentMap()
	var agents []Agent

	for _, s := range readClaudeSessions() {
		key := "claude:" + strconv.Itoa(s.PID)
		if !pidAlive(s.PID) {
			removeSeen(key)
			continue
		}
		pane, ok := paneOf(s.PID, parents, byPID)
		if !ok {
			continue
		}
		a := Agent{Kind: "claude", Name: s.Name, PID: s.PID, Pane: pane, Key: key}
		if s.StatusUpdatedAt > 0 { // absent until the first turn
			a.Since = time.UnixMilli(s.StatusUpdatedAt)
		}
		if s.Status == "busy" {
			a.Status = Busy
		}
		// Claude reports "idle" while a permission prompt is open, so the
		// marker wins over the session status.
		if blocked, at := claudeBlocked(s.SessionID); blocked {
			a.Status, a.Since = Blocked, at
		}
		if a.Name == "" {
			a.Name = filepath.Base(s.CWD)
		}
		agents = append(agents, a)
	}

	for _, s := range readOpencodeStates() {
		key := "opencode:" + strconv.Itoa(s.PID)
		pane, ok := byID[s.Pane]
		if !ok { // TMUX_PANE not inherited: walk the process tree instead
			if pane, ok = paneOf(s.PID, parents, byPID); !ok {
				continue
			}
		}
		a := Agent{Kind: "opencode", Name: s.Name, PID: s.PID, Pane: pane,
			Key: key, Since: time.UnixMilli(s.Updated)}
		switch s.Status {
		case "busy":
			a.Status = Busy
		case "blocked":
			a.Status = Blocked
		}
		// OpenCode titles sessions "New session - <date>" until it names them.
		if a.Name == "" || strings.HasPrefix(a.Name, "New session") {
			a.Name = filepath.Base(s.CWD)
		}
		agents = append(agents, a)
	}

	sort.SliceStable(agents, func(i, j int) bool {
		return order[agents[i].Pane.ID] < order[agents[j].Pane.ID]
	})
	return agents
}
