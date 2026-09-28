package main

import "testing"

func TestSSHDestinationIgnoresCommandArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"other", "echo", "dojo"}, "other"},
		{[]string{"-t", "-p", "2222", "-o", "ProxyJump=dojo", "user@argos", "tmux", "attach"}, "user@argos"},
		{[]string{"-o", "BatchMode=yes", "--", "dojo", "echo", "argos"}, "dojo"},
	} {
		if got := sshDestination(tc.args); got != tc.want {
			t.Fatalf("ssh destination %v = %q, want %q", tc.args, got, tc.want)
		}
	}
}
