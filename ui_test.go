package main

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func testModel() model {
	return model{th: loadTheme(nil), notify: "off", width: 32,
		seen: map[string]time.Time{}, started: time.Now(), prev: map[string]Status{}}
}

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

func TestBusyPulseChangesOnRefresh(t *testing.T) {
	m := testModel()
	m.frame = ticksPerRefresh - 1
	_, before := m.glyph(Agent{Status: Busy})
	m.frame++
	_, after := m.glyph(Agent{Status: Busy})
	if before == after {
		t.Fatal("busy pulse did not change on refresh")
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

func TestSidebarLoadsSharedDoneState(t *testing.T) {
	for _, alreadyRunning := range []bool{false, true} {
		name := "new sidebar"
		if alreadyRunning {
			name = "running sidebar"
		}
		t.Run(name, func(t *testing.T) {
			isolateState(t)
			m := testModel()
			agent := Agent{Key: "claude:100", Status: Idle, Since: m.started.Add(-time.Minute)}
			if alreadyRunning {
				m.applyPoll(pollMsg{agents: []Agent{agent}})
				if m.done(agent) {
					t.Fatal("unknown history must not be marked done")
				}
			}
			writeSeen(agent.Key, m.started.Add(-2*time.Minute))
			m.applyPoll(pollMsg{agents: []Agent{agent}})
			if !m.done(agent) {
				t.Fatal("sidebar did not load the shared done state")
			}
		})
	}
}

func TestViewingAgentClearsDoneInEverySidebar(t *testing.T) {
	isolateState(t)
	viewer, other := testModel(), testModel()
	agent := Agent{Key: "opencode:200", Status: Idle, Since: viewer.started.Add(-time.Minute)}
	writeSeen(agent.Key, viewer.started.Add(-2*time.Minute))
	viewer.applyPoll(pollMsg{agents: []Agent{agent}})
	other.applyPoll(pollMsg{agents: []Agent{agent}})
	if !viewer.done(agent) || !other.done(agent) {
		t.Fatal("both sidebars must initially show done")
	}
	agent.Pane.Visible = true
	viewer.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	agent.Pane.Visible = false
	other.applyPoll(pollMsg{agents: []Agent{agent}})
	newSidebar := testModel()
	newSidebar.applyPoll(pollMsg{agents: []Agent{agent}})
	if viewer.done(agent) || other.done(agent) || newSidebar.done(agent) {
		t.Fatal("done state remained after another sidebar saw the agent")
	}
}

func TestCursorVisibility(t *testing.T) {
	for _, tt := range []struct {
		name   string
		window string
		active bool
		want   int
	}{
		{"empty window", "@empty", false, -1},
		{"agent window", "@agent", false, 0},
		{"focused empty sidebar", "@empty", true, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateState(t)
			m := testModel()
			m.cursor = -1
			agent := Agent{Key: "claude:100", Pane: Pane{WindowID: "@agent"}}
			m.applyPoll(pollMsg{agents: []Agent{agent}, window: tt.window, active: tt.active, sel: agent.Key})
			if m.cursor != tt.want {
				t.Fatalf("cursor = %d, want %d", m.cursor, tt.want)
			}
			if got := strings.Contains(m.View(), "▌"); got != (tt.want >= 0) {
				t.Fatalf("visible bar = %v, cursor = %d", got, m.cursor)
			}
		})
	}
}

func TestKeyboardNavigation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		key    tea.KeyMsg
		cursor int
		want   int
	}{
		{"down from hidden", tea.KeyMsg{Type: tea.KeyDown}, -1, 0},
		{"j", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, 0, 1},
		{"k", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, 1, 0},
		{"up from hidden", tea.KeyMsg{Type: tea.KeyUp}, -1, -1},
		{"first row", tea.KeyMsg{Type: tea.KeyUp}, 0, 0},
		{"last row", tea.KeyMsg{Type: tea.KeyDown}, 1, 1},
		{"enter from hidden", tea.KeyMsg{Type: tea.KeyEnter}, -1, -1},
		{"l from hidden", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}, -1, -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateState(t)
			m := testModel()
			m.agents = []Agent{{Key: "claude:100"}, {Key: "opencode:200"}}
			m.cursor = tt.cursor
			updated, _ := m.Update(tt.key)
			if got := updated.(model).cursor; got != tt.want {
				t.Fatalf("cursor = %d, want %d", got, tt.want)
			}
			wantSelection := ""
			if tt.cursor != tt.want {
				wantSelection = m.agents[tt.want].Key
			}
			if got := readSelection(); got != wantSelection {
				t.Fatalf("shared selection = %q, want %q", got, wantSelection)
			}
		})
	}
}

func TestFocusPollPreservesManualSelection(t *testing.T) {
	isolateState(t)
	m := testModel()
	m.self, m.window = "%sidebar", "@empty"
	m.agents = []Agent{
		{Key: "claude:100", Pane: Pane{ID: "%1", WindowID: "@agent"}},
		{Key: "opencode:200", Pane: Pane{ID: "%2", WindowID: "@agent"}},
	}
	for _, active := range []bool{false, true, true, false} {
		if active {
			m.moveCursor(1)
		}
		updated, _ := m.Update(focusMsg{panes: map[string]Pane{m.self: {Visible: active}}, sel: readSelection()})
		m = updated.(model)
		want := -1
		if active {
			want = 1
		}
		if m.cursor != want {
			t.Fatalf("sidebar active=%v: cursor=%d, want %d", active, m.cursor, want)
		}
	}
}

