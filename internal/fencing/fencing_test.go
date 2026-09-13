package fencing

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGetProcessIdentity_Self(t *testing.T) {
	pid := os.Getpid()
	key, err := GetProcessIdentity(pid)
	if err != nil {
		t.Fatalf("GetProcessIdentity(%d) failed: %v", pid, err)
	}

	if key.PID != pid {
		t.Errorf("expected PID %d, got %d", pid, key.PID)
	}
	if key.Host == "" {
		t.Error("expected non-empty Host")
	}
	if key.BootID == "" {
		t.Error("expected non-empty BootID")
	}
	if key.StartTimeTicks == 0 {
		t.Error("expected non-zero StartTimeTicks")
	}
}

func TestGetProcessIdentity_InvalidPID(t *testing.T) {
	invalidPIDs := []int{0, -1, -99}
	for _, pid := range invalidPIDs {
		key, err := GetProcessIdentity(pid)
		if err == nil {
			t.Errorf("expected error for invalid PID %d, got key: %v", pid, key)
		}
		if !errors.Is(err, ErrInvalidPID) {
			t.Errorf("expected ErrInvalidPID for PID %d, got: %v", pid, err)
		}
	}
}

func TestGetProcessIdentity_NonExistentPID(t *testing.T) {
	// A high PID unlikely to exist
	nonExistentPID := 9999999
	key, err := GetProcessIdentity(nonExistentPID)
	if err == nil {
		t.Fatalf("expected error for non-existent PID %d, got: %v", nonExistentPID, key)
	}
	if !errors.Is(err, ErrProcessNotFound) {
		t.Logf("non-existent PID returned error: %v", err)
	}
}

func TestVerifyProcessIdentity_SelfMatch(t *testing.T) {
	pid := os.Getpid()
	key, err := GetProcessIdentity(pid)
	if err != nil {
		t.Fatalf("GetProcessIdentity failed: %v", err)
	}

	ok, err := VerifyProcessIdentity(key)
	if err != nil {
		t.Fatalf("VerifyProcessIdentity failed on self: %v", err)
	}
	if !ok {
		t.Fatal("expected VerifyProcessIdentity to return true for self")
	}
}

func TestVerifyProcessIdentity_NilKey(t *testing.T) {
	ok, err := VerifyProcessIdentity(nil)
	if ok {
		t.Fatal("expected ok=false for nil key")
	}
	if err == nil {
		t.Fatal("expected error for nil key")
	}
}

func TestVerifyProcessIdentity_BootIDMismatch(t *testing.T) {
	pid := os.Getpid()
	key, err := GetProcessIdentity(pid)
	if err != nil {
		t.Fatalf("GetProcessIdentity failed: %v", err)
	}

	// Tamper with BootID to simulate machine reboot
	tampered := *key
	tampered.BootID = "00000000-dead-beef-0000-000000000000"

	ok, err := VerifyProcessIdentity(&tampered)
	if ok {
		t.Fatal("expected ok=false for mismatched BootID")
	}
	if !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("expected ErrIdentityMismatch, got: %v", err)
	}
}

func TestVerifyProcessIdentity_StartTimeTicksMismatch(t *testing.T) {
	pid := os.Getpid()
	key, err := GetProcessIdentity(pid)
	if err != nil {
		t.Fatalf("GetProcessIdentity failed: %v", err)
	}

	// Tamper with StartTimeTicks to simulate PID recycling
	tampered := *key
	tampered.StartTimeTicks += 9999999

	ok, err := VerifyProcessIdentity(&tampered)
	if ok {
		t.Fatal("expected ok=false for mismatched StartTimeTicks")
	}
	if !errors.Is(err, ErrProcessReused) {
		t.Fatalf("expected ErrProcessReused, got: %v", err)
	}
}

func TestVerifyProcessIdentity_HostMismatch(t *testing.T) {
	pid := os.Getpid()
	key, err := GetProcessIdentity(pid)
	if err != nil {
		t.Fatalf("GetProcessIdentity failed: %v", err)
	}

	tampered := *key
	tampered.Host = "foreign-cluster-node-99.example.com"

	ok, err := VerifyProcessIdentity(&tampered)
	if ok {
		t.Fatal("expected ok=false for mismatched Host")
	}
	if !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("expected ErrIdentityMismatch, got: %v", err)
	}
}

