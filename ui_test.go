package main

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestPulseColor(t *testing.T) {
	from := lipgloss.Color("#ffd166")
	to := lipgloss.Color("#ff8a1f")

	if got := pulseColor(from, to, 0); got != from {
		t.Fatalf("frame 0 = %q, want %q", got, from)
	}
	if got := pulseColor(from, to, busyCycleFrames/2); got != to {
		t.Fatalf("middle frame = %q, want %q", got, to)
	}
	if left, right := pulseColor(from, to, 3), pulseColor(from, to, busyCycleFrames-3); left != right {
		t.Fatalf("pulse is not symmetric: %q != %q", left, right)
	}
}

func TestPulseColorKeepsUnsupportedColor(t *testing.T) {
	from := lipgloss.Color("3")
	if got := pulseColor(from, "#ff8a1f", 4); got != from {
		t.Fatalf("pulseColor() = %q, want %q", got, from)
	}
}

func TestBusyPulseFollowsTheme(t *testing.T) {
	yellow := loadTheme(map[string]string{"@th_accent3": "#eed49f"})
	teal := loadTheme(map[string]string{"@th_accent3": "#8bd5ca"})

	if yellow.busy != "#eed49f" || teal.busy != "#8bd5ca" {
		t.Fatalf("busy colors do not follow accent3: %q, %q", yellow.busy, teal.busy)
	}
	if yellow.busyGlow == teal.busyGlow {
		t.Fatalf("derived glow does not follow the theme: %q", yellow.busyGlow)
	}
}

func TestDoneStateSurvivesNewSidebar(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	now := time.Now()
	agent := Agent{Key: "claude:100", Status: Idle, Since: now.Add(-time.Minute)}
	writeSeen(agent.Key, now.Add(-2*time.Minute))

	m := model{seen: map[string]time.Time{}, started: now, prev: map[string]Status{}}
	m.applyPoll(pollMsg{agents: []Agent{agent}})

	if !m.done(agent) {
		t.Fatal("new sidebar lost the shared done state")
	}
}

func TestSidebarLoadsSharedDoneStateAfterStarting(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	now := time.Now()
	agent := Agent{Key: "claude:101", Status: Idle, Since: now.Add(-time.Minute)}
	m := model{seen: map[string]time.Time{}, started: now, prev: map[string]Status{}}

	m.applyPoll(pollMsg{agents: []Agent{agent}})
	writeSeen(agent.Key, now.Add(-2*time.Minute))
	m.applyPoll(pollMsg{agents: []Agent{agent}})

	if !m.done(agent) {
		t.Fatal("running sidebar did not load the shared done state")
	}
}

func TestViewingAgentClearsDoneInEverySidebar(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	now := time.Now()
	agent := Agent{Key: "opencode:200", Status: Idle, Since: now.Add(-time.Minute),
		Pane: Pane{Visible: true}}

	viewer := model{seen: map[string]time.Time{}, started: now, prev: map[string]Status{}}
	viewer.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})

	agent.Pane.Visible = false
	other := model{seen: map[string]time.Time{}, started: now.Add(time.Second), prev: map[string]Status{}}
	other.applyPoll(pollMsg{agents: []Agent{agent}})
	if other.done(agent) {
		t.Fatal("done state remained after another sidebar saw the agent")
	}
}
