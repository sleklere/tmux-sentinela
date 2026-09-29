package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestReusableRemotePaneRequiresMatchingActiveSSH(t *testing.T) {
	p := Pane{ID: "%8", PID: os.Getpid(), Command: "ssh", RemoteHost: "dojo", RemoteSession: "dev"}
	a := Agent{Host: "dojo", Pane: Pane{Session: "dev"}}
	if got := reusableRemotePane([]Pane{p}, a); got != p.ID {
		t.Fatalf("reuse = %q, want %q", got, p.ID)
	}
	p.Command = "sh"
	if got := reusableRemotePane([]Pane{p}, a); got != "" {
		t.Fatalf("reused a pane without an SSH client: %s", got)
	}
	p.Command = "ssh"
	second := p
	second.ID = "%9"
	if got := reusableRemotePane([]Pane{p, second}, a); got != "" {
		t.Fatalf("ambiguous local SSH clients: %s", got)
	}
}

func TestRemoteAttachKeepsOnlyRemoteSidebar(t *testing.T) {
	run := isolatedTmux(t)
	sidebar := run("split-window", "-d", "-t", "%0", "-P", "-F", "#{pane_id}", "sleep 300")
	run("set-option", "-p", "-t", sidebar, "@sentinela_sidebar", "1")
	run("set-option", "-p", "-t", "%0", "@sentinela_remote_host", "dev")
	if err := suppressLocalSidebar("%0"); err != nil {
		t.Fatal(err)
	}
	if got := run("list-panes", "-t", "@0", "-F", "#{pane_id}"); got != "%0" {
		t.Fatalf("local attach panes = %q, want only %%0", got)
	}
	if got := run("show-option", "-wqv", "-t", "@0", userClosedOption); got != "1" {
		t.Fatalf("suppression option = %q", got)
	}
	if err := ensureSidebars("/bin/false"); err != nil {
		t.Fatal(err)
	}
	if got := run("list-panes", "-t", "@0", "-F", "#{pane_id}"); got != "%0" {
		t.Fatalf("ensure reopened local sidebar: %q", got)
	}
}

func TestRemoteClientTTYSelection(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-p", "-t", "%0", "@sentinela_remote_token", strings.Repeat("a", 32))
	fakeSSH(t, `printf '/dev/pts/13\n/dev/pts/14\n/dev/pts/13\n'`)
	a := Agent{Host: "dojo", Pane: Pane{Session: "dev", WindowIndex: 2, ID: "%9"}}
	if tty := remoteClientForPane(a, "%0"); tty != "/dev/pts/13" {
		t.Fatalf("client tty = %q", tty)
	}
	want := "'tmux' 'select-pane' '-t' '%9' && 'tmux' 'switch-client' '-c' '/dev/pts/13' '-t' 'dev:2'"
	if got := remoteSelectionForClient(a, "/dev/pts/13"); got != want {
		t.Fatalf("client selection = %q", got)
	}
}

func TestRemoteClientTTYRejectsUnmatchedClients(t *testing.T) {
	run := isolatedTmux(t)
	run("set-option", "-p", "-t", "%0", "@sentinela_remote_token", strings.Repeat("a", 32))
	for _, output := range []string{"/dev/pts/13\n/dev/pts/14\n", "not-a-tty\n/dev/pts/13\n", ""} {
		fakeSSH(t, "printf '%s' "+shellQuote(output))
		if got := remoteClientForPane(Agent{Host: "dojo", Pane: Pane{Session: "dev"}}, "%0"); got != "" {
			t.Fatalf("accepted %q as client tty: %q", output, got)
		}
	}
}

func scriptAttach(socket, session string) *exec.Cmd {
	args := []string{"env", "-u", "TMUX", "TERM=xterm-256color", "tmux", "-S", socket, "attach", "-t", session}
	if runtime.GOOS == "darwin" {
		// BSD script takes the command after the output file, without -c.
		return exec.Command("script", append([]string{"-q", "/dev/null"}, args...)...)
	}
	return exec.Command("script", "-q", "-c", "env -u TMUX TERM=xterm-256color tmux -S "+shellQuote(socket)+" attach -t "+shellQuote(session), "/dev/null")
}

