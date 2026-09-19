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

func loadPlugin(t *testing.T) {
	t.Helper()
	cmd := exec.Command("bash", "tmux-sentinela.tmux")
	cmd.Env = append(os.Environ(), "GOFLAGS="+strings.TrimSpace(os.Getenv("GOFLAGS")+" -modcacherw"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("load plugin: %v: %s", err, out)
	}
}

func TestSidebarFocusIsRestoredAfterWindowSwitch(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	run("new-window", "-d", "-t", "test:", "-n", "other", "sleep 300")
	loadPlugin(t)

	sidebar := run("split-window", "-hd", "-t", "@0", "-P", "-F", "#{pane_id}", "sleep 300")
	run("set-option", "-p", "-t", sidebar, "@sentinela_sidebar", "1")

	// Pane navigation can still focus the sidebar for keyboard interaction.
	run("select-pane", "-t", sidebar)
	if got := run("display-message", "-p", "-t", "@0", "#{pane_id}"); got != sidebar {
		t.Fatalf("pane after selecting sidebar = %q, want %s", got, sidebar)
	}

	// Returning to that window restores focus to its last work pane.
	run("select-window", "-t", "@1")
	run("select-window", "-t", "@0")
	if got := run("display-message", "-p", "-t", "@0", "#{pane_id}"); got != "%0" {
		t.Fatalf("pane after returning to window = %q, want work pane %%0", got)
	}
}

func TestListPanesIncludesScreenMetadata(t *testing.T) {
	run := isolatedTmux(t)
	run("select-pane", "-t", "%0", "-T", "agent title")
	panes, err := listPanes()
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 1 || panes[0].Command == "" || panes[0].Title != "agent title" {
		t.Fatalf("panes = %+v, want one pane with its current command and title", panes)
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

func TestToggleOffStaysClosedAfterNewWindow(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_notify", "off")
	run("set-option", "-g", "@sentinela_notify_done", "off")
	run("set-option", "-g", "@sentinela_sound", "off")
	loadPlugin(t)

	// Create a second work pane so the window survives sidebar closing.
	run("split-window", "-d", "-t", "@0", "sleep 300")

	// Verify sidebar exists after load.
	sidebars := run("list-panes", "-t", "@0", "-F", "#{@sentinela_sidebar}")
	if strings.Count(sidebars, "1") != 1 {
		t.Fatalf("expected 1 sidebar after load, got: %s", sidebars)
	}

	// User deliberately closes the sidebar (toggle off).
	bin := "./bin/tmux-sentinela"
	cmd := exec.Command(bin, "toggle", "@0")
	cmd.Env = append(os.Environ(), "TMUX="+run("display-message", "-p", "#{socket_path}")+",0,0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("toggle failed: %v: %s", err, out)
	}

	// Verify sidebar is closed.
	sidebars = run("list-panes", "-t", "@0", "-F", "#{@sentinela_sidebar}")
	if strings.Count(sidebars, "1") != 0 {
		t.Fatalf("expected 0 sidebars after toggle off, got: %s", sidebars)
	}

	// Zoom the work pane.
	run("select-pane", "-t", "%0")
	run("resize-pane", "-Z", "-t", "%0")
	zoomed := run("display-message", "-p", "-t", "@0", "#{window_zoomed_flag}")
	if zoomed != "1" {
		t.Fatalf("expected window to be zoomed, got %q", zoomed)
	}

	// Create a new window - this triggers after-new-window hook -> ensure.
	run("new-window", "-d", "-t", "test:", "-n", "other", "sleep 300")

	// Give ensure time to run.
	time.Sleep(500 * time.Millisecond)

	// Verify the original window's sidebar stayed closed.
	sidebars = run("list-panes", "-t", "@0", "-F", "#{@sentinela_sidebar}")
	if strings.Count(sidebars, "1") != 0 {
		t.Fatalf("expected 0 sidebars in @0 after new-window, got: %s", sidebars)
	}

	// Verify the original window stayed zoomed.
	zoomed = run("display-message", "-p", "-t", "@0", "#{window_zoomed_flag}")
	if zoomed != "1" {
		t.Fatalf("expected window @0 to stay zoomed, got %q", zoomed)
	}

	// Verify the new window got a sidebar (autocreate is on by default).
	sidebars = run("list-panes", "-t", "@1", "-F", "#{@sentinela_sidebar}")
	if strings.Count(sidebars, "1") != 1 {
		t.Fatalf("expected 1 sidebar in new window @1, got: %s", sidebars)
	}
}

func TestAutocreateOffPreventsSidebarInNewWindows(t *testing.T) {
	for _, tc := range []struct {
		name          string
		autocreate    string
		expectSidebar bool
	}{
		{"off", "off", false},
		{"on", "on", true},
		{"absent", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := isolatedTmux(t)
			run("set-option", "-g", "@sentinela_notify", "off")
			run("set-option", "-g", "@sentinela_notify_done", "off")
			run("set-option", "-g", "@sentinela_sound", "off")
			if tc.autocreate != "" {
				run("set-option", "-g", "@sentinela_autocreate", tc.autocreate)
			}
			loadPlugin(t)

			// Check what panes exist after loadPlugin
			allPanes := run("list-panes", "-a", "-F", "#{window_id} #{pane_id} #{@sentinela_sidebar}")
			t.Logf("all panes after loadPlugin: %s", allPanes)

			// Create a new window - this triggers after-new-window hook -> ensure.
			run("new-window", "-d", "-t", "test:", "-n", "work", "sleep 300")

			// Give ensure time to run.
			time.Sleep(1000 * time.Millisecond)

			// Check what panes exist
			allPanes = run("list-panes", "-a", "-F", "#{window_id} #{pane_id} #{@sentinela_sidebar}")
			t.Logf("all panes after new-window: %s", allPanes)

			sidebars := run("list-panes", "-t", "@1", "-F", "#{@sentinela_sidebar}")
			count := strings.Count(sidebars, "1")
			if tc.expectSidebar && count != 1 {
				t.Fatalf("expected sidebar in new window, got %d", count)
			}
			if !tc.expectSidebar && count != 0 {
				t.Fatalf("expected no sidebar in new window, got %d", count)
			}
		})
	}
}

func TestManualToggleWorksWithAutocreateOff(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	run("set-option", "-g", "@sentinela_notify", "off")
	run("set-option", "-g", "@sentinela_notify_done", "off")
	run("set-option", "-g", "@sentinela_sound", "off")
	loadPlugin(t)

	// Initial window should have no sidebar (autocreate off).
	sidebars := run("list-panes", "-t", "@0", "-F", "#{@sentinela_sidebar}")
	if strings.Count(sidebars, "1") != 0 {
		t.Fatalf("expected 0 sidebars initially with autocreate off, got: %s", sidebars)
	}

	// Manual toggle should open a sidebar.
	bin := "./bin/tmux-sentinela"
	cmd := exec.Command(bin, "toggle", "@0")
	cmd.Env = append(os.Environ(), "TMUX="+run("display-message", "-p", "#{socket_path}")+",0,0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("toggle open failed: %v: %s", err, out)
	}

	sidebars = run("list-panes", "-t", "@0", "-F", "#{@sentinela_sidebar}")
	if strings.Count(sidebars, "1") != 1 {
		t.Fatalf("expected 1 sidebar after manual toggle open, got: %s", sidebars)
	}

	// Manual toggle should close it again.
	cmd = exec.Command(bin, "toggle", "@0")
	cmd.Env = append(os.Environ(), "TMUX="+run("display-message", "-p", "#{socket_path}")+",0,0")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("toggle close failed: %v: %s", err, out)
	}

	sidebars = run("list-panes", "-t", "@0", "-F", "#{@sentinela_sidebar}")
	if strings.Count(sidebars, "1") != 0 {
		t.Fatalf("expected 0 sidebars after manual toggle close, got: %s", sidebars)
	}

	// Creating a new window should NOT open a sidebar (autocreate off).
	run("new-window", "-d", "-t", "test:", "-n", "other", "sleep 300")
	time.Sleep(500 * time.Millisecond)

	sidebars = run("list-panes", "-t", "@1", "-F", "#{@sentinela_sidebar}")
	if strings.Count(sidebars, "1") != 0 {
		t.Fatalf("expected 0 sidebars in new window with autocreate off, got: %s", sidebars)
	}

	// But manual toggle should still work in the new window.
	cmd = exec.Command(bin, "toggle", "@1")
	cmd.Env = append(os.Environ(), "TMUX="+run("display-message", "-p", "#{socket_path}")+",0,0")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("toggle open in new window failed: %v: %s", err, out)
	}

	sidebars = run("list-panes", "-t", "@1", "-F", "#{@sentinela_sidebar}")
	if strings.Count(sidebars, "1") != 1 {
		t.Fatalf("expected 1 sidebar after manual toggle in new window, got: %s", sidebars)
	}
}
