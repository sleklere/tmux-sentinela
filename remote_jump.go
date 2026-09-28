package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type jumpResult struct{ err error }

// shellQuote protects tmux session names and pane IDs from the local shell
// used by new-window, and from the remote shell used by SSH.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func remoteCommand(args ...string) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = shellQuote(arg)
	}
	return strings.Join(parts, " ")
}

func jumpAgent(a Agent, window string) error {
	if a.Host == "" {
		return jumpTo(a.Pane.ID)
	}
	if a.Stale {
		return fmt.Errorf("%s: offline; press r to retry", a.Host)
	}
	panes, err := listPanes()
	if err != nil {
		return err
	}
	if paneID := reusableRemotePane(panes, a); paneID != "" {
		if err := jumpTo(paneID); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "ssh", append(sshArgs(a.Host, false), remoteSelection(a))...)
		cmd.Stdout, cmd.Stderr = nil, nil // discard diagnostics; never write on the TUI
		return cmd.Run()
	}
	// Select before attaching: attach-session takes over the SSH terminal,
	// so a tmux command queued after it may not run until detach.
	// Both commands use the same SSH channel.
	remote := remoteSelection(a) + " && exec " + remoteCommand("tmux", "attach-session", "-t", a.Pane.Session)
	args := append(sshArgs(a.Host, true), remote)
	parts := append([]string{"ssh"}, args...)
	for i := range parts {
		parts[i] = shellQuote(parts[i])
	}
	command := exec.Command("tmux", "new-window", "-d", "-a", "-t", window, "-n", a.Host,
		"-P", "-F", "#{pane_id}", strings.Join(parts, " "))
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("new remote window: %w: %s", err, strings.TrimSpace(string(output)))
	}
	paneID := strings.TrimSpace(string(output))
	for _, pair := range [][2]string{{"@sentinela_remote_host", a.Host}, {"@sentinela_remote_session", a.Pane.Session}} {
		if _, err := tmux("set-option", "-p", "-t", paneID, pair[0], pair[1]); err != nil {
			return fmt.Errorf("mark remote pane: %w", err)
		}
	}
	return jumpTo(paneID)
}

func remoteSelection(a Agent) string {
	return remoteCommand("tmux", "select-window", "-t", fmt.Sprintf("%s:%d", a.Pane.Session, a.Pane.WindowIndex)) +
		" && " + remoteCommand("tmux", "select-pane", "-t", a.Pane.ID)
}

func reusableRemotePane(panes []Pane, a Agent) string {
	for _, p := range panes {
		if p.RemoteHost == a.Host && p.RemoteSession == a.Pane.Session && p.Command == "ssh" && pidAlive(p.PID) {
			return p.ID
		}
	}
	return ""
}

func (m model) jumpCommand(a Agent) tea.Cmd {
	return func() tea.Msg { return jumpResult{err: jumpAgent(a, m.window)} }
}
