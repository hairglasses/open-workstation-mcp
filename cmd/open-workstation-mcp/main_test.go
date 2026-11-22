package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/stdioguard"
	"github.com/hairglasses/open-workstation-mcp/internal/omarchy"
	"github.com/hairglasses/open-workstation-mcp/internal/process"
	"github.com/hairglasses/open-workstation-mcp/internal/systemd"
	"github.com/hairglasses/open-workstation-mcp/internal/tmux"
	"github.com/hairglasses/open-workstation-mcp/internal/workstationmcp"
)

func TestOpenWorkstationMCP44ToolsRegistration(t *testing.T) {
	reg := BuildRegistry()

	count := reg.ToolCount()
	if count != 57 {
		t.Fatalf("expected exactly 57 tools, got %d", count)
	}

	expectedTools := []string{
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

		"omarchy_status",
		"omarchy_commands",
		"omarchy_hw",
		"omarchy_debug",
		"omarchy_config_validate",
		"omarchy_theme",
		"omarchy_font",
		"omarchy_plugin",
		"omarchy_bar",
		"omarchy_hook",
		"omarchy_restart",
		"omarchy_toggle",
		"omarchy_reminder",
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
	omarchy.RegisterResourcesAndPrompts(s)
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

type probeInput struct{}

type probeOutput struct {
	OK bool `json:"ok"`
}

type probeModule struct {
	td registry.ToolDefinition
}

func (m probeModule) Name() string                     { return "probe" }
func (m probeModule) Description() string              { return "probe module" }
func (m probeModule) Tools() []registry.ToolDefinition { return []registry.ToolDefinition{m.td} }

func TestStdioServeHelperProcess(t *testing.T) {
	mode := os.Getenv("OWM_STDIO_GUARD_HELPER")
	switch mode {
	case "":
		return
	case "manifest":
		os.Args = []string{"open-workstation-mcp", "manifest"}
		main()
		os.Exit(0)
	case "real":
		os.Args = []string{"open-workstation-mcp"}
		main()
		os.Exit(0)
	case "stdio":
		_, restoreStdio, _ := stdioguard.GuardStdio()
		defer restoreStdio()
		reg := registry.NewToolRegistry()
		td := handler.TypedHandler[probeInput, probeOutput](
			"fd1_probe",
			"Emit stray stdout bytes and child fd1 bytes during stdio tool execution.",
			func(_ context.Context, _ probeInput) (probeOutput, error) {
				fmt.Println("STRAY_FMT_STDOUT")
				cmd := exec.Command("sh", "-c", "printf 'STRAY_CHILD_FD1\\n'")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				_ = cmd.Run()
				return probeOutput{OK: true}, nil
			},
		)
		reg.RegisterModule(probeModule{td: td})
		s := registry.NewMCPServer("open-workstation-mcp-test", "1.0.0")
		reg.RegisterWithServer(s)
		reg.SetMiddleware([]registry.Middleware{
			registry.AuditMiddleware(""),
			registry.SafetyTierMiddleware(),
		})
		if err := registry.ServeAuto(s); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func TestManifestSubcommandWritesCleanStdout(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^TestStdioServeHelperProcess$")
	cmd.Env = append(os.Environ(), "OWM_STDIO_GUARD_HELPER=manifest")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("manifest helper failed: %v (stderr=%s)", err, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("manifest stdout is not valid JSON: %v\nstdout=%s", err, stdout.String())
	}
}

func TestStdioServeIsolatesFD1Pollution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stdio fd duplication requires linux or darwin")
	}
	stateDir := t.TempDir()
	reqLines := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"probe","version":"1.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"fd1_probe","arguments":{}}}`,
	}, "\n") + "\n"

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^TestStdioServeHelperProcess$")
	cmd.Env = append(os.Environ(),
		"OWM_STDIO_GUARD_HELPER=stdio",
		"MCP_SOCKET_PATH=",
		"XDG_STATE_HOME="+stateDir,
	)
	cmd.Stdin = strings.NewReader(reqLines)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("stdio helper failed: %v\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}

	rawOut := strings.TrimSpace(stdout.String())
	if rawOut == "" {
		t.Fatalf("expected JSON-RPC responses on stdout, got empty (stderr=%s)", stderr.String())
	}
	if strings.Contains(rawOut, "STRAY_FMT_STDOUT") || strings.Contains(rawOut, "STRAY_CHILD_FD1") {
		t.Fatalf("stdout polluted by fd1 writes: %q", rawOut)
	}
	lines := strings.Split(rawOut, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON-RPC responses, got %d: %q", len(lines), rawOut)
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("non-JSON-RPC line on stdout %q: %v", line, err)
		}
	}
	if !strings.Contains(stderr.String(), "STRAY_FMT_STDOUT") || !strings.Contains(stderr.String(), "STRAY_CHILD_FD1") {
		t.Fatalf("expected stray fd1 writes redirected to stderr, got stderr=%q", stderr.String())
	}
}

func TestRealMainStdioToolsCallAndGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("linux process tools test requires linux or darwin")
	}
	stateDir := t.TempDir()
	reqLines := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"e2e","version":"1.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ps_list","arguments":{"limit":3}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"kill_process","arguments":{"pid":999999}}}`,
	}, "\n") + "\n"

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^TestStdioServeHelperProcess$")
	cmd.Env = append(os.Environ(),
		"OWM_STDIO_GUARD_HELPER=real",
		"MCP_SOCKET_PATH=",
		"OTEL_EXPORTER_OTLP_ENDPOINT=",
		"XDG_STATE_HOME="+stateDir,
	)
	cmd.Stdin = strings.NewReader(reqLines)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("real main stdio helper failed: %v\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 JSON-RPC responses, got %d:\nstdout=%s\nstderr=%s", len(lines), stdout.String(), stderr.String())
	}
	byID := make(map[int]string, len(lines))
	for i, line := range lines {
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("frame %d invalid JSON: %v (%q)", i, err, line)
		}
		idVal, ok := frame["id"].(float64)
		if !ok {
			t.Fatalf("frame %d missing numeric id: %q", i, line)
		}
		byID[int(idVal)] = line
	}
	for _, wantID := range []int{1, 2, 3, 4} {
		if _, ok := byID[wantID]; !ok {
			t.Fatalf("missing response for id=%d in %v", wantID, byID)
		}
	}
	if strings.Contains(byID[3], `"isError":true`) {
		t.Fatalf("expected ps_list tools/call to succeed without error, got: %s", byID[3])
	}
	if !strings.Contains(byID[4], "[GATE_DENIED]") {
		t.Fatalf("expected kill_process to be denied by write gate, got: %s", byID[4])
	}

	auditFile := filepath.Join(stateDir, "open-workstation-mcp", "audit.jsonl")
	afi, err := os.Lstat(auditFile)
	if err != nil {
		t.Fatalf("expected audit log at %s: %v", auditFile, err)
	}
	if afi.Mode().Perm() != 0o600 {
		t.Fatalf("expected audit log mode 0600, got %o", afi.Mode().Perm())
	}
}
