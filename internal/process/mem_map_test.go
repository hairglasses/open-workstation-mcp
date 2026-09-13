package process

import (
	"context"
	"os"
	"testing"

	"github.com/hairglasses-studio/mcpkit/mcptest"
	"github.com/hairglasses-studio/mcpkit/registry"
)

func TestMemMap_CurrentProcess(t *testing.T) {
	reg := registry.NewToolRegistry()
	reg.RegisterModule(&ProcessModule{})
	srv := mcptest.NewServer(t, reg)

	tool, ok := srv.Registry.GetTool("mem_map")
	if !ok {
		t.Fatal("tool mem_map not found")
	}

	res, err := tool.Handler(context.Background(), makeReq(map[string]any{
		"pid":   os.Getpid(),
		"limit": 10,
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned error: %v", res)
	}

	var out MemMapOutput
	unmarshalResult(t, res, &out)

	if out.PID != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), out.PID)
	}
	if out.TotalRegions == 0 {
		t.Error("expected TotalRegions > 0")
	}
	if len(out.Regions) == 0 || len(out.Regions) > 10 {
		t.Errorf("expected 1-10 regions, got %d", len(out.Regions))
	}
}

func TestMemMap_InvalidPID(t *testing.T) {
	reg := registry.NewToolRegistry()
	reg.RegisterModule(&ProcessModule{})
	srv := mcptest.NewServer(t, reg)

	tool, ok := srv.Registry.GetTool("mem_map")
	if !ok {
		t.Fatal("tool mem_map not found")
	}

	res, err := tool.Handler(context.Background(), makeReq(map[string]any{
		"pid": -1,
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for negative pid")
	}
}

func TestMemMap_NonexistentProcess(t *testing.T) {
	reg := registry.NewToolRegistry()
	reg.RegisterModule(&ProcessModule{})
	srv := mcptest.NewServer(t, reg)

	tool, ok := srv.Registry.GetTool("mem_map")
	if !ok {
		t.Fatal("tool mem_map not found")
	}

	res, err := tool.Handler(context.Background(), makeReq(map[string]any{
		"pid": 999999999,
	}))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for nonexistent pid")
	}
}

func TestSystemInfo_CGroupAndNamespaces(t *testing.T) {
	reg := registry.NewToolRegistry()
	reg.RegisterModule(&ProcessModule{})
	srv := mcptest.NewServer(t, reg)

	tool, ok := srv.Registry.GetTool("system_info")
	if !ok {
		t.Fatal("tool system_info not found")
	}

	res, err := tool.Handler(context.Background(), makeReq(nil))
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned error: %v", res)
	}

	var out SystemInfoOutput
	unmarshalResult(t, res, &out)

	if out.Hostname == "" {
		t.Error("expected non-empty hostname")
	}
	// On Linux /proc/self/cgroup and /proc/self/ns are populated
	if out.CGroup == "" {
		t.Log("warning: cgroup was empty (might be non-cgroup environment)")
	}
	if len(out.Namespaces) == 0 {
		t.Log("warning: namespaces map was empty")
	}
}
