package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
)

// Theme colors: explicit @sentinela_color_* options win, then the @th_* options
// of a tmux-wide theme, then rose-pine so the sidebar renders anywhere.
type theme struct {
	text, muted, accent, title, busy, busyGlow, alert lipgloss.Color
}

func loadTheme(o map[string]string) theme {
	pick := func(own, th, def string) lipgloss.Color {
		for _, name := range []string{own, th} {
			if v := o[name]; v != "" {
				return lipgloss.Color(v)
			}
		}
		return lipgloss.Color(def)
	}
	busy := pick("@sentinela_color_busy", "@th_accent3", "#ffd166")
	return theme{
		text:     pick("@sentinela_color_text", "@th_text", "#e0def4"),
		muted:    pick("@sentinela_color_muted", "@th_muted", "#908caa"),
		accent:   pick("@sentinela_color_accent", "@th_accent1", "#9ccfd8"),
		title:    pick("@sentinela_color_title", "@th_accent2", "#c4a7e7"),
		busy:     busy,
		busyGlow: pick("@sentinela_color_busy_glow", "", string(pulseVariant(busy))),
		alert:    pick("@sentinela_color_alert", "@th_alert", "#eb6f92"),
	}
}

const busyCycleFrames = 16

const rowHeight = 2 // name line + detail line

type pollMsg struct {
	agents []Agent
	leader bool   // this sidebar is the first one in tmux order: it notifies
	active bool   // this sidebar pane has focus
	window string // window containing this sidebar
	sel    string // key of the agent selected from any sidebar
	err    error
}
type focusMsg struct {
	panes map[string]Pane // fresh pane flags, by pane id
	sel   string
}
type tickMsg time.Time

type model struct {
	bin     string
	self    string // own pane id, "" outside tmux
	th      theme
	notify  string // @sentinela_notify
	agents  []Agent
	cursor  int
	width   int
	height  int
	frame   int
	err     error
	seen    map[string]time.Time // agent key → last time its pane was visible
	started time.Time
	prev    map[string]Status // status at the previous poll, for transitions
	focused string            // key of the agent whose pane had focus last poll
	window  string            // window containing this sidebar
	active  bool              // this sidebar pane has focus
}

func newModel(bin string) model {
	o := globalOptions()
	return model{bin: bin, self: selfPane(), th: loadTheme(o), notify: o["@sentinela_notify"],
		seen: map[string]time.Time{}, started: time.Now()}
}

func (m model) poll() tea.Msg {
	panes, err := listPanes()
	if err != nil {
		return pollMsg{err: err}
	}
	// Only one sidebar announces, so N windows do not mean N notifications.
	firstSidebar, window, active := "", "", false
	for _, p := range panes {
		if p.Sidebar && firstSidebar == "" {
			firstSidebar = p.ID
		}
		if p.ID == m.self {
			window, active = p.WindowID, p.Visible
		}
	}
	return pollMsg{agents: collectPanes(panes), leader: firstSidebar == m.self,
		active: active, window: window, sel: readSelection()}
}

// focusPoll is the cheap poll between full ones: one list-panes, no ps.
func focusPoll() tea.Msg {
	panes, err := listPanes()
	if err != nil {
		return nil
	}
	byID := make(map[string]Pane, len(panes))
	for _, p := range panes {
		byID[p.ID] = p
	}
	return focusMsg{panes: byID, sel: readSelection()}
}

