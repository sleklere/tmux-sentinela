package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRemoteUpdateKeepsLastResultWithoutLiveBlockedCount(t *testing.T) {
	isolateState(t)
	m := newModel("")
	m.hosts = []string{"dojo"}
	m.leader = true
	m.width, m.height = 50, 20
	m.local = []Agent{{Kind: "pi", Name: "local", Key: "pi:1", Pane: Pane{ID: "%1"}}}
	a := Agent{Host: "dojo", Kind: "claude", Name: "remote", Status: Blocked, Key: "remote:dojo:claude:1", Pane: Pane{ID: "%1", Session: "dev"}}
	updated, _ := m.Update(remoteResult{host: "dojo", agents: []Agent{a}})
	m = updated.(model)
	if len(m.agents) != 2 || countStatus(m.agents, Blocked) != 1 {
		t.Fatalf("agents after pull = %+v", m.agents)
	}
	updated, _ = m.Update(remoteResult{host: "dojo", err: errors.New("auth failed")})
	m = updated.(model)
	if len(m.agents) != 2 || countStatus(m.agents, Blocked) != 0 || !m.agents[1].Stale || !strings.Contains(m.View(), "dojo: auth failed") {
		t.Fatalf("offline state = %+v, view=%s", m.agents, m.View())
	}
	if !m.remoteRetry["dojo"].After(time.Now().Add(4 * time.Minute)) {
		t.Fatal("auth failure did not back off")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(model)
	if !m.remotePending["dojo"] || !m.remoteRetry["dojo"].IsZero() {
		t.Fatal("manual retry did not reset authentication backoff")
	}
}

func TestRemoteVisibilityIsPerAgentPane(t *testing.T) {
	isolateState(t)
	m := newModel("")
	m.hosts = []string{"dojo"}
	m.localPanes = []Pane{{ID: "%5", WindowID: "@9", RemoteHost: "dojo", RemoteSession: "dev", Command: "ssh", Visible: true, Current: true}}
	m.window = "@9"
	m.active = true
	m.remotes["dojo"] = remoteResult{agents: []Agent{
		{Key: "remote:dojo:a", Host: "dojo", Pane: Pane{ID: "%1", Session: "dev", Visible: true, Current: true}},
		{Key: "remote:dojo:b", Host: "dojo", Pane: Pane{ID: "%2", Session: "dev"}},
	}}
	m.applyPoll(pollMsg{localPanes: m.localPanes, window: "@9", active: true})
	if !m.agents[0].Pane.Visible || m.agents[1].Pane.Visible || m.agents[1].Pane.Current {
		t.Fatalf("remote pane visibility: %+v", m.agents)
	}
}

func TestRemoteBlockedTransitionNotifiesOnArrival(t *testing.T) {
	isolateState(t)
	m := newModel("")
	m.hosts = []string{"dojo"}
	m.notify = "tmux"
	m.leader = true
	a := Agent{Key: "remote:dojo:pi:1", Host: "dojo", Status: Busy, Pane: Pane{ID: "%1", Session: "dev"}}
	m.remotes["dojo"] = remoteResult{agents: []Agent{a}}
	m.applyPoll(pollMsg{leader: true})
	a.Status = Blocked
	m.Update(remoteResult{host: "dojo", agents: []Agent{a}})
	if _, ok := readNotified(a.Key, "blocked"); !ok {
		t.Fatal("remote blocked transition was consumed without notification")
	}
}

func TestRemotePollCommandsRemainIndependent(t *testing.T) {
	m := newModel("")
	m.hosts = []string{"dojo", "argos"}
	m.leader = true
	m.remotePending["dojo"] = true
	if cmd := m.pollRemotes(time.Now()); cmd == nil || !m.remotePending["argos"] {
		t.Fatal("one slow host blocked another")
	}
}
