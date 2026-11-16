package systemd

import (
	"context"
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/mcptest"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

func TestMCPServerIntegration_Lifecycle(t *testing.T) {
	m := &SystemdModule{}
	reg := registry.NewToolRegistry()
	reg.RegisterModule(m)
	srv := mcptest.NewServer(t, reg)

	ctx := context.Background()

	// 1. systemd_failed end-to-end through srv
	t.Run("systemd_failed", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("systemd_failed")
		if !ok {
			t.Fatal("tool systemd_failed not found in registry")
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

	// 2. systemd_list_units end-to-end through srv
	t.Run("systemd_list_units", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("systemd_list_units")
		if !ok {
			t.Fatal("tool systemd_list_units not found in registry")
		}
		req := makeReq(map[string]any{"state": "active"})
		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil result")
		}
	})

	// 3. systemd_list_timers end-to-end through srv
	t.Run("systemd_list_timers", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("systemd_list_timers")
		if !ok {
			t.Fatal("tool systemd_list_timers not found in registry")
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

	// 4. systemd_stop safety confirmation gate through srv
	t.Run("systemd_stop_critical_without_confirm", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("systemd_stop")
		if !ok {
			t.Fatal("tool systemd_stop not found in registry")
		}
		req := makeReq(map[string]any{"unit": "sshd.service", "confirm": false})
		res, err := tool.Handler(ctx, req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected stopping sshd without confirm to fail")
		}
	})

	// 5. systemd_status validation failure through srv
	t.Run("systemd_status_invalid_unit", func(t *testing.T) {
		tool, ok := srv.Registry.GetTool("systemd_status")
		if !ok {
			t.Fatal("tool systemd_status not found in registry")
		}
		req := makeReq(map[string]any{"unit": "../bad/unit"})
		res, err := tool.Handler(ctx, req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected path traversal in unit to fail")
		}
	})
}
