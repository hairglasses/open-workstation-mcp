package systemd

import (
	"context"
	"strings"
	"testing"

	"github.com/hairglasses-studio/mcpkit/mcptest"
	"github.com/hairglasses-studio/mcpkit/registry"
)

type mockTimerRunner struct {
	lastArgs []string
	output   string
	err      error
}

func (m *mockTimerRunner) Run(name string, args ...string) (string, string, error) {
	m.lastArgs = append([]string{name}, args...)
	return m.output, "", m.err
}

func TestCreateTimer_Validation(t *testing.T) {
	td := findTool(t, "systemd_create_timer")

	// 1. Missing command
	req1 := makeReq(map[string]any{
		"on_calendar": "daily",
		"command":     "",
	})
	res1, err1 := td.Handler(context.Background(), req1)
	if err1 == nil && (res1 == nil || !res1.IsError) {
		t.Fatal("expected error for empty command")
	}

	// 2. Missing all schedules
	req2 := makeReq(map[string]any{
		"command": "echo test",
	})
	res2, err2 := td.Handler(context.Background(), req2)
	if err2 == nil && (res2 == nil || !res2.IsError) {
		t.Fatal("expected error for missing schedules")
	}

	// 3. Path traversal in unit name
	req3 := makeReq(map[string]any{
		"unit":        "../bad-unit",
		"on_calendar": "hourly",
		"command":     "echo test",
	})
	res3, err3 := td.Handler(context.Background(), req3)
	if err3 == nil && (res3 == nil || !res3.IsError) {
		t.Fatal("expected error for path traversal in unit")
	}
}

func TestCreateTimer_MockSuccess(t *testing.T) {
	mock := &mockTimerRunner{
		output: "Running timer as unit: my-job.timer\nWill run service as unit: my-job.service",
	}
	restore := setCommandRunnerForTest(mock)
	defer restore()

	out, err := createTimer(CreateTimerInput{
		System:      false,
		Unit:        "my-job",
		Description: "Nightly sync job",
		OnCalendar:  "*-*-* 03:00:00",
		Command:     "/usr/bin/sync-data --fast",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !out.Created {
		t.Error("expected Created to be true")
	}
	if out.TimerUnit != "my-job.timer" {
		t.Errorf("expected TimerUnit 'my-job.timer', got %q", out.TimerUnit)
	}
	if out.ServiceUnit != "my-job.service" {
		t.Errorf("expected ServiceUnit 'my-job.service', got %q", out.ServiceUnit)
	}

	argsJoined := strings.Join(mock.lastArgs, " ")
	for _, expected := range []string{
		"systemd-run", "--user", "--unit=my-job",
		"--description=Nightly sync job",
		"--on-calendar=*-*-* 03:00:00",
		"/bin/sh -c /usr/bin/sync-data --fast",
	} {
		if !strings.Contains(argsJoined, expected) {
			t.Errorf("missing expected arg %q in command: %s", expected, argsJoined)
		}
	}
}

func TestCreateTimer_MCPServerIntegration(t *testing.T) {
	mock := &mockTimerRunner{
		output: "Running timer as unit: run-r12345.timer\nWill run service as unit: run-r12345.service",
	}
	restore := setCommandRunnerForTest(mock)
	defer restore()

	reg := registry.NewToolRegistry()
	reg.RegisterModule(&SystemdModule{})
	srv := mcptest.NewServer(t, reg)

	tool, ok := srv.Registry.GetTool("systemd_create_timer")
	if !ok {
		t.Fatal("tool systemd_create_timer not registered")
	}

	req := makeReq(map[string]any{
		"on_active_sec": "30s",
		"command":       "echo delayed-action",
	})
	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned tool error: %v", res)
	}
}
