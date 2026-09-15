package main

import "strings"

type hermesScreenState struct {
	Name   string
	Status Status
}

type paneCapture func(string) (string, error)

func capturePaneScreen(paneID string) (string, error) {
	return tmux("capture-pane", "-p", "-t", paneID)
}

func collectHermesAgents(panes []Pane, capture paneCapture) []Agent {
	var agents []Agent
	for _, pane := range panes {
		if pane.Sidebar || pane.Command != "ssh" {
			continue
		}
		screen, err := capture(pane.ID)
		if err != nil {
			continue
		}
		state, ok := detectHermesScreen(screen)
		if !ok {
			continue
		}
		agents = append(agents, Agent{
			Kind: "hermes", Name: state.Name, Status: state.Status,
			Pane: pane, Key: "hermes:" + pane.ID,
		})
	}
	return agents
}

// detectHermesScreen recognizes the live Hermes composer. The status bar sits
// directly above the composer's top rule; scrollback mentioning Hermes alone
// is therefore not enough to create an agent.
func detectHermesScreen(screen string) (hermesScreenState, bool) {
	lines := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	for i := len(lines) - 1; i > 0; i-- {
		if !isHorizontalRule(lines[i]) {
			continue
		}
		statusIndex := previousContentLine(lines, i-1)
		if statusIndex < 0 || !strings.Contains(strings.TrimSpace(lines[statusIndex]), "⚕ ") {
			continue
		}
		promptIndex := nextContentLine(lines, i+1)
		if promptIndex < 0 {
			continue
		}
		return hermesScreenState{
			Name:   hermesScreenName(lines, statusIndex),
			Status: hermesPromptStatus(strings.TrimSpace(lines[promptIndex])),
		}, true
	}
	return hermesScreenState{}, false
}

func isHorizontalRule(line string) bool {
	line = strings.TrimSpace(line)
	return len([]rune(line)) >= 8 && strings.Trim(line, "─") == ""
}

func previousContentLine(lines []string, from int) int {
	for i := from; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}

func nextContentLine(lines []string, from int) int {
	for i := from; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}

func hermesPromptStatus(prompt string) Status {
	for _, prefix := range []string{"⚠", "?", "✎", "🔐", "🔑"} {
		if strings.HasPrefix(prompt, prefix) {
			return Blocked
		}
	}
	if strings.HasPrefix(prompt, "⚕") || strings.Contains(prompt, "command in progress") ||
		strings.Contains(prompt, "Processing command") {
		return Busy
	}
	return Idle
}

func hermesScreenName(lines []string, statusIndex int) string {
	statusLine := strings.TrimSpace(lines[statusIndex])
	if _, title, ok := strings.Cut(statusLine, " ─ "); ok {
		if title = strings.TrimSpace(title); title != "" {
			return title
		}
	}
	for i := statusIndex - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "╭─ ") {
			continue
		}
		header := strings.TrimPrefix(line, "╭─ ")
		if before, _, ok := strings.Cut(header, " ─"); ok {
			fields := strings.Fields(before)
			if len(fields) > 1 {
				return strings.Join(fields[1:], " ")
			}
		}
	}
	return "Hermes"
}
