package main

import (
	"testing"

	"github.com/hairglasses-studio/mcpkit/registry"
	"github.com/hairglasses/open-workstation-mcp/internal/process"
	"github.com/hairglasses/open-workstation-mcp/internal/systemd"
	"github.com/hairglasses/open-workstation-mcp/internal/tmux"
	"github.com/hairglasses/open-workstation-mcp/internal/workstationmcp"
)

func TestOpenWorkstationMCP44ToolsRegistration(t *testing.T) {
	reg := BuildRegistry()

	count := reg.ToolCount()
	if count != 44 {
		t.Fatalf("expected exactly 44 tools, got %d", count)
	}

	expectedTools := []string{
		// 13 systemd tools
		"systemd_status",
		"systemd_start",
		"systemd_stop",
		"systemd_restart",
		"systemd_enable",
		"systemd_disable",
		"systemd_logs",
		"systemd_list_units",
		"systemd_list_timers",
		"systemd_failed",
		"systemd_investigate_unit",
		"systemd_create_timer",
		"systemd_analyze",

		// 14 tmux tools
		"tmux_list_sessions",
		"tmux_list_windows",
		"tmux_list_panes",
		"tmux_capture_pane",
		"tmux_send_keys",
		"tmux_new_session",
		"tmux_new_window",
		"tmux_kill_session",
		"tmux_wait_for_text",
		"tmux_search_panes",
		"tmux_workspace",
		"tmux_resize_pane",
		"tmux_snapshot",
		"tmux_template",

		// 17 process tools
		"ps_list",
		"ps_tree",
		"kill_process",
		"port_list",
		"gpu_status",
		"system_info",
		"investigate_port",
		"investigate_service",
		"fd_list",
		"mem_map",
		"net_connections",
		"investigate_memory",
		"investigate_cpu",
		"ps_search",
		"proc_inspect",
		"liveness_gate",
		"dbus_probe",
	}

	registered := make(map[string]bool)
	for _, td := range reg.GetAllToolDefinitions() {
		registered[td.Tool.Name] = true
	}

	for _, toolName := range expectedTools {
		if !registered[toolName] {
			t.Errorf("missing expected tool: %s", toolName)
		}
	}

	s := registry.NewMCPServer("open-workstation-mcp", "1.0.0")
	reg.RegisterWithServer(s)

	systemd.RegisterResourcesAndPrompts(s)
	tmux.RegisterResourcesAndPrompts(s)
	process.RegisterResourcesAndPrompts(s)
}

func TestRunManifest(t *testing.T) {
	if err := printJSON(workstationmcp.SampleGateway().Manifest()); err != nil {
		t.Fatalf("printJSON failed: %v", err)
	}
}

func TestRunCallLegacy(t *testing.T) {
	if err := call([]string{"workstation_status", "--param", "profile=desktop"}); err != nil {
		t.Fatalf("call() error = %v", err)
	}
}
