package process

import (
	"context"
	"os"
	"runtime"
	"testing"
)

func TestFdList_CurrentProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fd_list requires Linux /proc")
	}

	td := findTool(t, "fd_list")
	req := makeReq(map[string]any{"pid": os.Getpid()})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if result != nil && result.IsError {
		t.Fatalf("handler returned error: %v", result)
	}

	var out FdListOutput
	unmarshalResult(t, result, &out)

	if out.PID != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), out.PID)
	}
	if out.Total == 0 {
		t.Errorf("expected at least 1 open file descriptor for current process")
	}
	if len(out.FDs) == 0 {
		t.Errorf("expected at least 1 FD entry in list")
	}

	foundStdinOrOut := false
	for _, entry := range out.FDs {
		if entry.FD == "0" || entry.FD == "1" || entry.FD == "2" {
			foundStdinOrOut = true
			break
		}
	}
	if !foundStdinOrOut {
		t.Errorf("expected standard fd (0, 1, or 2) in fd list")
	}
}

func TestFdList_Limit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fd_list requires Linux /proc")
	}

	td := findTool(t, "fd_list")
	req := makeReq(map[string]any{"pid": os.Getpid(), "limit": 2})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}

	var out FdListOutput
	unmarshalResult(t, result, &out)

	if len(out.FDs) > 2 {
		t.Errorf("expected at most 2 FDs returned, got %d", len(out.FDs))
	}
	if out.Total < len(out.FDs) {
		t.Errorf("expected total (%d) >= returned count (%d)", out.Total, len(out.FDs))
	}
}

func TestFdList_InvalidPid(t *testing.T) {
	td := findTool(t, "fd_list")

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

func TestFdList_NonexistentProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fd_list requires Linux /proc")
	}

	td := findTool(t, "fd_list")
	req := makeReq(map[string]any{"pid": 99999999})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		assertContains(t, err.Error(), "NOT_FOUND")
		return
	}
	if result != nil && result.IsError {
		return
	}
	t.Error("expected NOT_FOUND error for non-existent process")
}
