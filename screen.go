package main

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type paneCapture func(string) (string, error)

type screenAgentState struct {
	Kind   string
	Name   string
	Status Status
}

var (
	opencodeProgress = regexp.MustCompile(`[■⬝]{4,}`)
	claudeChoice     = regexp.MustCompile(`(?mi)^\s*❯?\s*(?:[123]\.\s*)?(?:yes|no)\b`)
	claudeTimedWork  = regexp.MustCompile(`(?m)^\s*[\*·✢✳✶✻✽]\s+\S.*…(?:\s+\(\d+[smh](?:\s|·)|\s*$)`)
	claudeAgentsWait = regexp.MustCompile(`(?mi)waiting for [1-9]\d* background agents? to finish`)
)

func capturePaneScreen(paneID string) (string, error) {
	return tmux("capture-pane", "-p", "-t", paneID)
}

// collectScreenAgents captures each unclaimed pane once. Hermes has a strong
// composer signature and may run locally or over SSH; Claude and OpenCode are
// only inferred visually for SSH panes, where their authoritative local state
// files are unavailable.
func collectScreenAgents(panes []Pane, claimed map[string]bool, capture paneCapture, serverID string) []Agent {
	return collectScreenAgentsExcept(panes, claimed, capture, serverID, nil)
}

func collectScreenAgentsExcept(panes []Pane, claimed map[string]bool, capture paneCapture, serverID string, hosts []string) []Agent {
	covered := sshPanesForHosts(panes, hosts)
	var agents []Agent
	for _, pane := range panes {
		if pane.Sidebar || claimed[pane.ID] || covered[pane.ID] {
			continue
		}
		screen, err := capture(pane.ID)
		if err != nil {
			continue
		}
		state, ok := detectScreenAgent(pane, screen)
		if !ok {
			continue
		}
		key := state.Kind + "-screen:" + serverID + ":" + pane.ID
		if state.Kind == "hermes" { // keep existing selections and seen state stable
			key = "hermes:" + serverID + ":" + pane.ID
		}
		agents = append(agents, Agent{
			Kind: state.Kind, Name: state.Name, Status: state.Status,
			Pane: pane, Key: key, Visual: true,
		})
	}
	return agents
}

// An SSH process descended from a pane's shell identifies a configured host.
// Exclude that pane's visual guess even when the host is down: the pull is
// authoritative, and a stale rendered screen must not resurrect an agent.
func sshPanesForHosts(panes []Pane, hosts []string) map[string]bool {
	covered := map[string]bool{}
	if len(hosts) == 0 {
		return covered
	}
	out, err := exec.Command("ps", "-eo", "pid=,ppid=,args=").Output()
	if err != nil {
		return covered
	}
	parents := map[int]int{}
	sshHosts := map[int]string{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, e1 := strconv.Atoi(fields[0])
		ppid, e2 := strconv.Atoi(fields[1])
		if e1 != nil || e2 != nil {
			continue
		}
		parents[pid] = ppid
		if fields[2] != "ssh" && !strings.HasSuffix(fields[2], "/ssh") {
			continue
		}
		for _, field := range fields[3:] {
			for _, host := range hosts {
				if field == host || strings.HasSuffix(field, "@"+host) {
					sshHosts[pid] = host
				}
			}
		}
	}
	byPID := map[int]string{}
	for _, pane := range panes {
		byPID[pane.PID] = pane.ID
	}
	for pid := range sshHosts {
		for n := 0; n < 64 && pid > 1; n++ {
			if id, ok := byPID[pid]; ok {
				covered[id] = true
				break
			}
			pid = parents[pid]
		}
	}
	return covered
}

func detectScreenAgent(pane Pane, screen string) (screenAgentState, bool) {
	if state, ok := detectHermesScreen(screen); ok {
		return screenAgentState{Kind: "hermes", Name: state.Name, Status: state.Status}, true
	}
	if pane.Command != "ssh" {
		return screenAgentState{}, false
	}
	if state, ok := detectOpenCodeScreen(pane.Title, screen); ok {
		return state, true
	}
	return detectClaudeScreen(pane.Title, screen)
}

func detectOpenCodeScreen(title, screen string) (screenAgentState, bool) {
	lowerTitle := strings.ToLower(strings.TrimSpace(title))
	lower := strings.ToLower(screen)
	lowerBottom := strings.ToLower(bottomNonEmptyLines(screen, 12))
	permissionForm := strings.Contains(lowerBottom, "esc dismiss") &&
		containsAny(lowerBottom, "enter confirm", "enter submit", "enter toggle") &&
		containsAny(lowerBottom, "↑↓ select", "⇆ tab")
	present := lowerTitle == "opencode" || strings.HasPrefix(lowerTitle, "oc |") ||
		(strings.Contains(lowerBottom, "ctrl+p commands") &&
			(strings.Contains(lower, "build ·") || strings.Contains(lower, "plan ·"))) ||
		strings.Contains(lowerBottom, "△ permission required") || permissionForm
	if !present {
		return screenAgentState{}, false
	}

	status := Idle
	if strings.Contains(lowerBottom, "△ permission required") || permissionForm {
		status = Blocked
	} else if containsAny(lowerBottom, "esc to interrupt", "ctrl+c to interrupt", "press esc to interrupt") ||
		opencodeProgress.MatchString(lowerBottom) {
		status = Busy
	}

	name := "OpenCode"
	if strings.HasPrefix(lowerTitle, "oc |") {
		if candidate := strings.TrimSpace(title[len("OC |"):]); candidate != "" {
			name = candidate
		}
	}
	return screenAgentState{Kind: "opencode", Name: name, Status: status}, true
}

