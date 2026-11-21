package systemd

import (
	"strings"
	"testing"
	"time"
)

func TestExecCommandRunnerTimeout(t *testing.T) {
	orig := defaultCommandTimeout
	defaultCommandTimeout = 50 * time.Millisecond
	defer func() { defaultCommandTimeout = orig }()

	runner := execCommandRunner{}
	_, _, err := runner.Run("sleep", "2")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected context deadline / killed error, got %v", err)
	}
}
