package process

import (
	"context"
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/mcptest"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

func TestMCPServerIntegration_Lifecycle(t *testing.T) {
	m := &ProcessModule{}
	reg := registry.NewToolRegistry()
	reg.RegisterModule(m)
	srv := mcptest.NewServer(t, reg)

	ctx := context.Background()

	// 1. system_info end-to-end through srv
	t.Run("system_info", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("system_info")
		if !ok {
			t.Fatal("tool system_info not found in registry")
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

	// 2. ps_list end-to-end through srv
	t.Run("ps_list", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("ps_list")
		if !ok {
			t.Fatal("tool ps_list not found in registry")
		}
		req := makeReq(map[string]any{"limit": 5})
		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil result")
		}
	})

	// 3. port_list end-to-end through srv
	t.Run("port_list", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("port_list")
		if !ok {
			t.Fatal("tool port_list not found in registry")
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

	// 4. kill_process critical protection through srv
	t.Run("kill_process_critical_without_confirm", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("kill_process")
		if !ok {
			t.Fatal("tool kill_process not found in registry")
		}
		req := makeReq(map[string]any{"pid": 1, "signal": "TERM", "confirm": false})
		res, err := tool.Handler(ctx, req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected signaling PID 1 without confirm to fail")
		}
	})

	// 5. kill_process invalid PID validation through srv
	t.Run("kill_process_invalid_pid", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("kill_process")
		if !ok {
			t.Fatal("tool kill_process not found in registry")
		}
		req := makeReq(map[string]any{"pid": -5})
		res, err := tool.Handler(ctx, req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected negative PID to fail")
		}
	})

	// 6. fd_list end-to-end through srv
	t.Run("fd_list", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("fd_list")
		if !ok {
			t.Fatal("tool fd_list not found in registry")
		}
		req := makeReq(map[string]any{"pid": 1, "limit": 10})
		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil result")
		}
	})

	// 7. investigate_memory end-to-end through srv
	t.Run("investigate_memory", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("investigate_memory")
		if !ok {
			t.Fatal("tool investigate_memory not found in registry")
		}
		req := makeReq(map[string]any{"top_count": 5})
		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil result")
		}
		if res.IsError {
			t.Fatalf("expected non-error result: %v", res)
		}
	})

	// 8. investigate_cpu end-to-end through srv
	t.Run("investigate_cpu", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("investigate_cpu")
		if !ok {
			t.Fatal("tool investigate_cpu not found in registry")
		}
		req := makeReq(map[string]any{"top_count": 5})
		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil result")
		}
		if res.IsError {
			t.Fatalf("expected non-error result: %v", res)
		}
	})

	// 9. ps_search end-to-end through srv
	t.Run("ps_search", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("ps_search")
		if !ok {
			t.Fatal("tool ps_search not found in registry")
		}
		req := makeReq(map[string]any{"query": "process", "limit": 5})
		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil result")
		}
		if res.IsError {
			t.Fatalf("expected non-error result: %v", res)
		}
	})

	t.Run("proc_inspect", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("proc_inspect")
		if !ok {
			t.Fatal("tool proc_inspect not found in registry")
		}
		req := makeReq(map[string]any{"pid": 1})
		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil result")
		}
		if res.IsError {
			t.Fatalf("expected non-error result: %v", res)
		}
	})

	t.Run("liveness_gate", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("liveness_gate")
		if !ok {
			t.Fatal("tool liveness_gate not found in registry")
		}
		req := makeReq(map[string]any{
			"pid":          1,
			"max_rss_mb":   10000.0,
			"max_fd_count": 100000,
		})
		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil result")
		}
		if res.IsError {
			t.Fatalf("expected non-error result: %v", res)
		}
	})

	t.Run("dbus_probe", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("dbus_probe")
		if !ok {
			t.Fatal("tool dbus_probe not found in registry")
		}
		req := makeReq(map[string]any{
			"bus":   "system",
			"probe": "daemon_lifecycle",
		})
		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil result")
		}
	})
}