func detectClaudeScreen(title, screen string) (screenAgentState, bool) {
	lowerTitle := strings.ToLower(strings.TrimSpace(title))
	bottom := bottomNonEmptyLines(screen, 12)
	lowerBottom := strings.ToLower(bottom)
	blocked := claudeBlockedScreen(lowerBottom, bottom)
	busy := claudeBusyScreen(title, lowerBottom, bottom)

	// F6: Anchor presence to live bottom structure.
	// Check bottom 12 for blocked/busy, but for idle require live indicators
	// in the last few lines (footer or title). A bare prompt is not enough.
	lastFew := bottomNonEmptyLines(screen, 3)
	lowerLastFew := strings.ToLower(lastFew)
	hasLiveFooter := strings.Contains(lowerLastFew, "shift+tab to cycle") ||
		strings.Contains(lowerLastFew, "auto mode")

	// If the very last non-empty line is a shell prompt, it's not a live Claude frame.
	lines := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	var lastNonEmpty string
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			lastNonEmpty = strings.TrimSpace(lines[i])
			break
		}
	}
	looksLikeShellPrompt := strings.HasSuffix(lastNonEmpty, "$") ||
		strings.HasSuffix(lastNonEmpty, "#") ||
		strings.HasSuffix(lastNonEmpty, ">") ||
		strings.HasSuffix(lastNonEmpty, "%")

	present := strings.Contains(lowerTitle, "claude code") ||
		strings.Contains(lowerBottom, "claude code v") ||
		blocked || busy ||
		(claudeTitleWorking(title) && busy) ||
		hasLiveFooter
	if !present || looksLikeShellPrompt {
		return screenAgentState{}, false
	}

	status := Idle
	if blocked {
		status = Blocked
	} else if busy {
		status = Busy
	} else if !hasLiveFooter && !strings.Contains(lowerTitle, "claude code") {
		// Idle requires a live footer or a Claude title. A bare prompt is not enough.
		return screenAgentState{}, false
	}
	return screenAgentState{Kind: "claude", Name: "Claude Code", Status: status}, true
}

func hasClaudePrompt(screen string) bool {
	for _, line := range strings.Split(screen, "\n") {
		if strings.TrimSpace(line) == "❯" {
			return true
		}
	}
	return false
}

func claudeBlockedScreen(lower, screen string) bool {
	if strings.Contains(lower, "esc to cancel") &&
		containsAny(lower, "enter to confirm", "enter to select", "run a dynamic workflow?") {
		return true
	}
	if strings.Contains(lower, "do you want to proceed?") && claudeChoice.MatchString(screen) &&
		containsAny(lower, "bash command", "bash(", "contains expansion", "tab to amend", "ctrl+e to explain") {
		return true
	}
	if strings.Contains(lower, "do you want to proceed?") && strings.Contains(lower, "esc to cancel") &&
		claudeChoice.MatchString(screen) {
		return true
	}
	if strings.Contains(lower, "mcp server") && strings.Contains(lower, "requests your input") &&
		strings.Contains(lower, "esc to cancel") && containsAny(lower, "accept", "decline") {
		return true
	}
	return containsAny(lower, "waiting for permission", "do you want to allow this connection?",
		"review your answers", "skip interview and plan immediately")
}

func claudeBusyScreen(title, lower, screen string) bool {
	return containsAny(lower, "esc to interrupt", "esc to close") ||
		claudeTimedWork.MatchString(screen) || claudeAgentsWait.MatchString(screen) ||
		(strings.Contains(lower, "mcp task") && strings.Contains(lower, "still running")) ||
		claudeTitleWorking(title)
}

func claudeTitleWorking(title string) bool {
	title = strings.TrimSpace(title)
	r, _ := utf8.DecodeRuneInString(title)
	return (r >= 0x2800 && r <= 0x28ff) || (r >= 0x25d0 && r <= 0x25d3)
}

func containsAny(s string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(s, value) {
			return true
		}
	}
	return false
}

func bottomNonEmptyLines(screen string, count int) string {
	lines := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	nonEmpty := make([]string, 0, min(len(lines), count))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if len(nonEmpty) > count {
		nonEmpty = nonEmpty[len(nonEmpty)-count:]
	}
	return strings.Join(nonEmpty, "\n")
}
