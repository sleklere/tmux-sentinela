package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolatedTmux(t *testing.T) func(...string) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	isolateState(t)
	// Keep the Unix socket path short enough on macOS too.
	dir, err := os.MkdirTemp("", "sentinela-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "socket")
	t.Cleanup(func() {
		exec.Command("tmux", "-S", socket, "kill-server").Run()
		os.RemoveAll(dir)
	})
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("tmux", append([]string{"-S", socket}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, out)
		}
		return strings.TrimRight(string(out), "\n")
	}
	run("-f", "/dev/null", "new-session", "-d", "-s", "test", "-x", "160", "-y", "40", "/bin/sh")
	t.Setenv("TMUX", socket+",0,0")
	t.Setenv("TMUX_PANE", "%0")
	return run
}

func waitForWindowName(t *testing.T, run func(...string) string, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := run("display-message", "-p", "-t", "@0", "#{window_name}")
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("window name = %q, want %q", got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSidebarDoesNotNameWindow(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-w", "-t", "@0", "automatic-rename-format", "#{pane_title},#{pane_index}")
	run("select-pane", "-t", "%0", "-T", "work")
	sidebar := run("split-window", "-hd", "-t", "@0", "-P", "-F", "#{pane_id}", "while sleep 0.1; do printf .; done")
	run("set-option", "-p", "-t", sidebar, "@sentinela_sidebar", "1")
	run("select-pane", "-t", sidebar, "-T", "tmux-sentinela")
	waitForWindowName(t, run, "work,0")
	run("select-pane", "-t", sidebar)
	waitForWindowName(t, run, "tmux-sentinela,1")
	for range 2 {
		if err := configureSidebarName(sidebar); err != nil {
			t.Fatal(err)
		}
	}
	waitForWindowName(t, run, "work,0")
	if got := run("display-message", "-p", "-t", "@0", "#{E:automatic-rename-format}"); got != "work,0" {
		t.Fatalf("sidebar rename format = %q, want work,0", got)
	}
	run("select-pane", "-t", "%0", "-T", "updated")
	waitForWindowName(t, run, "updated,0")
	run("rename-window", "-t", "@0", "manual name")
	if err := configureSidebarName(sidebar); err != nil {
		t.Fatal(err)
	}
	if got := run("display-message", "-p", "-t", "@0", "#{automatic-rename}:#{window_name}"); got != "0:manual name" {
		t.Fatalf("manual name/automatic-rename changed: %q", got)
	}
	run("kill-pane", "-t", sidebar)
	run("set-option", "-w", "-t", "@0", "automatic-rename", "on")
	waitForWindowName(t, run, "updated,0")
}

func TestSidebarPreservesExistingManualName(t *testing.T) {
	run := isolatedTmux(t)
	run("rename-window", "-t", "@0", "my work")
	sidebar := run("split-window", "-h", "-t", "@0", "-P", "-F", "#{pane_id}", "sleep 300")
	run("set-option", "-p", "-t", sidebar, "@sentinela_sidebar", "1")
	if err := configureSidebarName(sidebar); err != nil {
		t.Fatal(err)
	}
	if got := run("display-message", "-p", "-t", "@0", "#{automatic-rename}:#{window_name}"); got != "0:my work" {
		t.Fatalf("existing manual name/automatic-rename changed: %q", got)
	}
}
