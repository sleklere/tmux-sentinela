package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func lockTestPanes(t *testing.T, run func(...string) string, workCount int) string {
	t.Helper()
	for i := 1; i < workCount; i++ {
		run("split-window", "-hd", "-t", "@0", "sleep 300")
	}
	sidebar := run("split-window", "-hbdf", "-l", "32", "-t", "@0", "-P", "-F", "#{pane_id}", "sleep 300")
	run("set-option", "-p", "-t", sidebar, "@sentinela_sidebar", "1")
	return sidebar
}

func assertSidebarPinned(t *testing.T, run func(...string) string, sidebar string) {
	t.Helper()
	got := run("display-message", "-p", "-t", sidebar, "#{pane_left}:#{pane_top}:#{pane_height}:#{window_height}:#{pane_width}")
	parts := strings.Split(got, ":")
	if len(parts) != 5 || parts[0] != "0" || parts[1] != "0" || parts[2] != parts[3] || parts[4] != "32" {
		t.Fatalf("sidebar geometry = %q, want left=0 top=0 full-height width=32", got)
	}
}

func waitSidebarPinned(t *testing.T, run func(...string) string, sidebar string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := run("display-message", "-p", "-t", sidebar, "#{pane_left}:#{pane_top}:#{pane_height}:#{window_height}:#{pane_width}")
		parts := strings.Split(got, ":")
		if len(parts) == 5 && parts[0] == "0" && parts[1] == "0" && parts[2] == parts[3] {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := run("display-message", "-p", "-t", sidebar, "#{pane_left}:#{pane_top}:#{pane_height}:#{window_height}")
	t.Fatalf("sidebar was not pinned: %s", got)
}

func TestLockSidebarAcrossLayouts(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	loadPlugin(t)
	sidebar := lockTestPanes(t, run, 3)
	run("select-pane", "-t", "%0")
	for i := 0; i < 8; i++ {
		run("next-layout", "-t", "@0")
		waitSidebarPinned(t, run, sidebar)
		if got := run("display-message", "-p", "-t", "@0", "#{pane_id}"); got != "%0" {
			t.Fatalf("iteration %d: focused pane = %s, want %%0", i, got)
		}
	}
	run("select-layout", "-t", "@0", "even-vertical")
	waitSidebarPinned(t, run, sidebar)
	assertSidebarPinned(t, run, sidebar)
	run("select-pane", "-t", sidebar)
	run("select-layout", "-t", "@0", "tiled")
	waitSidebarPinned(t, run, sidebar)
	if got := run("display-message", "-p", "-t", "@0", "#{pane_id}"); got != sidebar {
		t.Fatalf("sidebar focus lost: got %s, want %s", got, sidebar)
	}
}

func TestLockSidebarOffAndLiveSwitch(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	run("set-option", "-g", "@sentinela_lock_sidebar", "off")
	loadPlugin(t)
	sidebar := lockTestPanes(t, run, 2)
	run("select-layout", "-t", "@0", "even-vertical")
	time.Sleep(150 * time.Millisecond)
	if got := run("display-message", "-p", "-t", sidebar, "#{pane_height}"); got == "40" {
		t.Fatalf("opted-out sidebar was pinned: height = %s", got)
	}
	command := exec.Command("./bin/tmux-sentinela", "next-layout", "@0")
	command.Env = os.Environ()
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("next-layout with locking off: %v: %s", err, out)
	}
	if got := run("display-message", "-p", "-t", sidebar, "#{pane_height}"); got == "40" {
		t.Fatalf("opted-out binding pinned the sidebar: height = %s", got)
	}
	run("set-option", "-g", "@sentinela_lock_sidebar", "on")
	run("next-layout", "-t", "@0")
	waitSidebarPinned(t, run, sidebar)
}

