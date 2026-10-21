package process

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Composite Process Identity Tests
// Hardening process lifecycle governance against PID reuse races,
// cross-host false kills, and boot ID mismatches.
// ---------------------------------------------------------------------------

func TestKillProcess_CompositeIdentity_ForeignHostRejected(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux procfs")
	}

	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	td := findTool(t, "kill_process")
	req := makeReq(map[string]any{
		"pid":  pid,
		"host": "foreign-host.internal.example",
	})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		assertContains(t, err.Error(), "PERMISSION")
		assertContains(t, err.Error(), "foreign host")
	} else {
		if result == nil || !result.IsError {
			t.Fatal("expected error result for foreign host mismatch")
		}
		text := extractText(t, result)
		assertContains(t, text, "PERMISSION")
		assertContains(t, text, "foreign host")
	}

	// Invariant: Process must remain alive
	if err := syscall.Kill(pid, 0); err != nil {
		t.Errorf("expected process %d to remain alive, syscall.Kill(0) error: %v", pid, err)
	}
}

func TestKillProcess_CompositeIdentity_BootIDMismatchRejected(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux procfs")
	}

	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	td := findTool(t, "kill_process")
	req := makeReq(map[string]any{
		"pid":     pid,
		"boot_id": "00000000-0000-0000-0000-000000000000",
	})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		assertContains(t, err.Error(), "PERMISSION")
		assertContains(t, err.Error(), "boot ID mismatch")
	} else {
		if result == nil || !result.IsError {
			t.Fatal("expected error result for boot ID mismatch")
		}
		text := extractText(t, result)
		assertContains(t, text, "PERMISSION")
		assertContains(t, text, "boot ID mismatch")
	}

	// Invariant: Process must remain alive
	if err := syscall.Kill(pid, 0); err != nil {
		t.Errorf("expected process %d to remain alive, syscall.Kill(0) error: %v", pid, err)
	}
}

func TestKillProcess_CompositeIdentity_PIDReuseRejected(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux procfs")
	}

	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	actualTicks, err := readProcessStartTicks(pid)
	if err != nil {
		t.Fatalf("failed to read actual start ticks for pid %d: %v", pid, err)
	}

	td := findTool(t, "kill_process")
	req := makeReq(map[string]any{
		"pid":        pid,
		"start_time": actualTicks + 999999,
	})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		assertContains(t, err.Error(), "PERMISSION")
		assertContains(t, err.Error(), "PID reuse")
	} else {
		if result == nil || !result.IsError {
			t.Fatal("expected error result for PID reuse mismatch")
		}
		text := extractText(t, result)
		assertContains(t, text, "PERMISSION")
		assertContains(t, text, "PID reuse")
	}

	// Invariant: Process must remain alive
	if err := syscall.Kill(pid, 0); err != nil {
		t.Errorf("expected process %d to remain alive, syscall.Kill(0) error: %v", pid, err)
	}
}

func TestKillProcess_CompositeIdentity_CommMismatchRejected(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux procfs")
	}

	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	td := findTool(t, "kill_process")
	req := makeReq(map[string]any{
		"pid":           pid,
		"expected_comm": "wrong_comm_name",
	})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		assertContains(t, err.Error(), "PERMISSION")
		assertContains(t, err.Error(), "comm mismatch")
	} else {
		if result == nil || !result.IsError {
			t.Fatal("expected error result for comm mismatch")
		}
		text := extractText(t, result)
		assertContains(t, text, "PERMISSION")
		assertContains(t, text, "comm mismatch")
	}

	// Invariant: Process must remain alive
	if err := syscall.Kill(pid, 0); err != nil {
		t.Errorf("expected process %d to remain alive, syscall.Kill(0) error: %v", pid, err)
	}
}

func TestKillProcess_CompositeIdentity_MatchingSucceeds(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux procfs")
	}

	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("failed to get hostname: %v", err)
	}
	bootID, err := readBootID()
	if err != nil {
		t.Fatalf("failed to read boot ID: %v", err)
	}
	ticks, err := readProcessStartTicks(pid)
	if err != nil {
		t.Fatalf("failed to read start ticks: %v", err)
	}
	comm, err := readProcessComm(pid)
	if err != nil {
		t.Fatalf("failed to read comm: %v", err)
	}

	td := findTool(t, "kill_process")
	req := makeReq(map[string]any{
		"pid":           pid,
		"signal":        "TERM",
		"host":          hostname,
		"boot_id":       bootID,
		"start_time":    ticks,
		"expected_comm": comm,
	})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("expected successful kill with matching composite identity, got error: %v", err)
	}
	if result != nil && result.IsError {
		t.Fatalf("expected successful kill result, got error result: %s", extractText(t, result))
	}

	var killOut KillProcessOutput
	unmarshalResult(t, result, &killOut)
	if killOut.PID != pid {
		t.Errorf("expected PID=%d, got %d", pid, killOut.PID)
	}
	if killOut.Signal != "TERM" {
		t.Errorf("expected Signal=TERM, got %q", killOut.Signal)
	}

	// Invariant: Process must terminate
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait() // killed by signal, so a non-nil error is expected
		close(done)
	}()

	select {
	case <-done:
		// Process exited successfully
	case <-time.After(2 * time.Second):
		t.Errorf("process %d did not terminate within timeout after matching composite identity kill", pid)
	}
}
