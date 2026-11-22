package tmux

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
)

func TestCapture_LineCounts(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-edge-cap-%d", rand.Intn(100000))
	newTd := findTool(t, "tmux_new_session")
	_, err := newTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	if err != nil {
		t.Skipf("could not create session: %v", err)
	}
	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	td := findTool(t, "tmux_capture_pane")

	tests := []struct {
		name  string
		lines int
	}{
		{"zero_defaults_to_50", 0},
		{"one_line", 1},
		{"large_count", 5000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := makeReq(map[string]any{"session": sessName, "lines": tc.lines})
			result, err := td.Handler(context.Background(), req)
			if err != nil {
				t.Fatalf("capture error: %v", err)
			}
			var out CaptureOutput
			unmarshalResult(t, result, &out)
			if out.Target == "" {
				t.Error("target is empty")
			}
		})
	}
}

func TestSendKeys_SpecialChars(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-edge-keys-%d", rand.Intn(100000))
	newTd := findTool(t, "tmux_new_session")
	_, err := newTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	if err != nil {
		t.Skipf("could not create session: %v", err)
	}
	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	td := findTool(t, "tmux_send_keys")
	waitTd := findTool(t, "tmux_wait_for_text")

	if _, err := td.Handler(context.Background(), makeReq(map[string]any{
		"session": sessName,
		"keys":    "printf 'READY_%s\\n' 'SHELL'",
	})); err != nil {
		t.Fatalf("send_keys readiness command error: %v", err)
	}
	if _, err := td.Handler(context.Background(), makeReq(map[string]any{
		"session": sessName,
		"keys":    "Enter",
	})); err != nil {
		t.Fatalf("send_keys readiness Enter error: %v", err)
	}
	waitRes, err := waitTd.Handler(context.Background(), makeReq(map[string]any{
		"session":      sessName,
		"text":         "READY_SHELL",
		"timeout_secs": 5,
	}))
	if err != nil {
		t.Fatalf("wait_for_text readiness error: %v", err)
	}
	if waitRes != nil && waitRes.IsError {
		t.Fatalf("wait_for_text readiness failed: %s", extractText(t, waitRes))
	}

	tests := []struct {
		name string
		keys string
	}{
		{"enter", "Enter"},
		{"escape", "Escape"},
		{"ctrl_c", "C-c"},
		{"tab", "Tab"},
		{"literal_text", "echo test"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := makeReq(map[string]any{"session": sessName, "keys": tc.keys})
			result, err := td.Handler(context.Background(), req)
			if err != nil {
				t.Fatalf("send_keys error: %v", err)
			}
			var out SendKeysOutput
			unmarshalResult(t, result, &out)
			if out.Sent != tc.keys {
				t.Errorf("sent = %q, want %q", out.Sent, tc.keys)
			}
		})
	}
}

func TestWorkspace_ValidLayouts(t *testing.T) {
	requireTmux(t)

	layouts := []string{
		"tiled",
		"even-horizontal",
		"even-vertical",
		"main-horizontal",
		"main-vertical",
	}

	for _, layout := range layouts {
		t.Run(layout, func(t *testing.T) {
			sessName := fmt.Sprintf("test-layout-%d", rand.Intn(100000))
			td := findTool(t, "tmux_workspace")

			req := makeReq(map[string]any{
				"session": sessName,
				"windows": []any{
					map[string]any{
						"name":   "win",
						"layout": layout,
						"panes": []any{
							map[string]any{"command": "true"},
							map[string]any{"command": "true"},
						},
					},
				},
			})

			result, err := td.Handler(context.Background(), req)
			if err != nil {
				t.Fatalf("workspace error: %v", err)
			}

			defer func() {
				killTd := findTool(t, "tmux_kill_session")
				_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
			}()

			var out WorkspaceOutput
			unmarshalResult(t, result, &out)
			if out.PaneCount != 2 {
				t.Errorf("expected 2 panes, got %d", out.PaneCount)
			}
		})
	}
}

func TestNewSession_WithDirAndCommand(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-dir-cmd-%d", rand.Intn(100000))
	td := findTool(t, "tmux_new_session")
	req := makeReq(map[string]any{
		"name":      sessName,
		"directory": "/tmp",
		"command":   "echo hello",
	})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("new_session error: %v", err)
	}
	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	var out NewSessionOutput
	unmarshalResult(t, result, &out)
	if !out.Created {
		t.Error("expected created=true")
	}
	if out.Session != sessName {
		t.Errorf("session = %q, want %q", out.Session, sessName)
	}
}

func TestNewWindow_WithNameAndCommand(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-nw-%d", rand.Intn(100000))
	newTd := findTool(t, "tmux_new_session")
	_, err := newTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	if err != nil {
		t.Skipf("could not create session: %v", err)
	}
	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	td := findTool(t, "tmux_new_window")
	req := makeReq(map[string]any{
		"session": sessName,
		"name":    "my-win",
		"command": "echo window-test",
	})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("new_window error: %v", err)
	}
	var out NewWindowOutput
	unmarshalResult(t, result, &out)
	if out.Session != sessName {
		t.Errorf("session = %q, want %q", out.Session, sessName)
	}
}
