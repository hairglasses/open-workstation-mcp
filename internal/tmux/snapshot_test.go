package tmux

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func TestSnapshot_Validation(t *testing.T) {
	td := findTool(t, "tmux_snapshot")

	for _, invalid := range []string{"", "   "} {
		req := makeReq(map[string]any{"session": invalid})
		result, err := td.Handler(context.Background(), req)
		if err != nil {
			if !strings.Contains(err.Error(), "INVALID_PARAM") {
				t.Errorf("expected INVALID_PARAM for session %q, got %v", invalid, err)
			}
			continue
		}
		if result != nil && result.IsError {
			continue
		}
		t.Errorf("expected INVALID_PARAM for session %q", invalid)
	}
}

func TestSnapshot_NonexistentSession(t *testing.T) {
	requireTmux(t)
	td := findTool(t, "tmux_snapshot")

	req := makeReq(map[string]any{"session": "nonexistent-sess-xyz-98765"})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		if !strings.Contains(err.Error(), "NOT_FOUND") {
			t.Errorf("expected NOT_FOUND for nonexistent session, got %v", err)
		}
		return
	}
	if result != nil && result.IsError {
		return
	}
	t.Fatal("expected NOT_FOUND error for nonexistent session")
}

func TestSnapshot_LiveSession(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-snap-%d", rand.Intn(100000))

	// Create test session
	newTd := findTool(t, "tmux_new_session")
	req := makeReq(map[string]any{"name": sessName})
	result, err := newTd.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("new_session error: %v", err)
	}
	var newOut NewSessionOutput
	unmarshalResult(t, result, &newOut)
	if !newOut.Created {
		t.Fatal("expected session created")
	}

	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	// Snapshot the session
	snapTd := findTool(t, "tmux_snapshot")
	snapReq := makeReq(map[string]any{"session": sessName})
	snapRes, err := snapTd.Handler(context.Background(), snapReq)
	if err != nil {
		t.Fatalf("snapshot handler error: %v", err)
	}
	if snapRes != nil && snapRes.IsError {
		t.Fatalf("snapshot returned error: %v", snapRes)
	}

	var snapOut SnapshotOutput
	unmarshalResult(t, snapRes, &snapOut)

	if snapOut.Session != sessName {
		t.Errorf("expected session %q, got %q", sessName, snapOut.Session)
	}
	if snapOut.WindowCount == 0 {
		t.Errorf("expected at least 1 window in snapshot")
	}
	if snapOut.PaneCount == 0 {
		t.Errorf("expected at least 1 pane in snapshot")
	}
	if len(snapOut.Windows) == 0 {
		t.Fatalf("expected windows slice to be non-empty")
	}

	firstWin := snapOut.Windows[0]
	if firstWin.Layout == "" {
		t.Errorf("expected non-empty layout in window")
	}
	if len(firstWin.Panes) == 0 {
		t.Errorf("expected at least 1 pane in window")
	}
}
