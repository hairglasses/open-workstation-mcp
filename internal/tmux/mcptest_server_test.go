package tmux

import (
	"context"
	"testing"

	"github.com/hairglasses-studio/mcpkit/mcptest"
	"github.com/hairglasses-studio/mcpkit/registry"
)

func TestMCPServerIntegration_Lifecycle(t *testing.T) {
	m := &TmuxModule{}
	reg := registry.NewToolRegistry()
	reg.RegisterModule(m)
	srv := mcptest.NewServer(t, reg)

	ctx := context.Background()

	// 1. tmux_list_sessions end-to-end through srv
	t.Run("tmux_list_sessions", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("tmux_list_sessions")
		if !ok {
			t.Fatal("tool tmux_list_sessions not found in registry")
		}
		req := makeReq(nil)
		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil result")
		}
	})

	// 2. tmux_new_session validation failure (empty name)
	t.Run("tmux_new_session_empty_name", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("tmux_new_session")
		if !ok {
			t.Fatal("tool tmux_new_session not found in registry")
		}
		req := makeReq(map[string]any{"name": ""})
		res, err := tool.Handler(ctx, req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected empty session name to fail validation")
		}
	})

	// 3. tmux_kill_session validation failure (empty name)
	t.Run("tmux_kill_session_empty_name", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("tmux_kill_session")
		if !ok {
			t.Fatal("tool tmux_kill_session not found in registry")
		}
		req := makeReq(map[string]any{"name": ""})
		res, err := tool.Handler(ctx, req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected empty kill session name to fail validation")
		}
	})

	// 4. tmux_workspace empty session validation failure
	t.Run("tmux_workspace_empty_session", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("tmux_workspace")
		if !ok {
			t.Fatal("tool tmux_workspace not found in registry")
		}
		req := makeReq(map[string]any{
			"session": "",
			"windows": []any{
				map[string]any{"name": "win1"},
			},
		})
		res, err := tool.Handler(ctx, req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected empty workspace session name to fail")
		}
	})

	// 5. tmux_workspace missing windows validation failure
	t.Run("tmux_workspace_no_windows", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("tmux_workspace")
		if !ok {
			t.Fatal("tool tmux_workspace not found in registry")
		}
		req := makeReq(map[string]any{
			"session": "test-session",
			"windows": []any{},
		})
		res, err := tool.Handler(ctx, req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected empty workspace windows list to fail")
		}
	})

	// 6. tmux_capture_pane non-existent session
	t.Run("tmux_capture_pane_bad_session", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("tmux_capture_pane")
		if !ok {
			t.Fatal("tool tmux_capture_pane not found in registry")
		}
		req := makeReq(map[string]any{
			"session": "definitely-nonexistent-session-xyz-12345",
			"lines":   10,
		})
		res, err := tool.Handler(ctx, req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected capture on nonexistent session to return error")
		}
	})

	// 7. tmux_resize_pane end-to-end through srv
	t.Run("tmux_resize_pane_validation_and_error", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("tmux_resize_pane")
		if !ok {
			t.Fatal("tool tmux_resize_pane not found in registry")
		}

		// Empty session name fails validation
		reqEmpty := makeReq(map[string]any{
			"session":   "",
			"direction": "U",
		})
		resEmpty, err := tool.Handler(ctx, reqEmpty)
		if err == nil && (resEmpty == nil || !resEmpty.IsError) {
			t.Fatal("expected empty session to return error")
		}

		// No resize parameters specified fails validation
		reqNoParams := makeReq(map[string]any{
			"session": "test-session",
		})
		resNoParams, err := tool.Handler(ctx, reqNoParams)
		if err == nil && (resNoParams == nil || !resNoParams.IsError) {
			t.Fatal("expected missing resize parameters to return error")
		}

		// Bad session fails with formatted error
		reqBadSession := makeReq(map[string]any{
			"session": "nonexistent-session-for-resize-99999",
			"zoom":    true,
		})
		resBad, err := tool.Handler(ctx, reqBadSession)
		if err == nil && (resBad == nil || !resBad.IsError) {
			t.Fatal("expected nonexistent session resize to return error")
		}
	})

	// 8. tmux_snapshot validation & non-existent session handling
	t.Run("tmux_snapshot_validation_and_error", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("tmux_snapshot")
		if !ok {
			t.Fatal("tool tmux_snapshot not found in registry")
		}

		// Empty session name fails validation
		reqEmpty := makeReq(map[string]any{
			"session": "",
		})
		resEmpty, err := tool.Handler(ctx, reqEmpty)
		if err == nil && (resEmpty == nil || !resEmpty.IsError) {
			t.Fatal("expected empty session to return error")
		}

		// Non-existent session returns formatted error
		reqBad := makeReq(map[string]any{
			"session": "nonexistent-session-for-snapshot-99999",
		})
		resBad, err := tool.Handler(ctx, reqBad)
		if err == nil && (resBad == nil || !resBad.IsError) {
			t.Fatal("expected nonexistent session snapshot to return error")
		}
	})

	// 9. tmux_template validation and execution through srv
	t.Run("tmux_template_lifecycle_through_srv", func(t *testing.T) {
		t.Setenv("TMUX_MCP_TEMPLATE_DIR", t.TempDir())

		tool, ok := srv.Registry.GetTool("tmux_template")
		if !ok {
			t.Fatal("tool tmux_template not found in registry")
		}

		// Empty action returns error
		resEmpty, err := tool.Handler(ctx, makeReq(map[string]any{"action": ""}))
		if err == nil && (resEmpty == nil || !resEmpty.IsError) {
			t.Fatal("expected empty action to return error")
		}

		// Save template
		saveReq := makeReq(map[string]any{
			"action": "save",
			"name":   "srv-tpl",
			"windows": []map[string]any{
				{"name": "code", "panes": []map[string]any{{"command": "echo 1"}}},
			},
		})
		saveRes, err := tool.Handler(ctx, saveReq)
		if err != nil {
			t.Fatalf("unexpected save error: %v", err)
		}
		if saveRes == nil || saveRes.IsError {
			t.Fatalf("expected non-error save result: %v", saveRes)
		}

		// List templates
		listRes, err := tool.Handler(ctx, makeReq(map[string]any{"action": "list"}))
		if err != nil {
			t.Fatalf("unexpected list error: %v", err)
		}
		if listRes == nil || listRes.IsError {
			t.Fatalf("expected non-error list result: %v", listRes)
		}

		// Load template
		loadRes, err := tool.Handler(ctx, makeReq(map[string]any{"action": "load", "name": "srv-tpl"}))
		if err != nil {
			t.Fatalf("unexpected load error: %v", err)
		}
		if loadRes == nil || loadRes.IsError {
			t.Fatalf("expected non-error load result: %v", loadRes)
		}
	})
}
