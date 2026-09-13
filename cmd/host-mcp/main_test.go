package main

import (
	"testing"

	"github.com/hairglasses-studio/mcpkit/registry"
	"github.com/hairglasses/open-workstation-mcp/internal/process"
	"github.com/hairglasses/open-workstation-mcp/internal/systemd"
	"github.com/hairglasses/open-workstation-mcp/internal/tmux"
)

func TestHostMCP44ToolsRegistration(t *testing.T) {
	reg := registry.NewToolRegistry(registry.Config{})

	systemdMod := &systemd.SystemdModule{}
	tmuxMod := &tmux.TmuxModule{}
	processMod := &process.ProcessModule{}

	reg.RegisterModule(systemdMod)
	reg.RegisterModule(tmuxMod)
	reg.RegisterModule(processMod)

	count := reg.ToolCount()
	if count != 44 {
		t.Fatalf("expected exactly 44 tools, got %d", count)
	}

	s := registry.NewMCPServer("host-mcp-test", "1.0.0")
	reg.RegisterWithServer(s)
	systemd.RegisterResourcesAndPrompts(s)
	tmux.RegisterResourcesAndPrompts(s)
	process.RegisterResourcesAndPrompts(s)
}
