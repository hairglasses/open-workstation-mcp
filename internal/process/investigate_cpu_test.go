package process

import (
	"context"
	"runtime"
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/mcptest"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

func TestInvestigateCpu_Live(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /proc")
	}

	td := findTool(t, "investigate_cpu")
	req := makeReq(map[string]any{
		"top_count": 5,
	})

	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("investigate_cpu failed: %v", err)
	}
	if result != nil && result.IsError {
		t.Fatalf("handler returned error result: %v", result)
	}

	var out InvestigateCpuOutput
	unmarshalResult(t, result, &out)

	if out.SystemCPU.NumCPU <= 0 {
		t.Errorf("expected NumCPU > 0, got %d", out.SystemCPU.NumCPU)
	}
	if out.SystemCPU.SaturationRisk == "" {
		t.Errorf("expected non-empty SaturationRisk")
	}
	if len(out.TopProcesses) == 0 {
		t.Errorf("expected at least 1 top process, got 0")
	} else {
		top := out.TopProcesses[0]
		if top.PID <= 0 {
			t.Errorf("expected positive PID, got %d", top.PID)
		}
		if top.Threads < 1 {
			t.Errorf("expected threads >= 1, got %d", top.Threads)
		}
	}
	if out.Diagnosis == "" {
		t.Errorf("expected non-empty Diagnosis")
	}
	t.Logf("Diagnosis: %s", out.Diagnosis)
}

func TestInvestigateCpu_ServerIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux")
	}

	reg := registry.NewToolRegistry()
	reg.RegisterModule(&ProcessModule{})
	srv := mcptest.NewServer(t, reg)

	tool, ok := srv.Registry.GetTool("investigate_cpu")
	if !ok {
		t.Fatal("tool investigate_cpu not registered")
	}

	req := makeReq(map[string]any{
		"top_count":     3,
		"threshold_cpu": 0.0,
	})
	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned tool error: %v", res)
	}
}

func TestInvestigateCpu_TopCountClamping(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux")
	}

	td := findTool(t, "investigate_cpu")

	// Limit to 2
	req := makeReq(map[string]any{"top_count": 2})
	res, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out InvestigateCpuOutput
	unmarshalResult(t, res, &out)
	if len(out.TopProcesses) > 2 {
		t.Errorf("expected at most 2 processes, got %d", len(out.TopProcesses))
	}

	// Default when top_count <= 0
	reqDefault := makeReq(map[string]any{"top_count": 0})
	resDefault, err := td.Handler(context.Background(), reqDefault)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var outDefault InvestigateCpuOutput
	unmarshalResult(t, resDefault, &outDefault)
	if len(outDefault.TopProcesses) > 10 {
		t.Errorf("expected at most 10 processes for default top_count, got %d", len(outDefault.TopProcesses))
	}
}

func TestInvestigateCpu_ThresholdFilter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux")
	}

	td := findTool(t, "investigate_cpu")
	// An impossibly high threshold (99999% CPU) should yield 0 processes
	req := makeReq(map[string]any{"threshold_cpu": 99999.0})
	res, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out InvestigateCpuOutput
	unmarshalResult(t, res, &out)
	if len(out.TopProcesses) != 0 {
		t.Errorf("expected 0 processes for 99999%% threshold, got %d", len(out.TopProcesses))
	}
}
