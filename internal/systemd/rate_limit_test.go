package systemd

import (
	"strings"
	"testing"
	"time"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
)

func TestCheckWriteRateLimit(t *testing.T) {
	resetWriteRateLimits()
	defer resetWriteRateLimits()

	unitA := "test-a.service"
	unitB := "test-b.service"

	// 1. Initial write to unitA succeeds
	if err := checkWriteRateLimit(false, "restart", unitA); err != nil {
		t.Fatalf("first restart should succeed: %v", err)
	}

	// 2. Rapid-fire second write to unitA must be rate limited
	err := checkWriteRateLimit(false, "restart", unitA)
	if err == nil {
		t.Fatal("expected rate limit error on rapid-fire second write to unitA")
	}
	if !strings.Contains(err.Error(), string(handler.ErrRateLimited)) {
		t.Errorf("expected error to contain %q, got %q", handler.ErrRateLimited, err.Error())
	}

	// 3. Concurrent write to different unitB should succeed (per-unit isolation)
	if err := checkWriteRateLimit(false, "restart", unitB); err != nil {
		t.Fatalf("write to unitB should succeed: %v", err)
	}

	// 4. Write to unitA under system scope (system: true) should succeed (scope isolation)
	if err := checkWriteRateLimit(true, "restart", unitA); err != nil {
		t.Fatalf("write to system scope unitA should succeed: %v", err)
	}

	// 5. Non-lifecycle operations (enable, disable) bypass toggle rate limiting
	if err := checkWriteRateLimit(false, "enable", unitA); err != nil {
		t.Fatalf("enable should not be rate-limited: %v", err)
	}
	if err := checkWriteRateLimit(false, "disable", unitA); err != nil {
		t.Fatalf("disable should not be rate-limited: %v", err)
	}

	// 6. Reset allows write to unitA again
	resetWriteRateLimits()
	if err := checkWriteRateLimit(false, "start", unitA); err != nil {
		t.Fatalf("write after reset should succeed: %v", err)
	}
}

func TestWriteRateLimit_CustomInterval(t *testing.T) {
	resetWriteRateLimits()
	defer resetWriteRateLimits()

	origInterval := minWriteInterval
	minWriteInterval = 20 * time.Millisecond
	defer func() { minWriteInterval = origInterval }()

	unit := "fast-test.service"
	if err := checkWriteRateLimit(false, "stop", unit); err != nil {
		t.Fatalf("first stop should succeed: %v", err)
	}

	// Immediate retry fails
	if err := checkWriteRateLimit(false, "stop", unit); err == nil {
		t.Fatal("expected immediate retry to fail")
	}

	// Wait beyond the interval
	time.Sleep(30 * time.Millisecond)

	// Now it succeeds
	if err := checkWriteRateLimit(false, "start", unit); err != nil {
		t.Fatalf("expected write after interval to succeed, got: %v", err)
	}
}
