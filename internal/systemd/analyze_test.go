package systemd

import (
	"context"
	"strings"
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/mcptest"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

type mockAnalyzeRunner struct {
	lastArgs []string
	output   string
	err      error
}

func (m *mockAnalyzeRunner) Run(name string, args ...string) (string, string, error) {
	m.lastArgs = append([]string{name}, args...)
	return m.output, "", m.err
}

func TestAnalyze_Blame_Mock(t *testing.T) {
	mock := &mockAnalyzeRunner{
		output: "26min 23.785s vault-maintenance.service\n16min 12.272s jellyfin-stack.service\n 2.150s dbus.service",
	}
	restore := setCommandRunnerForTest(mock)
	defer restore()

	out, err := analyzeSystemd(AnalyzeInput{
		System:  false,
		Command: "blame",
		Limit:   2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(out.Blame) != 2 {
		t.Fatalf("expected 2 blame entries, got %d", len(out.Blame))
	}
	if out.Blame[0].Unit != "vault-maintenance.service" || out.Blame[0].Duration != "26min 23.785s" {
		t.Errorf("unexpected blame[0]: %+v", out.Blame[0])
	}
	if out.Blame[1].Unit != "jellyfin-stack.service" || out.Blame[1].Duration != "16min 12.272s" {
		t.Errorf("unexpected blame[1]: %+v", out.Blame[1])
	}

	argsJoined := strings.Join(mock.lastArgs, " ")
	if !strings.Contains(argsJoined, "systemd-analyze blame --user") {
		t.Errorf("expected 'systemd-analyze blame --user', got %s", argsJoined)
	}
}

func TestAnalyze_Time_Mock(t *testing.T) {
	mock := &mockAnalyzeRunner{
		output: "Startup finished in 3.276s (kernel) + 55.750s (userspace) = 59.026s",
	}
	restore := setCommandRunnerForTest(mock)
	defer restore()

	out, err := analyzeSystemd(AnalyzeInput{
		System:  true,
		Command: "time",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.StartupTime, "Startup finished in") {
		t.Errorf("expected StartupTime to contain 'Startup finished in', got %q", out.StartupTime)
	}
	argsJoined := strings.Join(mock.lastArgs, " ")
	if !strings.Contains(argsJoined, "systemd-analyze time") || strings.Contains(argsJoined, "--user") {
		t.Errorf("expected system-scope 'systemd-analyze time', got %s", argsJoined)
	}
}

func TestAnalyze_CriticalChain_Mock(t *testing.T) {
	mock := &mockAnalyzeRunner{
		output: "default.target @15.145s\n└─network.target @6.268s",
	}
	restore := setCommandRunnerForTest(mock)
	defer restore()

	out, err := analyzeSystemd(AnalyzeInput{
		System:  false,
		Command: "critical-chain",
		Unit:    "default.target",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.CriticalChain, "default.target") {
		t.Errorf("expected CriticalChain to contain 'default.target', got %q", out.CriticalChain)
	}
	argsJoined := strings.Join(mock.lastArgs, " ")
	if !strings.Contains(argsJoined, "systemd-analyze critical-chain --user default.target") {
		t.Errorf("unexpected args: %s", argsJoined)
	}
}

func TestAnalyze_Validation(t *testing.T) {
	td := findTool(t, "systemd_analyze")

	req := makeReq(map[string]any{
		"command": "verify",
		"unit":    "../evil-path",
	})
	res, err := td.Handler(context.Background(), req)
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected validation error for path traversal unit")
	}
}

func TestAnalyze_MCPServerIntegration(t *testing.T) {
	mock := &mockAnalyzeRunner{
		output: "1.234s custom.service",
	}
	restore := setCommandRunnerForTest(mock)
	defer restore()

	reg := registry.NewToolRegistry()
	reg.RegisterModule(&SystemdModule{})
	srv := mcptest.NewServer(t, reg)

	tool, ok := srv.Registry.GetTool("systemd_analyze")
	if !ok {
		t.Fatal("tool systemd_analyze not registered")
	}

	req := makeReq(map[string]any{
		"command": "blame",
		"limit":   5,
	})
	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res)
	}
}
