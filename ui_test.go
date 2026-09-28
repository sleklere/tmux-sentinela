package main

import (
	"errors"
	"os"
	"path/filepath"
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
	m.frame = ticksPerPulse - 1
	_, before := m.glyph(Agent{Status: Busy})
	m.frame++
	_, after := m.glyph(Agent{Status: Busy})
	if before == after {
		t.Fatal("busy pulse did not change on refresh")
	}
}

func TestBusyPulseIsSynchronizedAcrossSidebars(t *testing.T) {
	first, second := testModel(), testModel()
	agent := Agent{Status: Busy}
	at := time.Unix(1000, 0)
	// Sidebars started and received ticks at different times.
	first.frame = pulseTick(at.Add(-3 * time.Second))
	second.frame = pulseTick(at.Add(-time.Second))
	for _, instant := range []time.Time{at, at.Add(270 * time.Millisecond), at.Add(2 * time.Second)} {
		updated, _ := first.Update(tickMsg(instant))
		first = updated.(model)
		updated, _ = second.Update(tickMsg(instant))
		second = updated.(model)
		_, left := first.glyph(agent)
		_, right := second.glyph(agent)
		if left != right {
			t.Fatalf("pulse at %v differs between sidebars: %q, %q", instant, left, right)
		}
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

func TestBlockedBackgroundTintsThemeBase(t *testing.T) {
	th := loadTheme(map[string]string{"@th_alert": "#ff0000", "@th_base": "#000000"})
	if th.blockedBg != "#4d0000" {
		t.Fatalf("blockedBg = %q, want alert mixed into base", th.blockedBg)
	}
	own := loadTheme(map[string]string{"@th_alert": "#ff0000", "@sentinela_color_blocked_bg": "#123456"})
	if own.blockedBg != "#123456" {
		t.Fatalf("explicit blockedBg = %q", own.blockedBg)
	}
	named := loadTheme(map[string]string{"@th_alert": "red"})
	if named.blockedBg != "red" {
		t.Fatalf("non-hex alert blockedBg = %q, want the alert itself", named.blockedBg)
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

func TestScreenStatusTimesFollowVisualTransitions(t *testing.T) {
	isolateState(t)
	m := testModel()
	agent := Agent{Key: "claude-screen:%1", Kind: "claude", Status: Idle, Visual: true}
	m.applyPoll(pollMsg{agents: []Agent{agent}})
	if got := m.agents[0].Since; !got.Equal(m.started) {
		t.Fatalf("initial since = %v, want sidebar start %v", got, m.started)
	}
	first := m.agents[0].Since
	m.applyPoll(pollMsg{agents: []Agent{agent}})
	if got := m.agents[0].Since; !got.Equal(first) {
		t.Fatalf("stable status since = %v, want %v", got, first)
	}
	agent.Status = Busy
	m.applyPoll(pollMsg{agents: []Agent{agent}})
	if got := m.agents[0].Since; !got.After(first) {
		t.Fatalf("transition since = %v, want after %v", got, first)
	}

	native := Agent{Key: "claude:100", Kind: "claude", Status: Idle}
	m.applyPoll(pollMsg{agents: []Agent{native}})
	if got := m.agents[0].Since; !got.IsZero() {
		t.Fatalf("native agent received a visual timestamp: %v", got)
	}
}

func TestCompletionSoundTransitions(t *testing.T) {
	agent := Agent{Status: Idle}
	for _, tt := range []struct {
		name     string
		previous Status
		known    bool
		leader   bool
		mode     string
		visible  bool
		want     bool
	}{
		{"busy finishes", Busy, true, true, "on", false, true},
		{"blocked finishes", Blocked, true, true, "on", false, true},
		{"first observation", Busy, false, true, "on", false, false},
		{"already idle", Idle, true, true, "on", false, false},
		{"visible pane", Busy, true, true, "on", true, true},
		{"follower sidebar", Busy, true, false, "on", false, false},
		{"disabled", Busy, true, true, "off", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			agent.Pane.Visible = tt.visible
			if got := shouldPlayCompletionSound(tt.previous, tt.known, agent, tt.leader, tt.mode); got != tt.want {
				t.Fatalf("shouldPlayCompletionSound() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompletionNotificationTransitions(t *testing.T) {
	agent := Agent{Status: Idle}
	for _, tt := range []struct {
		name     string
		previous Status
		known    bool
		leader   bool
		mode     string
		visible  bool
		want     bool
	}{
		{"background completion", Busy, true, true, "desktop", false, true},
		{"blocked completion", Blocked, true, true, "tmux", false, true},
		{"both mode", Busy, true, true, "both", false, true},
		{"active pane", Busy, true, true, "desktop", true, false},
		{"first observation", Busy, false, true, "desktop", false, false},
		{"follower sidebar", Busy, true, false, "desktop", false, false},
		{"disabled", Busy, true, true, "off", false, false},
		{"default disabled", Busy, true, true, "", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			agent.Pane.Visible = tt.visible
			if got := shouldNotifyCompletion(tt.previous, tt.known, agent, tt.leader, tt.mode); got != tt.want {
				t.Fatalf("shouldNotifyCompletion() = %v, want %v", got, tt.want)
			}
		})
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
		{"inactive agent window without focused agent", "@agent", false, -1},
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

func TestPollFollowsActivePaneBeforeCurrentWindow(t *testing.T) {
	isolateState(t)
	m := testModel()
	m.window = "@agent"
	agents := []Agent{
		{Key: "claude:100", Pane: Pane{ID: "%1", WindowID: "@agent"}},
		{Key: "opencode:200", Pane: Pane{ID: "%2", WindowID: "@agent", Current: true, Visible: true}},
	}
	writeSelection("claude:100")
	m.applyPoll(pollMsg{agents: agents, window: "@agent", sel: readSelection()})
	if m.cursor != 1 {
		t.Fatal("active pane did not win over current window")
	}
	m.applyPoll(pollMsg{agents: agents, window: "@agent", sel: readSelection()})
	if m.cursor != 1 {
		t.Fatal("stale shared selection overwrote stable focus")
	}
	m.moveCursor(0)
	m.applyPoll(pollMsg{agents: agents, window: "@agent", sel: readSelection()})
	if m.cursor != 0 {
		t.Fatal("unchanged focus overwrote manual navigation")
	}
	agents[0].Pane.Current = true
	agents[1].Pane.Current, agents[1].Pane.Visible = false, false
	m.applyPoll(pollMsg{agents: agents, window: "@agent", sidebarFocused: true})
	if m.focused != "claude:100" {
		t.Fatal("did not fall back to the agent in the current window")
	}
}

func TestCursorDoesNotFlashLastAgentAfterPlainWindow(t *testing.T) {
	isolateState(t)
	m := testModel()
	m.window = "@pstack"
	agents := []Agent{
		{Key: "pi:old", Pane: Pane{WindowID: "@old", Current: true, Visible: true}},
		{Key: "claude:pstack", Pane: Pane{WindowID: "@pstack"}},
	}
	m.applyPoll(pollMsg{agents: agents, window: "@pstack"})
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want last focused agent", m.cursor)
	}
	// The client moves to btop; this sidebar belongs to pstack and stays open.
	agents[0].Pane.Current, agents[0].Pane.Visible = false, false
	m.applyPoll(pollMsg{agents: agents, window: "@pstack"})
	if m.cursor != -1 {
		t.Fatalf("btop left the cursor on the previous agent: %d", m.cursor)
	}
	// The client returns to the agent window by normal tmux navigation.
	agents[1].Pane.Current, agents[1].Pane.Visible = true, true
	m.applyPoll(pollMsg{agents: agents, window: "@pstack"})
	if m.cursor != 1 {
		t.Fatalf("cursor after entering pstack = %d, want 1", m.cursor)
	}
}

func TestPollDoesNotFollowAgentInWindowWhenPlainPaneIsActive(t *testing.T) {
	isolateState(t)
	m := testModel()
	m.window = "@work"
	agents := []Agent{
		{Key: "claude:100", Pane: Pane{WindowID: "@work", Current: true}},
		{Key: "opencode:200", Pane: Pane{WindowID: "@other"}},
	}
	m.applyPoll(pollMsg{agents: agents, window: "@work", sel: "opencode:200"})
	if m.cursor != -1 {
		t.Fatal("plain pane left a cursor on an unfocused agent")
	}
	agents[0].Pane.Current = false
	agents[1].Pane.Current, agents[1].Pane.Visible = true, true
	m.applyPoll(pollMsg{agents: agents, window: "@work", sel: "opencode:200"})
	if m.cursor != 1 {
		t.Fatal("cursor did not stay on the destination agent")
	}
}

func TestRefreshEventQueuesLatestPoll(t *testing.T) {
	m := testModel()
	m.requestedSequence, m.polling = 1, true
	updated, _ := m.Update(refreshMsg{})
	m = updated.(model)
	if m.requestedSequence != 2 || !m.pendingPoll {
		t.Fatalf("event while polling: sequence=%d pending=%v", m.requestedSequence, m.pendingPoll)
	}
	updated, cmd := m.Update(pollMsg{sequence: 1})
	m = updated.(model)
	if cmd == nil || !m.polling || m.pendingPoll || m.appliedSequence != 1 {
		t.Fatalf("queued poll not started: polling=%v pending=%v applied=%d", m.polling, m.pendingPoll, m.appliedSequence)
	}
}

func TestFallbackPollRunsEveryTwoSeconds(t *testing.T) {
	m := testModel()
	base := time.Now()
	m.lastPollRequested = base
	updated, _ := m.Update(tickMsg(base.Add(fallbackInterval - time.Millisecond)))
	m = updated.(model)
	if m.requestedSequence != 0 || m.polling {
		t.Fatal("fallback poll ran early")
	}
	updated, _ = m.Update(tickMsg(base.Add(fallbackInterval)))
	m = updated.(model)
	if m.requestedSequence != 1 || !m.polling {
		t.Fatalf("fallback did not request poll: sequence=%d polling=%v", m.requestedSequence, m.polling)
	}
}

func TestSidebarRestartsOnceRebuiltBinaryIsStable(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "tmux-sentinela")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := testModel()
	m.bin = bin
	m.binStat, _ = os.Stat(bin)
	base := time.Now()
	check := func() bool {
		base = base.Add(fallbackInterval)
		updated, _ := m.Update(tickMsg(base))
		m = updated.(model)
		return m.restart
	}
	if check() {
		t.Fatal("restarted with unchanged binary")
	}
	// A build replaces the file: a new inode, not an in-place write.
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("new build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if check() {
		t.Fatal("restarted before the rebuilt binary was seen twice")
	}
	if !check() {
		t.Fatal("did not restart after a stable rebuild")
	}
}

func TestStalePollCannotReplaceNewerSnapshot(t *testing.T) {
	m := testModel()
	m.appliedSequence = 2
	m.agents = []Agent{{Key: "new"}}
	updated, _ := m.Update(pollMsg{sequence: 1, agents: []Agent{{Key: "old"}}})
	m = updated.(model)
	if len(m.agents) != 1 || m.agents[0].Key != "new" || m.appliedSequence != 2 {
		t.Fatalf("stale poll replaced state: agents=%v sequence=%d", m.agents, m.appliedSequence)
	}
}

func TestPollKeepsSelectionAcrossReorderAndRemoval(t *testing.T) {
	isolateState(t)
	m := testModel()
	a := Agent{Key: "claude:100", Pane: Pane{WindowID: "@agent"}}
	b := Agent{Key: "opencode:200", Pane: Pane{WindowID: "@agent"}}
	m.applyPoll(pollMsg{agents: []Agent{a, b}, window: "@agent", active: true, sel: b.Key})
	if m.cursor != 1 {
		t.Fatal("second agent was not selected")
	}
	m.applyPoll(pollMsg{agents: []Agent{b, a}, window: "@agent", active: true, sel: b.Key})
	if m.cursor != 0 {
		t.Fatal("selection followed the index instead of the agent")
	}
	m.moveCursor(1)
	m.applyPoll(pollMsg{agents: []Agent{b}, window: "@agent", active: true, sel: a.Key})
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
			Since: m.started, Pane: Pane{Session: "work", WindowIndex: 1, WindowName: "api"}},
		{Key: "opencode:200", Kind: "opencode", Name: "blocked", Status: Blocked,
			Pane: Pane{Session: "code", WindowIndex: 2, WindowName: "login"}},
	}
	m.seen["claude:100"] = m.started.Add(-time.Minute)

	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(m.View(), "")
	want := " agents 1\n\n" +
		" work  " + strings.Repeat("─", 23) + "  \n" +
		"  ✓ done\n" +
		"    claude  api\n\n" +
		" code  " + strings.Repeat("─", 23) + "  \n" +
		"▌ ● blocked" + strings.Repeat(" ", 21) + "\n" +
		"▌   opencode  login" + strings.Repeat(" ", 13) + "\n"
	if plain != want {
		t.Fatalf("view =\n%q\nwant =\n%q", plain, want)
	}
}

func TestWindowNameFallsBackToIndex(t *testing.T) {
	m := testModel()
	m.agents = []Agent{{Kind: "pi", Name: "agent", Pane: Pane{Session: "work", WindowIndex: 7}}}
	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(m.View(), "")
	if !strings.Contains(plain, "▌   pi  7\n") {
		t.Fatalf("empty window name did not fall back to index: %q", plain)
	}
}

func TestSessionHeaderKeepsCaseAndColor(t *testing.T) {
	th := loadTheme(map[string]string{"@sentinela_color_title": "#aabbcc", "@sentinela_color_muted": "#112233"})
	want := lipgloss.NewStyle().Foreground(th.title).Bold(true).Render(" MiXeD  "+strings.Repeat("─", 10)) + "  "
	if got := sessionHeader("MiXeD", 20, th); got != want {
		t.Fatalf("header = %q, want %q", got, want)
	}
}

func TestSessionGroupsAndMouseRows(t *testing.T) {
	agents := []Agent{
		{Pane: Pane{Session: "code"}},
		{Pane: Pane{Session: "general"}},
		{Pane: Pane{Session: "code"}},
	}
	groups := sessionGroups(agents)
	if len(groups) != 2 || groups[0].name != "code" || groups[1].name != "general" ||
		len(groups[0].agents) != 2 || groups[0].agents[0] != 0 || groups[0].agents[1] != 2 {
		t.Fatalf("groups = %+v", groups)
	}
	for _, tt := range []struct{ row, want int }{
		{0, -1}, {1, -1}, {2, -1}, // title and first header
		{3, 0}, {4, 0}, {5, 2}, {6, 2},
		{7, -1}, {8, -1}, // gap and second header
		{9, 1}, {10, 1}, {11, -1},
	} {
		if got := agentAtRow(groups, tt.row); got != tt.want {
			t.Errorf("row %d = agent %d, want %d", tt.row, got, tt.want)
		}
	}
}

func TestSessionHeaderWidth(t *testing.T) {
	th := loadTheme(nil)
	for _, name := range []string{"a-long-session-name", "sesión-東京"} {
		for _, width := range []int{1, 3, 4, 12, 32} {
			header := sessionHeader(name, width, th)
			if got := lipgloss.Width(header); got != width {
				t.Errorf("header %q width at %d = %d", name, width, got)
			}
		}
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
