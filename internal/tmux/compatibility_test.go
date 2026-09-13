package tmux

import (
	"context"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// Compatibility smoke tests: no tmux server running
// ---------------------------------------------------------------------------

func isolateTmuxSocket(t *testing.T) {
	t.Helper()
	requireTmux(t)
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")
}

func TestCompatibility_NoServer_ListSessions(t *testing.T) {
	isolateTmuxSocket(t)

	td := findTool(t, "tmux_list_sessions")
	req := makeReq(nil)
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error when no server is running: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Fatalf("expected graceful success for list_sessions when no server running, got error: %s", extractText(t, result))
	}

	var out ListSessionsOutput
	unmarshalResult(t, result, &out)
	if len(out.Sessions) != 0 {
		t.Fatalf("expected 0 sessions from isolated empty tmux socket, got %d", len(out.Sessions))
	}
}

func TestCompatibility_NoServer_KillSession(t *testing.T) {
	isolateTmuxSocket(t)

	td := findTool(t, "tmux_kill_session")
	req := makeReq(map[string]any{"name": "any-session"})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		return // error is acceptable
	}
	if result == nil || !result.IsError {
		t.Fatal("expected error result for kill_session when no server is running")
	}
}

func TestCompatibility_NoServer_ListWindows(t *testing.T) {
	isolateTmuxSocket(t)

	td := findTool(t, "tmux_list_windows")
	req := makeReq(map[string]any{"session": "nonexistent"})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Fatalf("expected graceful empty list when no server is running, got error: %s", extractText(t, result))
	}

	var out ListWindowsOutput
	unmarshalResult(t, result, &out)
	if len(out.Windows) != 0 {
		t.Fatalf("expected 0 windows when no server running, got %d", len(out.Windows))
	}
}

func TestCompatibility_NoServer_ListPanes(t *testing.T) {
	isolateTmuxSocket(t)

	td := findTool(t, "tmux_list_panes")
	req := makeReq(map[string]any{"session": "nonexistent"})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Fatalf("expected graceful empty list when no server is running, got error: %s", extractText(t, result))
	}

	var out ListPanesOutput
	unmarshalResult(t, result, &out)
	if len(out.Panes) != 0 {
		t.Fatalf("expected 0 panes when no server running, got %d", len(out.Panes))
	}
}

func TestCompatibility_NoServer_CapturePane(t *testing.T) {
	isolateTmuxSocket(t)

	td := findTool(t, "tmux_capture_pane")
	req := makeReq(map[string]any{"session": "nonexistent"})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		return // error is acceptable
	}
	if result == nil || !result.IsError {
		t.Fatal("expected error result for capture_pane when no server is running")
	}
}

func TestCompatibility_NoServer_SearchPanes(t *testing.T) {
	isolateTmuxSocket(t)

	td := findTool(t, "tmux_search_panes")
	req := makeReq(map[string]any{"session": "nonexistent", "pattern": "test"})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Fatalf("expected graceful empty matches when no server is running, got error: %s", extractText(t, result))
	}

	var out SearchPanesOutput
	unmarshalResult(t, result, &out)
	if len(out.Matches) != 0 {
		t.Fatalf("expected 0 matches when no server running, got %d", len(out.Matches))
	}
}

func TestCompatibility_NoServer_WaitForText(t *testing.T) {
	isolateTmuxSocket(t)

	td := findTool(t, "tmux_wait_for_text")
	req := makeReq(map[string]any{
		"session":      "nonexistent",
		"text":         "target-phrase",
		"timeout_secs": 1,
	})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		return // error is acceptable
	}
	if result == nil || !result.IsError {
		t.Fatal("expected error or timeout result for wait_for_text when no server is running")
	}
}

func TestCompatibility_NoServer_ResizePane(t *testing.T) {
	isolateTmuxSocket(t)

	td := findTool(t, "tmux_resize_pane")
	req := makeReq(map[string]any{
		"session": "nonexistent",
		"zoom":    true,
	})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		return // error is acceptable
	}
	if result == nil || !result.IsError {
		t.Fatal("expected error result for resize_pane when no server is running")
	}
}

func TestCompatibility_IsNoServer_VersionVariations(t *testing.T) {
	cases := []struct {
		errString string
		want      bool
	}{
		{"error connecting to /tmp/tmux-1000/default (No such file or directory)", true},
		{"no server running on /run/user/1000/tmux-1000/default", true},
		{"failed to connect to server: Connection refused", false},
		{"error connecting to /tmp/user/1000/default (Connection refused)", true},
		{"no current client", true},
		{"can't find session nonexistent", false},
		{"session not found: mysess", false},
		{"duplicate session: mysess", false},
	}

	for _, tc := range cases {
		err := fmt.Errorf("%s", tc.errString)
		if got := isNoServer(err); got != tc.want {
			t.Errorf("isNoServer(%q) = %v, want %v", tc.errString, got, tc.want)
		}
	}
}
