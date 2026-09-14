package main

import "testing"

func TestPaneOfFindsAgentInHiddenWindow(t *testing.T) {
	want := Pane{ID: "%2", PID: 100, Visible: false, Current: false}
	panes := map[int]Pane{want.PID: want}
	parents := map[int]int{300: 200, 200: want.PID}

	got, ok := paneOf(300, parents, panes)
	if !ok {
		t.Fatal("paneOf() did not find the hidden pane")
	}
	if got != want {
		t.Fatalf("paneOf() = %+v, want %+v", got, want)
	}
}