func TestRemoteSelectionTargetsAttachedClient(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script unavailable")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	dir, err := os.MkdirTemp("", "sentinela-remote-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAllRetry(dir, 2*time.Second) })
	socket := filepath.Join(dir, "remote")
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("tmux", append([]string{"-S", socket}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("-f", "/dev/null", "new-session", "-d", "-s", "dev", "sleep 300")
	t.Cleanup(func() { exec.Command("tmux", "-S", socket, "kill-server").Run() })
	paneID := run("new-window", "-d", "-t", "dev:", "-P", "-F", "#{pane_id}", "sleep 300")
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	client := scriptAttach(socket, "dev:0")
	client.Stdin = input
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Process.Kill(); client.Wait(); input.Close(); writer.Close() })
	var tty string
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		tty = run("list-clients", "-t", "dev", "-F", "#{client_tty}")
		if tty != "" {
			break
		}
	}
	if tty == "" {
		t.Fatal("remote tmux client did not attach")
	}
	a := Agent{Pane: Pane{Session: "dev", WindowIndex: 1, ID: paneID}}
	selection := strings.ReplaceAll(remoteSelectionForClient(a, tty), "'tmux'", "tmux -S "+shellQuote(socket))
	cmd := exec.Command("sh", "-c", selection)
	cmd.Env = append(os.Environ(), "TMUX=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("selection: %v: %s", err, out)
	}
	if got := run("display-message", "-p", "-c", tty, "#{window_index}"); got != "1" {
		t.Fatalf("attached client window = %s, want 1", got)
	}
	if got := run("display-message", "-p", "-t", paneID, "#{pane_active}"); got != "1" {
		t.Fatalf("selected pane active = %s", got)
	}
}