func TestLockSidebarPreservesResizeAndZoom(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	loadPlugin(t)
	sidebar := lockTestPanes(t, run, 2)
	run("resize-pane", "-t", sidebar, "-x", "44")
	// Resizing a pinned sidebar is a deliberate user action, not a layout reset.
	if got := run("display-message", "-p", "-t", sidebar, "#{pane_width}"); got != "44" {
		t.Fatalf("manual sidebar width = %s, want 44", got)
	}
	run("resize-pane", "-t", "%0", "-Z")
	if got := run("display-message", "-p", "-t", "@0", "#{window_zoomed_flag}"); got != "1" {
		t.Fatalf("zoom was cancelled: %s", got)
	}
}

func TestLockSidebarCommandAndSpaceBinding(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	loadPlugin(t)
	if got := run("list-keys", "-T", "prefix"); !strings.Contains(got, "tmux-sentinela") {
		t.Fatalf("Space binding does not use Sentinela: %s", got)
	}
	sidebar := lockTestPanes(t, run, 2)
	command := exec.Command("./bin/tmux-sentinela", "next-layout", "@0")
	command.Env = os.Environ()
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("next-layout: %v: %s", err, out)
	}
	waitSidebarPinned(t, run, sidebar)
	assertSidebarPinned(t, run, sidebar)

	// Joining the pane back into its own window must not steal its focus.
	run("select-pane", "-t", sidebar)
	command = exec.Command("./bin/tmux-sentinela", "next-layout", "@0")
	command.Env = os.Environ()
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("next-layout with focused sidebar: %v: %s", err, out)
	}
	assertSidebarPinned(t, run, sidebar)
	if got := run("display-message", "-p", "-t", "@0", "#{pane_id}"); got != sidebar {
		t.Fatalf("focused sidebar changed to %s", got)
	}
}

func TestSidebarWidthCappedInTmux(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	run("set-option", "-g", "@sentinela_width", "120")
	loadPlugin(t)
	if err := toggleSidebar("./bin/tmux-sentinela", "@0"); err != nil {
		t.Fatal(err)
	}
	sidebar := run("list-panes", "-t", "@0", "-F", "#{?#{@sentinela_sidebar},#{pane_id},}")
	sidebar = strings.TrimSpace(sidebar)
	if sidebar == "" {
		t.Fatal("sidebar not opened")
	}
	if got := run("display-message", "-p", "-t", sidebar, "#{pane_width}"); got != "80" {
		t.Fatalf("initial sidebar width = %s, want 80", got)
	}
	run("select-layout", "-t", "@0", "even-horizontal")
	waitSidebarPinned(t, run, sidebar)
	if got := run("display-message", "-p", "-t", sidebar, "#{pane_width}"); got != "80" {
		t.Fatalf("restored sidebar width = %s, want 80", got)
	}
	if got := run("show-option", "-gqv", "@sentinela_width"); got != "120" {
		t.Fatalf("configured width changed in a small window: %s", got)
	}
}

func TestLockSidebarBindingWithoutSidebar(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	loadPlugin(t)
	run("split-window", "-hd", "-t", "@0", "sleep 300")
	run("select-layout", "-t", "@0", "even-vertical")
	before := run("display-message", "-p", "-t", "@0", "#{window_layout}")
	command := exec.Command("./bin/tmux-sentinela", "next-layout")
	command.Env = os.Environ()
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("next-layout without sidebar: %v: %s", err, out)
	}
	if after := run("display-message", "-p", "-t", "@0", "#{window_layout}"); after == before {
		t.Fatalf("layout did not change without sidebar: %s", after)
	}
}

func TestListPanesTitleWithTab(t *testing.T) {
	fields := []string{"%1", "123", "test", "@0", "0", "work", "0", "/tmp", "1", "1", "1", "1", "32", "160", "0", "sleep", "0", "0", "40", "40", "header\twith tab"}
	pane, ok := parsePaneLine(strings.Join(fields, "\t"))
	if !ok || pane.Title != "header\twith tab" || pane.Height != 40 || pane.WindowHeight != 40 || !pane.Sidebar {
		t.Fatalf("parsed pane with tab in title: %+v, ok=%v", pane, ok)
	}
}