func (m *model) refresh() tea.Cmd {
	o := globalOptions()
	m.th, m.notify = loadTheme(o), o["@sentinela_notify"]
	return m.poll
}

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Init() tea.Cmd { return tea.Batch(m.poll, tick()) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.frame++
		if m.frame%8 == 0 { // ~1s data refresh, 120ms busy animation
			return m, tea.Batch(tick(), m.refresh())
		}
		if m.frame%2 == 0 { // ~240ms focus refresh
			return m, tea.Batch(tick(), focusPoll)
		}
		return m, tick()
	case pollMsg:
		m.err = msg.err
		if msg.err == nil {
			m.applyPoll(msg)
		}
	case focusMsg:
		if self, ok := msg.panes[m.self]; ok {
			m.active = self.Visible
		}
		for i := range m.agents {
			if p, ok := msg.panes[m.agents[i].Pane.ID]; ok {
				m.agents[i].Pane.Visible, m.agents[i].Pane.Current = p.Visible, p.Current
			}
		}
		m.syncCursor(msg.sel)
	case tea.MouseMsg:
		// Press and release both count: when the sidebar pane is inactive,
		// tmux uses the press to focus it and only forwards the release.
		press := msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
		release := msg.Action == tea.MouseActionRelease
		if press || release {
			if i := (msg.Y - 2) / rowHeight; i >= 0 && i < len(m.agents) && msg.Y >= 2 {
				m.moveCursor(i)
				jumpTo(m.agents[i].Pane.ID)
			}
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.agents)-1 {
				m.moveCursor(m.cursor + 1)
			}
		case "k", "up":
			if m.cursor > 0 {
				m.moveCursor(m.cursor - 1)
			}
		case "enter", "l":
			if m.cursor >= 0 && m.cursor < len(m.agents) {
				jumpTo(m.agents[m.cursor].Pane.ID)
			}
		case "r":
			return m, m.poll
		}
	}
	return m, nil
}

func (m *model) applyPoll(msg pollMsg) {
	m.agents = msg.agents
	m.window, m.active = msg.window, msg.active
	now := time.Now()
	prev := make(map[string]Status, len(m.agents))
	for _, a := range m.agents {
		seen, known := m.seen[a.Key]
		if shared, ok := readSeen(a.Key); ok && (!known || seen.Equal(m.started) || seen.Before(shared)) {
			m.seen[a.Key] = shared
			known = true
		}
		if a.Pane.Visible {
			m.seen[a.Key] = now
			if msg.leader {
				writeSeen(a.Key, now)
			}
		} else if !known {
			m.seen[a.Key] = m.started // unknown history: not "done"
		}
		// Announce the transition into blocked, once, and not for the pane
		// the user is already looking at.
		was, known := m.prev[a.Key]
		if known && was != Blocked && a.Status == Blocked && !a.Pane.Visible &&
			msg.leader && m.notify != "off" {
			go notify(a, m.notify)
		}
		prev[a.Key] = a.Status
	}
	m.prev = prev
	m.syncCursor(msg.sel)
	if m.cursor >= len(m.agents) {
		m.cursor = max(0, len(m.agents)-1)
	}
}

// moveCursor publishes the new selection so every other sidebar marks the same
// agent: a jump then lands on a window whose bar is already in place.
func (m *model) moveCursor(i int) {
	m.cursor = i
	if i >= 0 && i < len(m.agents) {
		writeSelection(m.agents[i].Key)
	}
}

func (m *model) syncCursor(selected string) {
	hasAgent := false
	for _, a := range m.agents {
		if a.Pane.WindowID == m.window {
			hasAgent = true
			break
		}
	}
	if !hasAgent && !m.active {
		m.cursor = -1
		return
	}
	if !m.followFocus() {
		m.selectKey(selected)
	}
}

func (m *model) selectKey(key string) {
	if key == "" {
		return
	}
	for i, a := range m.agents {
		if a.Key == key {
			m.cursor = i
			return
		}
	}
}

// followFocus selects the agent of the active window when that agent changes,
// and reports whether it did; j/k keep working while focus is still. The active
// pane wins over other agents in the window (the sidebar itself may hold focus).
func (m *model) followFocus() bool {
	focused := ""
	for _, a := range m.agents {
		if a.Pane.Visible {
			focused = a.Key
			break
		}
		if a.Pane.Current && focused == "" {
			focused = a.Key
		}
	}
	if focused == m.focused {
		return false
	}
	m.focused = focused
	if focused == "" {
		return false
	}
	for i, a := range m.agents {
		if a.Key == focused {
			m.moveCursor(i)
			return true
		}
	}
	return false
}

