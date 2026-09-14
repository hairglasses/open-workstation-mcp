package tmux

import (
	"os/exec"
	"testing"
)

func TestExactTarget(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"agent-1", "=agent-1:"},
		{"=agent-1", "=agent-1:"},
		{"=agent-1:", "=agent-1:"},
		{"$1", "$1"},
		{"@2", "@2"},
		{"%3", "%3"},
		{"sess:win", "=sess:win"},
		{"sess:0.1", "=sess:0.1"},
		{"=sess:0.1", "=sess:0.1"},
	}
	for _, tc := range cases {
		if got := exactTarget(tc.in); got != tc.want {
			t.Errorf("exactTarget(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAnchorTargetArg(t *testing.T) {
	got := anchorTargetArg([]string{"send-keys", "-t", "agent", "ls -t", "Enter"})
	if got[2] != "=agent:" || got[3] != "ls -t" {
		t.Fatalf("anchorTargetArg = %q", got)
	}
	keys := []string{"send-keys", "--", "-t", "x"}
	if got := anchorTargetArg(keys); got[3] != "x" {
		t.Fatalf("anchorTargetArg rewrote an argument after --: %q", got)
	}
}

func TestValidateKillSessionName(t *testing.T) {
	for _, bad := range []string{"", "agent-*", "agent-?", "agent-[1]"} {
		if validateKillSessionName(bad) == nil {
			t.Errorf("validateKillSessionName(%q) = nil, want error", bad)
		}
	}
	if err := validateKillSessionName("agent.1"); err != nil {
		t.Errorf("validateKillSessionName(agent.1) = %v", err)
	}
}

// TestRunTmuxExactTargetsAgainstRealTmux drives real tmux (isolated by
// TestMain) through runTmux for pane-, window- and session-target commands and
// proves a short name never prefix-matches a longer session.
func TestRunTmuxExactTargetsAgainstRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	for _, s := range []string{"owmx", "owmx-long"} {
		if _, err := runTmux("new-session", "-d", "-s", s, "sh"); err != nil {
			t.Fatalf("new-session %s: %v", s, err)
		}
	}
	t.Cleanup(func() {
		_, _ = runTmux("kill-session", "-t", "owmx")
		_, _ = runTmux("kill-session", "-t", "owmx-long")
	})
	if out, err := runTmux("display-message", "-p", "#{socket_path}"); err != nil || len(out) == 0 || out[:5] != "/tmp/" {
		t.Fatalf("not isolated: socket=%q err=%v", out, err)
	}
	if _, err := runTmux("send-keys", "-t", "owmx", "echo EXACT_OK", "Enter"); err != nil {
		t.Fatalf("send-keys pane target: %v", err)
	}
	if _, err := runTmux("capture-pane", "-p", "-t", "owmx"); err != nil {
		t.Fatalf("capture-pane pane target: %v", err)
	}
	if _, err := runTmux("list-panes", "-t", "owmx:0"); err != nil {
		t.Fatalf("list-panes window target: %v", err)
	}
	if _, err := runTmux("has-session", "-t", "owmx-l"); err == nil {
		t.Fatal("has-session owmx-l prefix-matched owmx-long")
	}
	if _, err := runTmux("kill-session", "-t", "owmx"); err != nil {
		t.Fatalf("kill-session: %v", err)
	}
	if _, err := runTmux("has-session", "-t", "owmx-long"); err != nil {
		t.Fatal("killing owmx also removed owmx-long")
	}
}