func TestRemoteAttachSelectWithTwoTmuxServers(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	dir, err := os.MkdirTemp("", "sentinela-remote-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAllRetry(dir, 2*time.Second) })
	remoteSocket := filepath.Join(dir, "remote")
	remoteRun := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("tmux", append([]string{"-S", remoteSocket}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("remote tmux %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	remoteRun("-f", "/dev/null", "new-session", "-d", "-s", "dev", "-x", "120", "-y", "35", "sleep 300")
	t.Cleanup(func() { exec.Command("tmux", "-S", remoteSocket, "kill-server").Run() })
	remotePane := remoteRun("new-window", "-d", "-t", "dev:", "-P", "-F", "#{pane_id}", "sleep 300")
	otherPane := remoteRun("new-window", "-d", "-t", "dev:", "-P", "-F", "#{pane_id}", "sleep 300")
	// SSH stand-in: run the exact remote command chain on an independent tmux
	// server, without requiring a live sshd or touching user sessions.
	sshLog := filepath.Join(t.TempDir(), "ssh-log")
	fakeSSH(t, fmt.Sprintf(`for cmd; do :; done
printf '%%s\n' "$cmd" >> %s
cmd=$(printf '%%s' "$cmd" | sed "s|'tmux'|tmux -S %s|g")
printf 'running: %%s\n' "$cmd" >> %s
LC_ALL=C TMUX= sh -c "$cmd" 2>> %s`, shellQuote(sshLog), remoteSocket, shellQuote(sshLog), shellQuote(sshLog)))
	localRun := isolatedTmux(t)
	localRun("set-option", "-g", "remain-on-exit", "on")
	localSocket := strings.Split(os.Getenv("TMUX"), ",")[0]
	input, keepOpen, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	client := scriptAttach(localSocket, "test")
	client.Stdin, client.Stdout, client.Stderr = input, nil, nil
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Process.Kill(); client.Wait(); input.Close(); keepOpen.Close() })
	for limit := time.Now().Add(time.Second); ; {
		if out, _ := tmux("list-clients", "-F", "#{client_tty}"); out != "" {
			break
		}
		if time.Now().After(limit) {
			t.Fatal("local tmux client did not attach")
		}
		time.Sleep(20 * time.Millisecond)
	}
	a := Agent{Host: "dojo", Pane: Pane{Session: "dev", WindowIndex: 1, ID: remotePane}}
	if err := jumpAgent(a, "@0"); err != nil {
		out, _ := tmux("list-panes", "-a", "-F", "#{pane_id} #{pane_current_command} #{pane_dead}")
		t.Logf("local panes: %s", out)
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for remoteRun("display-message", "-p", "-t", "dev:", "#{window_id}") != "@1" || !strings.Contains(remoteRun("list-panes", "-t", "dev:1", "-F", "#{pane_id} #{pane_active}"), remotePane+" 1") {
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(sshLog)
			lp, _ := tmux("list-panes", "-a", "-F", "#{pane_id} #{pane_current_command} #{pane_dead}")
			capture, _ := tmux("capture-pane", "-p", "-t", "%1")
			t.Fatalf("remote attach did not select pane; ssh log: %s; clients: %s; local panes: %s; capture: %s", log, remoteRun("list-clients", "-F", "#{client_session} #{client_tty}"), lp+"; remote panes: "+remoteRun("list-panes", "-t", "dev:", "-F", "#{pane_id} #{pane_active}"), capture)
		}
		time.Sleep(20 * time.Millisecond)
	}
	panes, err := listPanes()
	if err != nil {
		t.Fatal(err)
	}
	var attached string
	for _, p := range panes {
		if p.RemoteHost == "dojo" && p.RemoteSession == "dev" {
			attached = p.ID
		}
	}
	if attached == "" {
		t.Fatalf("no marked local SSH pane: %+v", panes)
	}
	token, err := tmux("show-option", "-pqv", "-t", attached, "@sentinela_remote_token")
	if err != nil || len(token) != 32 {
		t.Fatalf("local attach token = %q: %v", token, err)
	}
	if tty := remoteRun("show-option", "-sqv", "@sentinela_client_"+token); !strings.HasPrefix(tty, "/dev/") {
		t.Fatalf("remote PTY not registered for local pane: %q", tty)
	}
	log, err := os.ReadFile(sshLog)
	if err != nil || !strings.Contains(string(log), "'-u' 'attach-session'") {
		t.Fatalf("remote tmux client must force UTF-8: %v: %s", err, log)
	}
	if got := remoteRun("list-clients", "-t", "dev", "-F", "#{client_utf8} #{client_flags}"); !strings.Contains(got, "1 ") || !strings.Contains(got, "ignore-size") {
		t.Fatalf("remote client must use UTF-8 without resizing tmux: %q", got)
	}
	// Exercise repeated agent jumps against the real attached client on an
	// isolated server. Selecting a pane in an inactive window must not show
	// the previous agent before the destination window becomes visible.
	remoteTTY := remoteRun("show-option", "-sqv", "@sentinela_client_"+token)
	for _, target := range []struct {
		pane   string
		window int
	}{{otherPane, 2}, {remotePane, 1}, {otherPane, 2}, {remotePane, 1}} {
		a.Pane.ID, a.Pane.WindowIndex = target.pane, target.window
		selection := remoteSelectionForClient(a, remoteTTY)
		cmd := exec.Command("ssh", append(sshArgs("dojo", false), selection)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("select agent in window %d: %v: %s", target.window, err, out)
		}
		if got := remoteRun("display-message", "-p", "-c", remoteTTY, "#{window_index} #{pane_id}"); got != fmt.Sprintf("%d %s", target.window, target.pane) {
			t.Fatalf("remote jump landed on %q, want window %d pane %s", got, target.window, target.pane)
		}
	}
	if got := remoteRun("list-clients", "-t", "dev", "-F", "#{client_tty}"); got != remoteTTY {
		t.Fatalf("jump created a second remote client: %q", got)
	}
	if got, err := listPanes(); err != nil || len(got) != 2 {
		t.Fatalf("jump created another local pane: %v, %+v", err, got)
	}
	// The SSH stand-in is a shell script, not a process named ssh; check
	// the real process reuse predicate independently below.
}
