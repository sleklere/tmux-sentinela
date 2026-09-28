package main

import (
	"errors"
	"testing"
	"time"
)

func TestRemoteCacheSharedBetweenSidebars(t *testing.T) {
	isolatedTmux(t)
	now := time.Now()
	saveRemote(remoteResult{host: "Dojo", at: now, agents: []Agent{{Host: "Dojo", Key: "remote:Dojo:pi:1", Name: "agent", Status: Blocked}}})
	got, ok := loadRemote("Dojo")
	if !ok || got.err != nil || len(got.agents) != 1 || got.agents[0].Status != Blocked {
		t.Fatalf("shared snapshot = %+v, %v", got, ok)
	}
	if _, ok := loadRemote("Argos"); ok {
		t.Fatal("one host read another host's snapshot")
	}
	saveRemote(remoteResult{host: "Dojo", at: now.Add(time.Second), agents: got.agents, err: errors.New("auth failed")})
	got, ok = loadRemote("Dojo")
	if !ok || got.err == nil || got.err.Error() != "auth failed" || len(got.agents) != 1 {
		t.Fatalf("cached failure = %+v, %v", got, ok)
	}
	saveRemote(remoteResult{host: "Dojo", at: now.Add(-time.Minute), agents: got.agents})
	got, ok = loadRemote("Dojo")
	if !ok || got.err == nil || got.err.Error() != "unavailable" {
		t.Fatalf("expired snapshot = %+v, %v", got, ok)
	}
}

func TestFollowerLoadsLeaderSnapshotWithoutPolling(t *testing.T) {
	isolatedTmux(t)
	saveRemote(remoteResult{host: "Dojo", at: time.Now(), agents: []Agent{{Host: "Dojo", Key: "remote:Dojo:pi:1", Name: "agent", Status: Blocked}}})
	m := newModel("")
	m.self = "%3"
	m.hosts = []string{"Dojo"}
	updated, cmd := m.Update(pollMsg{sequence: 1, leader: false})
	m = updated.(model)
	if cmd != nil || len(m.agents) != 1 || m.agents[0].Name != "agent" || m.remotePending["Dojo"] {
		t.Fatalf("follower = %+v, scheduled=%v", m.agents, cmd != nil)
	}
}

func TestOnlyLeaderPollsRemotes(t *testing.T) {
	m := newModel("")
	m.self = "%3"
	m.hosts = []string{"Dojo", "Argos"}
	if cmd := m.pollRemotes(time.Now()); cmd != nil || len(m.remotePending) != 0 {
		t.Fatalf("follower scheduled remote polls: %v", m.remotePending)
	}
	m.leader = true
	if cmd := m.pollRemotes(time.Now()); cmd == nil || len(m.remotePending) != 2 {
		t.Fatalf("leader missed remote polls: %v", m.remotePending)
	}
}
