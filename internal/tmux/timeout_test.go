package tmux

import (
	"strings"
	"testing"
	"time"
)

func TestRunCmdTimeout(t *testing.T) {
	orig := defaultTmuxCmdTimeout
	defaultTmuxCmdTimeout = 50 * time.Millisecond
	defer func() { defaultTmuxCmdTimeout = orig }()

	_, _, err := runCmd("sleep", "2")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected context deadline / killed error, got %v", err)
	}
}
