package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const remoteTimeout = 3 * time.Second

type remoteResult struct {
	host   string
	agents []Agent
	err    error
	at     time.Time
}

// A single status command per host uses one SSH channel. The control socket
// is short enough for Unix socket path limits even with a long home directory.
func sshArgs(host string, interactive bool) []string {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("sentinela-ssh-%%C-%d", os.Getuid()))
	args := []string{"-o", "BatchMode=yes", "-o", "ControlMaster=auto", "-o", "ControlPersist=60", "-o", "ControlPath=" + path, "-o", "ConnectTimeout=2"}
	if interactive {
		args = append(args, "-t")
	} else {
		args = append(args, "-T")
	}
	return append(args, "--", host)
}

func pollRemote(ctx context.Context, host string) remoteResult {
	result := remoteResult{host: host, at: time.Now()}
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	args := append(sshArgs(host, false), "tmux-sentinela status --json")
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.WaitDelay = 100 * time.Millisecond // do not wait for inherited SSH pipes after cancellation
	// Keep diagnostics separate: stderr is never part of the JSON or terminal.
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			result.err = fmt.Errorf("timeout")
		case strings.Contains(strings.ToLower(stderr.String()), "permission denied"):
			result.err = fmt.Errorf("auth failed")
		default:
			result.err = fmt.Errorf("unavailable")
		}
		return result
	}
	var doc statusDocument
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil || doc.Version != statusVersion || doc.Agents == nil {
		result.err = fmt.Errorf("incompatible status format")
		return result
	}
	for _, a := range doc.Agents {
		status := map[string]Status{"idle": Idle, "busy": Busy, "blocked": Blocked}
		s, ok := status[a.Status]
		if !ok || a.PaneID == "" || a.Session == "" {
			result.err = fmt.Errorf("invalid remote agent")
			result.agents = nil
			return result
		}
		result.agents = append(result.agents, Agent{
			Host: host, Kind: a.Kind, Name: a.Name, Status: s, Since: a.Since,
			PID: a.PID, Key: "remote:" + host + ":" + a.Key,
			Pane: Pane{ID: a.PaneID, Session: a.Session, WindowIndex: a.Window, PaneIndex: a.Pane},
		})
	}
	return result
}

func configuredHosts(opts map[string]string) []string {
	var hosts []string
	seen := map[string]bool{}
	for _, host := range strings.Fields(opts["@sentinela_hosts"]) {
		// Hostnames are SSH aliases, not commands or options. SSH config can
		// supply User, HostName, ProxyJump and Port for each alias.
		if strings.HasPrefix(host, "-") || strings.ContainsAny(host, "'\"\\;\n\r") || seen[host] {
			continue
		}
		seen[host] = true
		hosts = append(hosts, host)
	}
	return hosts
}
