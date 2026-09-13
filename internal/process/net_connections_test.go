package process

import (
	"context"
	"os"
	"runtime"
	"testing"
)

func TestNetConnections_CurrentProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("net_connections requires Linux")
	}

	td := findTool(t, "net_connections")
	pid := os.Getpid()
	req := makeReq(map[string]any{"pid": pid})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("net_connections for PID %d failed: %v", pid, err)
	}
	if result != nil && result.IsError {
		t.Fatalf("handler returned error: %v", result)
	}
	var out NetConnectionsOutput
	unmarshalResult(t, result, &out)
	if out.PID != pid {
		t.Errorf("PID = %d, want %d", out.PID, pid)
	}
	t.Logf("found %d connections for test PID %d", out.Total, pid)
}

func TestNetConnections_InvalidPID(t *testing.T) {
	td := findTool(t, "net_connections")
	for _, invalidPid := range []int{0, -1, -999} {
		req := makeReq(map[string]any{"pid": invalidPid})
		result, err := td.Handler(context.Background(), req)
		if err != nil {
			assertContains(t, err.Error(), "INVALID_PARAM")
			continue
		}
		if result != nil && result.IsError {
			continue
		}
		t.Errorf("expected error for invalid PID %d", invalidPid)
	}
}

func TestNetConnections_NonexistentPID(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux")
	}
	td := findTool(t, "net_connections")
	req := makeReq(map[string]any{"pid": 99999999})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		assertContains(t, err.Error(), "NOT_FOUND")
		return
	}
	if result != nil && result.IsError {
		return
	}
	t.Error("expected error for nonexistent process")
}

func TestNetConnections_ProtoFilter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux")
	}
	td := findTool(t, "net_connections")
	pid := os.Getpid()
	req := makeReq(map[string]any{"pid": pid, "proto": "tcp", "limit": 10})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("net_connections tcp failed: %v", err)
	}
	var out NetConnectionsOutput
	unmarshalResult(t, result, &out)
	for _, c := range out.Connections {
		if c.Proto != "tcp" {
			t.Errorf("expected proto tcp, got %s", c.Proto)
		}
	}
}
