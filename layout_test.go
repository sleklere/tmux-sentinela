package main

import (
	"reflect"
	"testing"
)

func TestPlanSidebarWidths(t *testing.T) {
	a := Pane{ID: "%1", WindowID: "@1", Sidebar: true, Width: 32, WindowWidth: 160}
	b := Pane{ID: "%2", WindowID: "@2", Sidebar: true, Width: 32, WindowWidth: 160}
	workA := Pane{ID: "%3", WindowID: "@1", Width: 128, WindowWidth: 160}
	for _, tt := range []struct {
		name       string
		before     []Pane
		after      []Pane
		previous   int
		configured int
		wantWidth  int
		resizes    []paneResize
	}{
		{"unchanged", []Pane{a, b}, []Pane{a, b}, 32, 32, 32, nil},
		{"layout drift is not a manual resize", []Pane{a, b}, []Pane{a, resized(b, 40)}, 32, 32, 32, []paneResize{{b.ID, 32}}},
		{"oversized layout drift is capped", []Pane{a, b}, []Pane{a, resized(b, 90)}, 32, 32, 32, []paneResize{{b.ID, 32}}},
		{"configured width capped without losing preference", []Pane{resized(a, 100)}, []Pane{resized(a, 100)}, 100, 100, 100, []paneResize{{a.ID, 80}}},
		{"new sidebar", []Pane{a}, []Pane{a, resized(b, 40)}, 32, 32, 32, []paneResize{{b.ID, 32}}},
		{"new leader", nil, []Pane{a, resized(b, 40)}, 0, 32, 32, []paneResize{{b.ID, 32}}},
		{"explicit option wins", []Pane{a, b}, []Pane{a, resized(b, 40)}, 32, 48, 48, []paneResize{{a.ID, 48}, {b.ID, 48}}},
		{"resize hook publishes shared width", []Pane{a, b}, []Pane{a, resized(b, 40)}, 32, 40, 40, []paneResize{{a.ID, 40}}},
		{"terminal resized", []Pane{a, b}, []Pane{a, {ID: b.ID, Sidebar: true, Width: 20, WindowWidth: 100}}, 32, 32, 32, []paneResize{{b.ID, 32}}},
		{"zoom", []Pane{a, b}, []Pane{a, {ID: b.ID, Sidebar: true, Width: 160, WindowWidth: 160, Zoomed: true}}, 32, 32, 32, nil},
		{"unzoom", []Pane{{ID: a.ID, Sidebar: true, Width: 160, WindowWidth: 160, Zoomed: true}}, []Pane{resized(a, 40)}, 32, 32, 32, []paneResize{{a.ID, 32}}},
		{"clamped width is stable", []Pane{resized(a, 20)}, []Pane{resized(a, 20)}, 32, 32, 32, nil},
		{"work pane resize ignored", []Pane{{ID: "%work", Width: 80}}, []Pane{{ID: "%work", Width: 60}}, 32, 32, 32, nil},
		{"work pane closed", []Pane{a, workA, b}, []Pane{resized(a, 160), b}, 32, 32, 32, []paneResize{{a.ID, 32}}},
		{"no sidebars", []Pane{a}, nil, 32, 32, 32, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			width, resizes := planSidebarWidths(tt.after, sidebarLayout{panes: tt.before, width: tt.previous}, tt.configured)
			if width != tt.wantWidth || !reflect.DeepEqual(resizes, tt.resizes) {
				t.Fatalf("plan = %d, %+v; want %d, %+v", width, resizes, tt.wantWidth, tt.resizes)
			}
		})
	}
}

func TestSidebarWidthLimit(t *testing.T) {
	for _, tc := range []struct{ requested, window, want int }{
		{32, 160, 32}, {120, 160, 80}, {120, 143, 71}, {120, 1, 1},
	} {
		if got := sidebarWidth(tc.requested, tc.window); got != tc.want {
			t.Errorf("sidebarWidth(%d, %d) = %d, want %d", tc.requested, tc.window, got, tc.want)
		}
	}
}

func resized(p Pane, width int) Pane {
	p.Width = width
	return p
}

