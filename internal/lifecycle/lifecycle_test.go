package lifecycle

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestReadBootID(t *testing.T) {
	bootID, err := ReadBootID()
	if err != nil {
		t.Fatalf("ReadBootID failed: %v", err)
	}
	if bootID == "" {
		t.Fatal("expected non-empty bootID")
	}
}

func TestReadProcessStartTicks_Self(t *testing.T) {
	pid := os.Getpid()
	ticks, err := ReadProcessStartTicks(pid)
	if err != nil {
		t.Fatalf("ReadProcessStartTicks failed: %v", err)
	}
	if ticks == 0 {
		t.Fatal("expected non-zero start ticks for current process")
	}
}

func TestGetProcessKey_Self(t *testing.T) {
	pid := os.Getpid()
	key, err := GetProcessKey("archglasses-test", pid)
	if err != nil {
		t.Fatalf("GetProcessKey failed: %v", err)
	}

	if key.PID != pid {
		t.Errorf("expected pid %d, got %d", pid, key.PID)
	}
	if key.HostID != "archglasses-test" {
		t.Errorf("expected host archglasses-test, got %s", key.HostID)
	}

	// Validation against current process must succeed
	if err := key.ValidateCurrent(); err != nil {
		t.Fatalf("ValidateCurrent() failed on self: %v", err)
	}
}

func TestValidateCurrent_BootIDMismatch(t *testing.T) {
	pid := os.Getpid()
	key, err := GetProcessKey("archglasses-test", pid)
	if err != nil {
		t.Fatalf("GetProcessKey failed: %v", err)
	}

	// Artificially change BootID to simulate machine reboot
	key.BootID = "00000000-0000-0000-0000-000000000000"
	err = key.ValidateCurrent()
	if err == nil {
		t.Fatal("expected error for mismatched bootID, got nil")
	}
	if want := "boot_id mismatch"; !strings.Contains(err.Error(), want) {
		t.Errorf("expected error containing %q, got %q", want, err.Error())
	}
}

func TestValidateCurrent_PIDReuse(t *testing.T) {
	pid := os.Getpid()
	key, err := GetProcessKey("archglasses-test", pid)
	if err != nil {
		t.Fatalf("GetProcessKey failed: %v", err)
	}

	// Artificially change StartTicks to simulate PID recycling
	key.StartTicks = key.StartTicks + 999999
	err = key.ValidateCurrent()
	if err == nil {
		t.Fatal("expected error for mismatched start ticks, got nil")
	}
	if want := "pid reuse detected"; !strings.Contains(err.Error(), want) {
		t.Errorf("expected error containing %q, got %q", want, err.Error())
	}
}

func TestOpenPIDFD_Self(t *testing.T) {
	pid := os.Getpid()
	pfd, err := OpenPIDFD(pid, false)
	if err != nil {
		t.Skipf("pidfd_open unsupported on this kernel/container: %v", err)
	}
	defer pfd.Close()

	if pfd.FD() == 0 {
		t.Fatal("expected non-zero pidfd")
	}

	// Signal 0 checks if process exists without killing it
	if err := pfd.SendSignal(syscall.Signal(0)); err != nil {
		t.Errorf("SendSignal(0) failed on self pidfd: %v", err)
	}
}

func TestLeaseRecord_CanAdvance(t *testing.T) {
	rec := LeaseRecord{
		Epoch: 1,
		Fence: 5,
	}

	// Greater epoch advances regardless of fence
	if !rec.CanAdvance(2, 1) {
		t.Error("expected epoch 2 to advance over epoch 1")
	}

	// Same epoch, greater fence advances
	if !rec.CanAdvance(1, 6) {
		t.Error("expected fence 6 to advance over fence 5 in same epoch")
	}

	// Same epoch, same fence rejects
	if rec.CanAdvance(1, 5) {
		t.Error("expected same epoch and fence to be rejected")
	}

	// Same epoch, lesser fence rejects
	if rec.CanAdvance(1, 4) {
		t.Error("expected lesser fence to be rejected")
	}

	// Lesser epoch rejects
	if rec.CanAdvance(0, 10) {
		t.Error("expected lesser epoch to be rejected")
	}
}

func TestProcessLeaseSchema_DDLContainsIndexes(t *testing.T) {
	ddl := ProcessLeaseSchemaDDL
	if !strings.Contains(ddl, "idx_process_lease_epoch_fence") {
		t.Error("expected epoch_fence index in DDL")
	}
	if !strings.Contains(ddl, "UNIQUE (host_id, epoch, fence)") {
		t.Error("expected UNIQUE constraint in DDL")
	}
}