func TestFocusPollUpdatesSidebarWindowVisibility(t *testing.T) {
	m := testModel()
	m.self = "%sidebar"
	updated, _ := m.Update(focusMsg{panes: map[string]Pane{
		"%sidebar": {Current: true},
	}})
	if !updated.(model).current {
		t.Fatal("sidebar window should be current")
	}
}

func TestFocusPollFollowsActivePaneBeforeCurrentWindow(t *testing.T) {
	isolateState(t)
	m := testModel()
	m.window = "@agent"
	m.agents = []Agent{
		{Key: "claude:100", Pane: Pane{ID: "%1", WindowID: "@agent"}},
		{Key: "opencode:200", Pane: Pane{ID: "%2", WindowID: "@agent"}},
	}
	focus := focusMsg{panes: map[string]Pane{"%1": {Current: true}, "%2": {Current: true, Visible: true}}}
	updated, _ := m.Update(focus)
	m = updated.(model)
	if m.cursor != 1 || readSelection() != "opencode:200" {
		t.Fatal("active pane did not win over current window")
	}
	m.moveCursor(0)
	focus.sel = readSelection()
	updated, _ = m.Update(focus)
	if updated.(model).cursor != 0 {
		t.Fatal("unchanged focus overwrote manual navigation")
	}
	focus.panes["%2"] = Pane{}
	updated, _ = updated.Update(focus)
	if updated.(model).focused != "claude:100" {
		t.Fatal("did not fall back to the agent in the current window")
	}
}

func TestPollKeepsSelectionAcrossReorderAndRemoval(t *testing.T) {
	isolateState(t)
	m := testModel()
	a := Agent{Key: "claude:100", Pane: Pane{WindowID: "@agent"}}
	b := Agent{Key: "opencode:200", Pane: Pane{WindowID: "@agent"}}
	m.applyPoll(pollMsg{agents: []Agent{a, b}, window: "@agent", sel: b.Key})
	if m.cursor != 1 {
		t.Fatal("second agent was not selected")
	}
	m.applyPoll(pollMsg{agents: []Agent{b, a}, window: "@agent", sel: b.Key})
	if m.cursor != 0 {
		t.Fatal("selection followed the index instead of the agent")
	}
	m.moveCursor(1)
	m.applyPoll(pollMsg{agents: []Agent{b}, window: "@agent", sel: a.Key})
	if m.cursor != 0 {
		t.Fatalf("cursor after selected agent removal = %d, want 0", m.cursor)
	}
	m.applyPoll(pollMsg{window: "@agent"})
	if m.cursor != -1 || len(m.prev) != 0 {
		t.Fatal("empty poll retained selection or previous agents")
	}
}

func TestViewStates(t *testing.T) {
	m := testModel()
	if got := m.View(); !strings.Contains(got, "no agents running") {
		t.Fatalf("empty view = %q", got)
	}
	err := errors.New("server unavailable")
	updated, _ := m.Update(pollMsg{err: err})
	if got := updated.View(); !strings.Contains(got, "tmux: server unavailable") {
		t.Fatalf("error view = %q", got)
	}
	updated, _ = updated.Update(tea.WindowSizeMsg{})
	if got := updated.View(); got != "" {
		t.Fatalf("view before layout = %q", got)
	}
}

func TestViewAgentRows(t *testing.T) {
	m := testModel()
	m.cursor = 1
	m.agents = []Agent{
		{Key: "claude:100", Kind: "claude", Name: "done", Status: Idle,
			Since: m.started, Pane: Pane{Session: "work", WindowIndex: 1}},
		{Key: "opencode:200", Kind: "opencode", Name: "blocked", Status: Blocked,
			Pane: Pane{Session: "code", WindowIndex: 2}},
	}
	m.seen["claude:100"] = m.started.Add(-time.Minute)

	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(m.View(), "")
	want := " agents 1\n\n" +
		"  ✓ done\n" +
		"    claude  work:1\n" +
		"▌ ● blocked\n" +
		"▌   opencode  code:2\n"
	if plain != want {
		t.Fatalf("view =\n%q\nwant =\n%q", plain, want)
	}
}

func TestTruncateBounds(t *testing.T) {
	for _, tt := range []struct {
		n    int
		want string
	}{
		{-1, ""},
		{0, ""},
		{1, "…"},
		{2, "a…"},
		{3, "abc"},
	} {
		if got := truncate("abc", tt.n); got != tt.want {
			t.Errorf("truncate(abc, %d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestStatusGlyphs(t *testing.T) {
	m := testModel()
	for _, tt := range []struct {
		name   string
		status Status
		seen   time.Time
		glyph  string
		color  lipgloss.Color
	}{
		{"blocked", Blocked, m.started, "●", m.th.alert},
		{"busy", Busy, m.started, "●", m.th.busy},
		{"done", Idle, m.started.Add(-time.Minute), "✓", m.th.accent},
		{"seen", Idle, m.started, "○", m.th.muted},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := Agent{Key: "agent", Status: tt.status, Since: m.started}
			m.seen[a.Key] = tt.seen
			if glyph, color := m.glyph(a); glyph != tt.glyph || color != tt.color {
				t.Fatalf("glyph = %q, %q; want %q, %q", glyph, color, tt.glyph, tt.color)
			}
		})
	}
}