func TestLayoutDoesNotPublishSidebarWidth(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	loadPlugin(t)
	sidebar := run("split-window", "-hbdf", "-l", "32", "-t", "@0", "-P", "-F", "#{pane_id}", "sleep 300")
	run("set-option", "-p", "-t", sidebar, "@sentinela_sidebar", "1")
	m := testModel()
	m.self = sidebar
	poll := func() {
		t.Helper()
		msg := m.poll(0).(pollMsg)
		if msg.err != nil {
			t.Fatal(msg.err)
		}
		m.applyPoll(msg)
	}
	poll()
	// Force the precise race: polling observes the rearranged window before
	// the asynchronous layout repair gets a chance to run.
	run("set-hook", "-gu", "window-layout-changed[50]")
	run("next-layout", "-t", "@0")
	if got := run("display-message", "-p", "-t", sidebar, "#{pane_width}"); got == "32" {
		t.Fatal("layout did not resize the sidebar; race not reproduced")
	}
	poll()
	if got := run("show-option", "-gqv", "@sentinela_width"); got != "" && got != "32" {
		t.Fatalf("layout width leaked into shared preference: %s", got)
	}
}

func TestSidebarWidthSyncCapsManualResize(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	loadPlugin(t)
	sidebar := run("split-window", "-hbdf", "-l", "32", "-t", "@0", "-P", "-F", "#{pane_id}", "sleep 300")
	run("set-option", "-p", "-t", sidebar, "@sentinela_sidebar", "1")
	m := testModel()
	m.self = sidebar
	poll := func() {
		t.Helper()
		msg := m.poll(0).(pollMsg)
		if msg.err != nil {
			t.Fatal(msg.err)
		}
		m.applyPoll(msg)
	}
	poll()
	run("resize-pane", "-t", sidebar, "-x", "100")
	poll()
	if got := run("display-message", "-p", "-t", sidebar, "#{pane_width}"); got != "80" {
		t.Fatalf("resized sidebar width = %s, want 80", got)
	}
	if got := run("show-option", "-gqv", "@sentinela_width"); got != "80" {
		t.Fatalf("shared width = %s, want 80", got)
	}
}

func TestSidebarWidthSyncAcrossWindows(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-g", "@sentinela_autocreate", "off")
	loadPlugin(t)
	addSidebar := func(window string) string {
		id := run("split-window", "-hbdf", "-t", window, "-l", "32", "-P", "-F", "#{pane_id}", "sleep 300")
		run("set-option", "-p", "-t", id, "@sentinela_sidebar", "1")
		return id
	}
	a := addSidebar("@0")
	window := run("new-window", "-d", "-P", "-F", "#{window_id}", "/bin/sh")
	b := addSidebar(window)
	m := testModel()
	m.self = a
	poll := func() {
		t.Helper()
		msg := m.poll(0).(pollMsg)
		if msg.err != nil {
			t.Fatal(msg.err)
		}
		m.applyPoll(msg)
	}
	assertWidth := func(id, want string) {
		t.Helper()
		if got := run("display-message", "-p", "-t", id, "#{pane_width}"); got != want {
			t.Fatalf("pane %s width = %s, want %s", id, got, want)
		}
	}
	poll()
	run("resize-pane", "-t", b, "-x", "44")
	poll()
	assertWidth(a, "44")
	assertWidth(b, "44")
	if got := run("show-option", "-gqv", "@sentinela_width"); got != "44" {
		t.Fatalf("shared width = %q, want 44", got)
	}
	window = run("new-window", "-d", "-P", "-F", "#{window_id}", "/bin/sh")
	c := addSidebar(window)
	poll()
	assertWidth(c, "44")
	run("resize-pane", "-t", b, "-Z")
	poll()
	assertWidth(a, "44")
	if got := run("display-message", "-p", "-t", b, "#{window_zoomed_flag}"); got != "1" {
		t.Fatal("sync cancelled zoom")
	}
	run("set-option", "-g", "@sentinela_width", "36")
	poll()
	assertWidth(a, "36")
	assertWidth(c, "36")
	run("resize-pane", "-t", b, "-Z")
	poll()
	assertWidth(b, "36")
	// A constrained window must not make every other sidebar shrink.
	run("resize-window", "-t", window, "-x", "30", "-y", "40")
	poll()
	poll()
	assertWidth(a, "36")
	if got := run("show-option", "-gqv", "@sentinela_width"); got != "36" {
		t.Fatalf("constrained window overwrote shared width: %q", got)
	}
	run("resize-window", "-t", window, "-x", "160", "-y", "40")
	poll()
	assertWidth(c, "36")
	// Observe the transient orphan before the prune hook closes it.
	run("set-hook", "-gu", "after-kill-pane[50]")
	run("set-hook", "-gwu", "pane-exited[50]")
	run("kill-pane", "-t", "%0")
	poll()
	assertWidth(c, "36")
	if got := run("show-option", "-gqv", "@sentinela_width"); got != "36" {
		t.Fatalf("orphaned sidebar overwrote shared width: %q", got)
	}
}
