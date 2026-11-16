package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMain isolates every test in this package from the operator's live tmux
// servers. Tests here create and kill real sessions; on 2026-09-13 they were
// observed doing so on the fleet's default server (test-layout-*, test-ws-*,
// sess-applied-* sessions). A private TMUX_TMPDIR, no inherited $TMUX (which
// tmux consults before TMUX_TMPDIR), and a HOME/XDG_CONFIG_HOME without a
// tmux.conf (so continuum never restores saved sessions into the test server)
// keep every server this package starts private and disposable.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "owm-tmux-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tmux test isolation:", err)
		os.Exit(1)
	}
	_ = os.WriteFile(filepath.Join(dir, ".tmux.conf"), []byte("set-option -s exit-empty off\nset-option -g default-command \"bash --norc --noprofile -i\"\n"), 0o600)
	_ = os.Unsetenv("TMUX")
	_ = os.Unsetenv("TMUX_PANE")
	_ = os.Setenv("TMUX_TMPDIR", dir)
	_ = os.Setenv("HOME", dir)
	_ = os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	_ = exec.Command("tmux", "kill-server").Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