func TestSafeKill_RefusalOnBootMismatch(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	key, err := GetProcessIdentity(pid)
	if err != nil {
		t.Fatalf("failed to get process identity: %v", err)
	}

	// Simulate stale boot ID
	tampered := *key
	tampered.BootID = "mismatched-boot-id"

	err = SafeKill(&tampered, syscall.SIGTERM)
	if err == nil {
		t.Fatal("expected SafeKill to refuse killing process with mismatched boot ID")
	}
	if !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("expected ErrIdentityMismatch, got: %v", err)
	}

	// Invariant: Process must remain alive
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("expected process %d to remain alive, got: %v", pid, err)
	}
}

func TestSafeKill_RefusalOnStartTimeMismatch(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	key, err := GetProcessIdentity(pid)
	if err != nil {
		t.Fatalf("failed to get process identity: %v", err)
	}

	// Simulate PID recycling
	tampered := *key
	tampered.StartTimeTicks += 888888

	err = SafeKill(&tampered, syscall.SIGTERM)
	if err == nil {
		t.Fatal("expected SafeKill to refuse killing process with mismatched start time")
	}
	if !errors.Is(err, ErrProcessReused) {
		t.Fatalf("expected ErrProcessReused, got: %v", err)
	}

	// Invariant: Process must remain alive
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("expected process %d to remain alive, got: %v", pid, err)
	}
}

func TestSafeKill_MatchingSucceeds(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	key, err := GetProcessIdentity(pid)
	if err != nil {
		t.Fatalf("failed to get process identity: %v", err)
	}

	err = SafeKill(key, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("expected SafeKill with matching key to succeed, got: %v", err)
	}

	// Process should terminate
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		// Succeeded
	case <-time.After(2 * time.Second):
		t.Fatalf("process %d did not terminate in time after SafeKill", pid)
	}
}

func TestSafeKill_NilKey(t *testing.T) {
	err := SafeKill(nil, syscall.SIGTERM)
	if err == nil {
		t.Fatal("expected error for nil key in SafeKill")
	}
}

func TestCompositeProcessKey_StringAndEquals(t *testing.T) {
	k1 := &CompositeProcessKey{
		Host:           "node-1",
		PID:            100,
		BootID:         "boot-uuid",
		StartTimeTicks: 12345,
	}
	k2 := &CompositeProcessKey{
		Host:           "node-1",
		PID:            100,
		BootID:         "boot-uuid",
		StartTimeTicks: 12345,
	}
	k3 := &CompositeProcessKey{
		Host:           "node-2",
		PID:            100,
		BootID:         "boot-uuid",
		StartTimeTicks: 12345,
	}

	if !k1.Equals(k2) {
		t.Error("expected k1 to equal k2")
	}
	if k1.Equals(k3) {
		t.Error("expected k1 not to equal k3")
	}

	expectedStr := "node-1:boot-uuid:100:12345"
	if k1.String() != expectedStr {
		t.Errorf("expected string %q, got %q", expectedStr, k1.String())
	}

	var nilKey *CompositeProcessKey
	if nilKey.String() != "<nil>" {
		t.Errorf("expected <nil>, got %s", nilKey.String())
	}
}

