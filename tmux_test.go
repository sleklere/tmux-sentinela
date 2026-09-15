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

func TestSidebarBackgroundFollowsTheme(t *testing.T) {
	run := isolatedTmux(t)
	sidebar := run("split-window", "-hd", "-t", "@0", "-P", "-F", "#{pane_id}", "sleep 300")
	t.Setenv("TMUX_PANE", sidebar)
	style := func() string {
		return run("display-message", "-p", "-t", sidebar, "#{window-style}|#{window-active-style}")
	}

	run("set-option", "-g", "@th_base", "#191724")
	m := newModel("")
	paintSidebar(sidebar, string(m.th.background))
	if got := style(); got != "bg=#191724|bg=#191724" {
		t.Fatalf("initial style = %q", got)
	}
	// The running sidebar's normal refresh must update the stored styles.
	run("set-option", "-g", "@th_base", "#2e3440")
	m.beginPoll()
	if got := style(); got != "bg=#2e3440|bg=#2e3440" {
		t.Fatalf("style after theme switch = %q, want the new @th_base", got)
	}
	run("set-option", "-g", "@sentinela_bg", "#000000")
	m.beginPoll()
	if got := style(); got != "bg=#000000|bg=#000000" {
		t.Fatalf("style with @sentinela_bg = %q, want the override", got)
	}
	run("set-option", "-g", "@th_base", "#ffffff")
	m.beginPoll()
	if got := style(); got != "bg=#000000|bg=#000000" {
		t.Fatalf("theme change replaced the explicit background: %q", got)
	}
	run("set-option", "-gu", "@sentinela_bg")
	m.beginPoll()
	if got := style(); got != "bg=#ffffff|bg=#ffffff" {
		t.Fatalf("style after removing override = %q, want the current theme", got)
	}
	run("set-option", "-gu", "@th_base")
	m.beginPoll()
	if got := style(); got != "default|default" {
		t.Fatalf("style without colors = %q, want default", got)
	}
	if got := run("display-message", "-p", "-t", "@0", "#{pane_id}"); got != "%0" {
		t.Fatalf("refresh stole focus from the work pane: %q", got)
	}
}

func TestSidebarBackgroundAcceptsBlackIndex(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@th_base", "blue")
	run("set-option", "-g", "@sentinela_bg", "0")
	paintSidebar("%0", string(loadTheme(globalOptions()).background))
	if got := run("display-message", "-p", "-t", "%0", "#{window-style}"); got != "bg=0" {
		t.Fatalf("black background = %q, want bg=0", got)
	}
	run("set-option", "-gu", "@sentinela_bg")
	run("set-option", "-g", "@th_base", "0")
	paintSidebar("%0", string(loadTheme(globalOptions()).background))
	if got := run("display-message", "-p", "-t", "%0", "#{window-style}"); got != "bg=0" {
		t.Fatalf("black theme background = %q, want bg=0", got)
	}
}

func TestSidebarBackgroundInheritsWindowStyle(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-w", "-t", "@0", "window-style", "bg=blue")
	run("set-option", "-w", "-t", "@0", "window-active-style", "bg=red")
	m := newModel("")
	paintSidebar("%0", string(m.th.background))
	style := func() string {
		return run("display-message", "-p", "-t", "%0", "#{window-style}|#{window-active-style}")
	}
	if got := style(); got != "bg=blue|bg=red" {
		t.Fatalf("initial inherited style = %q, want bg=blue|bg=red", got)
	}
	run("set-option", "-g", "@th_base", "green")
	m.beginPoll()
	if got := style(); got != "bg=green|bg=green" {
		t.Fatalf("style after adding a theme = %q, want bg=green|bg=green", got)
	}
	run("set-option", "-gu", "@th_base")
	m.beginPoll()
	if got := style(); got != "bg=blue|bg=red" {
		t.Fatalf("style after removing theme = %q, want inheritance restored", got)
	}
}

func TestSidebarBackgroundRefreshOnlyOnChange(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@th_base", "blue")
	m := newModel("")
	paintSidebar("%0", string(m.th.background))
	run("set-option", "-p", "-t", "%0", "window-style", "bg=red")
	run("set-option", "-g", "@th_text", "#ffffff")
	m.beginPoll()
	if got := run("display-message", "-p", "-t", "%0", "#{window-style}"); got != "bg=red" {
		t.Fatalf("unchanged background was repainted: %q", got)
	}
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