// done: idle since a moment the user has not looked at the pane afterwards.
func (m model) done(a Agent) bool {
	return a.Status == Idle && m.seen[a.Key].Before(a.Since)
}

func (m model) glyph(a Agent) (string, lipgloss.Color) {
	switch {
	case a.Status == Blocked:
		return "●", m.th.alert
	case a.Status == Busy:
		return "●", pulseColor(m.th.busy, m.th.busyGlow, m.frame)
	case m.done(a):
		return "✓", m.th.accent
	default:
		return "○", m.th.muted
	}
}

func pulseColor(from, to lipgloss.Color, frame int) lipgloss.Color {
	start, startOK := parseHexColor(from)
	end, endOK := parseHexColor(to)
	if !startOK || !endOK {
		return from
	}
	t := (1 - math.Cos(2*math.Pi*float64(frame%busyCycleFrames)/busyCycleFrames)) / 2
	mix := func(a, b uint64) uint8 {
		return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
		mix(start>>16, end>>16),
		mix(start>>8&0xff, end>>8&0xff),
		mix(start&0xff, end&0xff),
	))
}

func parseHexColor(c lipgloss.Color) (uint64, bool) {
	s := strings.TrimPrefix(string(c), "#")
	if len(s) != 6 {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 16, 24)
	return n, err == nil
}

func pulseVariant(base lipgloss.Color) lipgloss.Color {
	c, err := colorful.Hex(string(base))
	if err != nil {
		return base
	}
	h, s, l := c.Hsl()
	return lipgloss.Color(colorful.Hsl(math.Mod(h+340, 360), min(1, s+0.12), max(0, l-0.08)).Hex())
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	w := m.width
	var b strings.Builder

	title := lipgloss.NewStyle().Foreground(m.th.title).Bold(true).Render(" agents")
	count := ""
	if n := countStatus(m.agents, Blocked); n > 0 {
		count = lipgloss.NewStyle().Foreground(m.th.alert).Bold(true).Render(fmt.Sprintf(" %d", n))
	}
	b.WriteString(title + count + "\n\n")

	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(m.th.alert).Render(" tmux: " + m.err.Error()))
		return b.String()
	}
	if len(m.agents) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(m.th.muted).Render(" no agents running"))
		return b.String()
	}

	name := lipgloss.NewStyle().Foreground(m.th.text)
	dim := lipgloss.NewStyle().Foreground(m.th.muted)
	bar := lipgloss.NewStyle().Foreground(m.th.accent).Render("▌")

	for i, a := range m.agents {
		g, c := m.glyph(a)
		nameStyle := name
		if m.done(a) || a.Status == Blocked {
			nameStyle = nameStyle.Bold(true)
		}
		edge := " "
		if i == m.cursor {
			edge = bar
		}
		line1 := edge + " " + lipgloss.NewStyle().Foreground(c).Render(g) + " " +
			nameStyle.Render(truncate(a.Name, w-5))
		detail := a.Kind + "  " + fmt.Sprintf("%s:%d", a.Pane.Session, a.Pane.WindowIndex)
		if a.Status != Idle { // how long it has been working / waiting
			if d := since(a.Since); d != "" {
				detail += "  " + d
			}
		}
		line2 := edge + "   " + dim.Render(truncate(detail, w-5))
		b.WriteString(line1 + "\n" + line2 + "\n")
	}
	return b.String()
}

func countStatus(agents []Agent, s Status) int {
	n := 0
	for _, a := range agents {
		if a.Status == s {
			n++
		}
	}
	return n
}

func since(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t).Round(time.Minute)
	switch {
	case d < time.Minute:
		return ""
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n <= 1 || len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func runSidebar(bin string) error {
	if self := selfPane(); self != "" {
		// Self-register so a sidebar restored by tmux-resurrect is recognised;
		// bail out if the window already has one.
		if dup, _ := siblingSidebar(self); dup {
			return nil
		}
		tmux("set-option", "-p", "-t", self, "@sentinela_sidebar", "1")
		paintSidebar(self)
	}
	_, err := tea.NewProgram(newModel(bin), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