func TestProcfsMock_LinuxParsing(t *testing.T) {
	tmpDir := t.TempDir()
	mockBootIDPath := filepath.Join(tmpDir, "sys", "kernel", "random")
	if err := os.MkdirAll(mockBootIDPath, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	expectedBootID := "4a5c6d7e-1234-5678-9abc-def012345678"
	if err := os.WriteFile(filepath.Join(mockBootIDPath, "boot_id"), []byte(expectedBootID+"\n"), 0644); err != nil {
		t.Fatalf("write boot_id failed: %v", err)
	}

	mockPIDDir := filepath.Join(tmpDir, "54321")
	if err := os.MkdirAll(mockPIDDir, 0755); err != nil {
		t.Fatalf("mkdir pid failed: %v", err)
	}
	// /proc/54321/stat mock: field 1 is 54321, field 2 is (my mock comm (special)),
	// followed by fields: state(3)=S, ppid(4)=1, ... starttime(22)=987654321
	statContent := "54321 (my mock comm (special)) S 1 54321 54321 0 -1 4194304 100 0 0 0 10 20 0 0 20 0 1 0 987654321 1234567 1000 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"
	if err := os.WriteFile(filepath.Join(mockPIDDir, "stat"), []byte(statContent), 0644); err != nil {
		t.Fatalf("write stat failed: %v", err)
	}

	SetProcfsRoot(tmpDir)
	defer ResetRoots()

	key, err := GetProcessIdentity(54321)
	if err != nil {
		t.Fatalf("GetProcessIdentity failed on mock procfs: %v", err)
	}
	if key.BootID != expectedBootID {
		t.Errorf("expected BootID %q, got %q", expectedBootID, key.BootID)
	}
	if key.StartTimeTicks != 987654321 {
		t.Errorf("expected StartTimeTicks 987654321, got %d", key.StartTimeTicks)
	}

	// Positive verification
	ok, err := VerifyProcessIdentity(key)
	if err != nil || !ok {
		t.Fatalf("expected positive match on mock procfs, ok=%v, err=%v", ok, err)
	}

	// Negative verification: tamper boot_id
	tamperedBoot := *key
	tamperedBoot.BootID = "different-boot-id"
	ok, err = VerifyProcessIdentity(&tamperedBoot)
	if ok || !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("expected ErrIdentityMismatch, got ok=%v, err=%v", ok, err)
	}

	// Negative verification: tamper start time ticks
	tamperedTicks := *key
	tamperedTicks.StartTimeTicks = 11111111
	ok, err = VerifyProcessIdentity(&tamperedTicks)
	if ok || !errors.Is(err, ErrProcessReused) {
		t.Fatalf("expected ErrProcessReused, got ok=%v, err=%v", ok, err)
	}
}

func TestEnforceMemoryLimit_Validation(t *testing.T) {
	if err := EnforceMemoryLimit(0, 1024); !errors.Is(err, ErrInvalidPID) {
		t.Errorf("expected ErrInvalidPID for PID 0, got: %v", err)
	}
	if err := EnforceMemoryLimit(-1, 1024); !errors.Is(err, ErrInvalidPID) {
		t.Errorf("expected ErrInvalidPID for PID -1, got: %v", err)
	}
	if err := EnforceMemoryLimit(os.Getpid(), 0); err == nil {
		t.Error("expected error for limitBytes=0, got nil")
	}
}

func TestEnforceMemoryLimit_CgroupV2(t *testing.T) {
	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "proc")
	cgroupDir := filepath.Join(tmpDir, "cgroup")

	pid := 8888
	pidDir := filepath.Join(procDir, fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		t.Fatalf("mkdir pid failed: %v", err)
	}

	cgroupSlicePath := "/system.slice/test-worker.service"
	cgroupContent := fmt.Sprintf("0::%s\n", cgroupSlicePath)
	if err := os.WriteFile(filepath.Join(pidDir, "cgroup"), []byte(cgroupContent), 0644); err != nil {
		t.Fatalf("write cgroup failed: %v", err)
	}

	targetCgroupDir := filepath.Join(cgroupDir, cgroupSlicePath)
	if err := os.MkdirAll(targetCgroupDir, 0755); err != nil {
		t.Fatalf("mkdir cgroup target failed: %v", err)
	}

	SetProcfsRoot(procDir)
	SetCgroupV2Root(cgroupDir)
	defer ResetRoots()

	limitBytes := uint64(1073741824) // 1GB
	if err := EnforceMemoryLimit(pid, limitBytes); err != nil {
		t.Fatalf("EnforceMemoryLimit cgroup v2 failed: %v", err)
	}

	writtenBytes, err := os.ReadFile(filepath.Join(targetCgroupDir, "memory.max"))
	if err != nil {
		t.Fatalf("failed to read memory.max: %v", err)
	}
	if strings.TrimSpace(string(writtenBytes)) != "1073741824" {
		t.Errorf("expected memory.max=1073741824, got %s", string(writtenBytes))
	}
}

func TestEnforceMemoryLimit_Rlimit_Self(t *testing.T) {
	limitBytes := uint64(2147483648) // 2GB
	if err := EnforceMemoryLimit(os.Getpid(), limitBytes); err != nil {
		t.Logf("EnforceMemoryLimit self rlimit note: %v", err)
	}
}
