package process

import (
	"context"
	"runtime"
	"testing"
)

func TestInvestigateMemory_Live(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("investigate_memory requires Linux")
	}

	td := findTool(t, "investigate_memory")
	req := makeReq(map[string]any{"top_count": 5})
	res, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if res != nil && res.IsError {
		t.Fatalf("handler returned error: %v", res)
	}

	var out InvestigateMemoryOutput
	unmarshalResult(t, res, &out)

	if out.SystemMemory.TotalMB <= 0 {
		t.Errorf("expected TotalMB > 0, got %d", out.SystemMemory.TotalMB)
	}
	if out.SystemMemory.AvailableMB <= 0 {
		t.Errorf("expected AvailableMB > 0, got %d", out.SystemMemory.AvailableMB)
	}
	if out.ProcessCount == 0 || len(out.TopProcesses) == 0 {
		t.Errorf("expected at least 1 top process, got %d", out.ProcessCount)
	}
	if out.OOMRisk.RiskLevel == "" {
		t.Error("expected non-empty OOM risk level")
	}
	if out.OOMRisk.Summary == "" {
		t.Error("expected non-empty OOM risk summary")
	}

	// Verify top processes are sorted descending by RSS
	for i := 1; i < len(out.TopProcesses); i++ {
		if out.TopProcesses[i].RssMB > out.TopProcesses[i-1].RssMB {
			t.Errorf("top processes not sorted by RSS: [%d]=%f > [%d]=%f",
				i, out.TopProcesses[i].RssMB, i-1, out.TopProcesses[i-1].RssMB)
		}
	}
}

func TestInvestigateMemory_TopCountClamping(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux")
	}

	td := findTool(t, "investigate_memory")

	// Limit to 2
	req := makeReq(map[string]any{"top_count": 2})
	res, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out InvestigateMemoryOutput
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
	var outDefault InvestigateMemoryOutput
	unmarshalResult(t, resDefault, &outDefault)
	if len(outDefault.TopProcesses) > 10 {
		t.Errorf("expected at most 10 processes for default top_count, got %d", len(outDefault.TopProcesses))
	}
}

func TestInvestigateMemory_ThresholdFilter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux")
	}

	td := findTool(t, "investigate_memory")
	// An absurdly high threshold (1,000,000 MB = 1 TB) should yield 0 processes
	req := makeReq(map[string]any{"threshold_mb": 1000000})
	res, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out InvestigateMemoryOutput
	unmarshalResult(t, res, &out)
	if len(out.TopProcesses) != 0 {
		t.Errorf("expected 0 processes for 1TB threshold, got %d", len(out.TopProcesses))
	}
}
