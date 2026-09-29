package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"time"

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
		// The attach records its remote PTY under a random token. Never infer
		// identity from session alone: other clients can attach to it too.
		if tty := remoteClientForPane(a, paneID); tty != "" {
			if err := suppressLocalSidebar(paneID); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "ssh", append(sshArgs(a.Host, false), remoteSelectionForClient(a, tty))...)
			cmd.Stdout, cmd.Stderr = nil, nil
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("select remote pane: %w", err)
			}
			return jumpTo(paneID)
		}
	}
	// Select before attaching: attach-session takes over the SSH terminal,
	// so a tmux command queued after it may not run until detach.
	// Both commands use the same SSH channel.
	var tokenBytes [16]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes[:])
	// tty runs inside the SSH PTY. Register it before attach takes over;
	// the token lives on the local pane so only this pane's client is reused.
	remote := remoteSelection(a) + " && tty=$(tty) && " +
		remoteCommand("tmux", "set-option", "-s", "@sentinela_client_"+token) + " \"$tty\" && exec " +
		remoteCommand("tmux", "attach-session", "-t", a.Pane.Session)
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
	for _, pair := range [][2]string{{"@sentinela_remote_host", a.Host}, {"@sentinela_remote_session", a.Pane.Session}, {"@sentinela_remote_token", token}} {
		if _, err := tmux("set-option", "-p", "-t", paneID, pair[0], pair[1]); err != nil {
			return fmt.Errorf("mark remote pane: %w", err)
		}
	}
	if err := suppressLocalSidebar(paneID); err != nil {
		return err
	}
	return jumpTo(paneID)
}

// The attached remote tmux already draws its own sidebar. Keep the local
// attach window full-width, but let the user reopen its local bar with toggle.
func suppressLocalSidebar(paneID string) error {
	if !ensureLock() {
		if !waitForLock(2*time.Second) || !ensureLock() {
			return fmt.Errorf("remote sidebar: could not acquire ensure lock")
		}
	}
	defer ensureUnlock()
	window, err := tmux("display-message", "-p", "-t", paneID, "#{window_id}")
	if err != nil {
		return err
	}
	if _, err := tmux("set-option", "-w", "-t", window, userClosedOption, "1"); err != nil {
		return err
	}
	panes, err := listPanes()
	if err != nil {
		return err
	}
	for _, p := range panes {
		if p.WindowID == window && p.Sidebar {
			if _, err := tmux("kill-pane", "-t", p.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func remoteSelection(a Agent) string {
	return remoteCommand("tmux", "select-pane", "-t", a.Pane.ID) +
		" && " + remoteCommand("tmux", "select-window", "-t", fmt.Sprintf("%s:%d", a.Pane.Session, a.Pane.WindowIndex))
}

func remoteSelectionForClient(a Agent, tty string) string {
	return remoteCommand("tmux", "select-pane", "-t", a.Pane.ID) +
		" && " + remoteCommand("tmux", "switch-client", "-c", tty, "-t", fmt.Sprintf("%s:%d", a.Pane.Session, a.Pane.WindowIndex))
}

func remoteClientForPane(a Agent, paneID string) string {
	token, err := tmux("show-option", "-pqv", "-t", paneID, "@sentinela_remote_token")
	if err != nil || len(token) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(token); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
	defer cancel()
	// Verify that the recorded PTY is still attached to this session. An old
	// server option or a recycled PTY alone is not evidence of a live client.
	remote := remoteCommand("tmux", "show-option", "-sqv", "@sentinela_client_"+token) + " && " +
		remoteCommand("tmux", "list-clients", "-t", a.Pane.Session, "-F", "#{client_tty}")
	cmd := exec.CommandContext(ctx, "ssh", append(sshArgs(a.Host, false), remote)...)
	out, err := cmd.Output() // SSH stderr stays out of the TUI
	if err != nil {
		return ""
	}
	lines := strings.Fields(string(out))
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "/dev/") {
		return ""
	}
	for _, tty := range lines[1:] {
		if tty == lines[0] {
			return tty
		}
	}
	return ""
}

func reusableRemotePane(panes []Pane, a Agent) string {
	found := ""
	for _, p := range panes {
		if p.RemoteHost == a.Host && p.RemoteSession == a.Pane.Session && p.Command == "ssh" && pidAlive(p.PID) {
			if found != "" {
				return "" // no local pane ↔ remote client mapping is unique
			}
			found = p.ID
		}
	}
	return found
}

func (m model) jumpCommand(a Agent) tea.Cmd {
	return func() tea.Msg { return jumpResult{err: jumpAgent(a, m.window)} }
}
