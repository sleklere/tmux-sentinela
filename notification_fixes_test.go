package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeNotifier(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "notifications.log")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$SENTINELA_NOTIFY_LOG\"\n"
	for _, name := range []string{"notify-send", "terminal-notifier"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SENTINELA_NOTIFY_LOG", log)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return log
}

func waitNotificationLines(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, _ := os.ReadFile(path)
		lines := strings.FieldsFunc(string(data), func(r rune) bool { return r == '\n' })
		if len(lines) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("notification lines = %d, want %d; data=%q", len(lines), want, data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAuditFirstBlockedObservationIsSilent verifies that starting with an agent
// already blocked does not generate a historical notification.
// This preserves the existing intentional behavior.
func TestAuditFirstBlockedObservationIsSilent(t *testing.T) {
	isolateState(t)
	log := fakeNotifier(t)
	m := testModel()
	m.notify = "desktop"
	agent := Agent{Key: "claude:101", Kind: "claude", Name: "agent", Status: Blocked}
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	time.Sleep(100 * time.Millisecond)
	if data, _ := os.ReadFile(log); len(data) != 0 {
		t.Fatalf("first blocked observation unexpectedly notified: %q", data)
	}

	// But a subsequent Idle->Blocked transition should notify
	agent.Status = Idle
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	agent.Status = Blocked
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	waitNotificationLines(t, log, 1)
}

// TestAuditAgentReappearsBlockedAfterLoss verifies F7 fix:
// an agent that disappears for a poll and reappears blocked should notify.
func TestAuditAgentReappearsBlockedAfterLoss(t *testing.T) {
	isolateState(t)
	log := fakeNotifier(t)
	m := testModel()
	m.notify = "desktop"
	agent := Agent{Key: "opencode:202", Kind: "opencode", Name: "agent", Status: Idle}

	// First poll: agent is Idle
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})

	// Second poll: agent disappears (not in the list)
	m.applyPoll(pollMsg{agents: []Agent{}, leader: true})

	// Third poll: agent reappears as Blocked
	agent.Status = Blocked
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	waitNotificationLines(t, log, 1)
}

// TestAuditLeaderChangeDoesNotDuplicateBlockedNotification verifies F9 fix:
// when the leader sidebar exits and another becomes leader, it should not
// re-notify a transition that was already announced.
func TestAuditLeaderChangeDoesNotDuplicateBlockedNotification(t *testing.T) {
	for i := 0; i < 10; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			isolateState(t)
			log := fakeNotifier(t)
			leader, follower := testModel(), testModel()
			leader.notify, follower.notify = "desktop", "desktop"
			agent := Agent{Key: "opencode:202", Kind: "opencode", Name: "agent", Status: Idle}

			// Both sidebars see Idle
			leader.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
			follower.applyPoll(pollMsg{agents: []Agent{agent}, leader: false})

			// Leader sees Blocked and notifies
			agent.Status = Blocked
			leader.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
			waitNotificationLines(t, log, 1)

			// Leader exits, follower becomes leader before polling blocked state
			// Follower still has prev=Idle in its local map, but shared notified state should prevent dup
			follower.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
			// Small delay to allow goroutine to complete
			time.Sleep(50 * time.Millisecond)
			waitNotificationLines(t, log, 1) // Still 1, not 2
		})
	}
}

// TestAuditSeparateTransitionsCanNotifyAgain verifies that after clearing the
// notified state (simulating TTL expiration), a new genuine transition can notify again.
func TestAuditSeparateTransitionsCanNotifyAgain(t *testing.T) {
	isolateState(t)
	log := fakeNotifier(t)
	m := testModel()
	m.notify = "desktop"
	agent := Agent{Key: "claude:303", Kind: "claude", Name: "agent", Status: Idle}

	// First transition: Idle -> Blocked
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	agent.Status = Blocked
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	waitNotificationLines(t, log, 1)

	// Back to Idle
	agent.Status = Idle
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})

	// Clear notified state to simulate TTL expiration
	clearNotified(agent.Key, "blocked")

	// Second transition: Idle -> Blocked (genuine new transition)
	agent.Status = Blocked
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	waitNotificationLines(t, log, 2)
}

// TestAuditVisiblePaneDoesNotNotify verifies that visible panes don't trigger
// blocked notifications (guard preserved).
func TestAuditVisiblePaneDoesNotNotify(t *testing.T) {
	isolateState(t)
	log := fakeNotifier(t)
	m := testModel()
	m.notify = "desktop"
	agent := Agent{Key: "opencode:404", Kind: "opencode", Name: "agent", Status: Idle, Pane: Pane{Visible: true}}

	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	agent.Status = Blocked
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	time.Sleep(100 * time.Millisecond)
	if data, _ := os.ReadFile(log); len(data) != 0 {
		t.Fatalf("visible pane unexpectedly notified: %q", data)
	}
}

// TestAuditFollowerSidebarDoesNotNotify verifies that follower sidebars don't
// trigger notifications (guard preserved).
func TestAuditFollowerSidebarDoesNotNotify(t *testing.T) {
	isolateState(t)
	log := fakeNotifier(t)
	leader, follower := testModel(), testModel()
	leader.notify, follower.notify = "desktop", "desktop"
	agent := Agent{Key: "claude:505", Kind: "claude", Name: "agent", Status: Idle}

	leader.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	follower.applyPoll(pollMsg{agents: []Agent{agent}, leader: false})

	agent.Status = Blocked
	follower.applyPoll(pollMsg{agents: []Agent{agent}, leader: false})
	time.Sleep(100 * time.Millisecond)
	if data, _ := os.ReadFile(log); len(data) != 0 {
		t.Fatalf("follower sidebar unexpectedly notified: %q", data)
	}
}

// TestAuditNotifyOffModeDoesNotNotify verifies that notify=off mode doesn't
// trigger notifications (guard preserved).
func TestAuditNotifyOffModeDoesNotNotify(t *testing.T) {
	isolateState(t)
	log := fakeNotifier(t)
	m := testModel()
	m.notify = "off"
	agent := Agent{Key: "opencode:606", Kind: "opencode", Name: "agent", Status: Idle}

	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	agent.Status = Blocked
	m.applyPoll(pollMsg{agents: []Agent{agent}, leader: true})
	time.Sleep(100 * time.Millisecond)
	if data, _ := os.ReadFile(log); len(data) != 0 {
		t.Fatalf("notify=off mode unexpectedly notified: %q", data)
	}
}
