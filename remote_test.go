package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeSSH(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "ssh"), []byte("#!/bin/sh\n"+script))
	if err := os.Chmod(filepath.Join(dir, "ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestRemotePollParsesHooksAndKeepsSSHStderrPrivate(t *testing.T) {
	fakeSSH(t, `echo 'ssh warning' >&2
printf '%s\n' '{"version":1,"agents":[{"key":"pi:7","kind":"pi","name":"agent","status":"blocked","pane_id":"%1","session":"dev","window":1,"pane":2,"duration_seconds":42}]}'`)
	got := pollRemote(context.Background(), "dojo", "")
	if got.err != nil || len(got.agents) != 1 || got.agents[0].Key != "remote:dojo:pi:7" || got.agents[0].Status != Blocked || got.agents[0].Pane.PaneIndex != 2 {
		t.Fatalf("poll: %+v", got)
	}
}

func TestRemotePollFailures(t *testing.T) {
	for _, tc := range []struct{ name, script, reason string }{
		{"auth", "echo 'Permission denied (publickey)' >&2; exit 255", "auth failed"},
		{"version", "echo '{\"version\":2,\"agents\":[]}'", "incompatible status format"},
		{"invalid", "echo '{\"version\":1,\"agents\":[{\"status\":\"bogus\",\"pane_id\":\"%1\",\"session\":\"a\"}]}'", "invalid remote agent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeSSH(t, tc.script)
			if got := pollRemote(context.Background(), "dojo", ""); got.err == nil || got.err.Error() != tc.reason {
				t.Fatalf("poll error: %v, want %s", got.err, tc.reason)
			}
		})
	}
}

func TestRemotePollTimeout(t *testing.T) {
	fakeSSH(t, "sleep 10")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := pollRemote(ctx, "down", "")
	if got.err == nil || got.err.Error() != "unavailable" && got.err.Error() != "timeout" || time.Since(start) > time.Second {
		t.Fatalf("timeout: %v, elapsed %s", got.err, time.Since(start))
	}
}

func TestRemoteBinaryPathWithSpacesIsSingleCommand(t *testing.T) {
	fakeSSH(t, `for arg; do last=$arg; done
case "$last" in
  "'/opt/sentinela bin/tmux-sentinela' 'status' '--json'") echo '{"version":1,"agents":[]}' ;;
  *) exit 1 ;;
esac`)
	if got := pollRemote(context.Background(), "dojo", "/opt/sentinela bin/tmux-sentinela"); got.err != nil {
		t.Fatalf("remote binary command: %v", got.err)
	}
}

func TestConfiguredHosts(t *testing.T) {
	got := configuredHosts(map[string]string{"@sentinela_hosts": "dojo argos dojo -unsafe 'bad;cmd'"})
	if strings.Join(got, ",") != "dojo,argos" {
		t.Fatalf("hosts = %v", got)
	}
}

func TestRemotePollDoesNotBlockLocalInit(t *testing.T) {
	fakeSSH(t, "sleep 5")
	m := newModel("")
	m.hosts = []string{"slow", "other"}
	m.leader = true
	start := time.Now()
	cmd := m.pollRemotes(start)
	if cmd == nil || time.Since(start) > 100*time.Millisecond || !m.remotePending["slow"] || !m.remotePending["other"] {
		t.Fatalf("dispatch blocked or missed hosts: %+v", m.remotePending)
	}
}
