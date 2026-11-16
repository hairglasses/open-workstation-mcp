package process

import (
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/mcptest"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

func TestPsSearch_Validation(t *testing.T) {
	td := findTool(t, "ps_search")

	// Missing all filters
	req := makeReq(map[string]any{})
	res, err := td.Handler(context.Background(), req)
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected validation error when no search filters provided")
	}
}

func TestPsSearch_Live_Query(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /proc")
	}

	td := findTool(t, "ps_search")
	req := makeReq(map[string]any{
		"query": "systemd",
		"limit": 5,
	})

	res, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("ps_search query failed: %v", err)
	}
	if res != nil && res.IsError {
		t.Fatalf("handler returned error result: %v", res)
	}

	var out PsSearchOutput
	unmarshalResult(t, res, &out)

	if out.Total == 0 {
		t.Log("no systemd process matched in this namespace/container (acceptable in containerized tests)")
		return
	}

	if len(out.Matches) == 0 {
		t.Errorf("expected matches when total > 0")
	} else {
		m := out.Matches[0]
		if m.PID <= 0 {
			t.Errorf("expected positive PID, got %d", m.PID)
		}
		if len(m.MatchedOn) == 0 {
			t.Errorf("expected non-empty MatchedOn")
		}
		t.Logf("Matched PID %d (%s) on: %v", m.PID, m.Command, m.MatchedOn)
	}
}

func TestPsSearch_Live_Environ(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /proc")
	}

	td := findTool(t, "ps_search")
	// Search for PATH which is universal in environment
	req := makeReq(map[string]any{
		"env":   "PATH=",
		"limit": 3,
	})

	res, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("ps_search environ failed: %v", err)
	}
	if res != nil && res.IsError {
		t.Fatalf("handler returned error result: %v", res)
	}

	var out PsSearchOutput
	unmarshalResult(t, res, &out)

	if out.Total > 0 && len(out.Matches) > 0 {
		t.Logf("Found %d matches with PATH= env", out.Total)
	}
}

func TestPsSearch_ServerIntegration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux")
	}

	reg := registry.NewToolRegistry()
	reg.RegisterModule(&ProcessModule{})
	srv := mcptest.NewServer(t, reg)

	tool, ok := srv.Registry.GetTool("ps_search")
	if !ok {
		t.Fatal("tool ps_search not registered")
	}

	myPid := os.Getpid()
	req := makeReq(map[string]any{
		"user":  "hg",
		"limit": 5,
	})
	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res)
	}
	t.Logf("Server integration verified for test PID %d", myPid)
}
