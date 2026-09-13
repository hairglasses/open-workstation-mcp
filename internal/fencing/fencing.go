package fencing

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Sentinel errors for identity verification and fencing.
var (
	ErrIdentityMismatch = errors.New("process identity mismatch")
	ErrProcessReused    = errors.New("process PID reused")
	ErrProcessNotFound  = errors.New("process not found")
	ErrInvalidPID       = errors.New("invalid PID")
)

var (
	mu           sync.RWMutex
	procfsRoot   = "/proc"
	cgroupV2Root = "/sys/fs/cgroup"
)

// SetProcfsRoot sets the root directory for procfs operations (useful for testing or container environments).
func SetProcfsRoot(root string) {
	mu.Lock()
	defer mu.Unlock()
	procfsRoot = root
}

// SetCgroupV2Root sets the root directory for cgroup v2 operations (useful for testing).
func SetCgroupV2Root(root string) {
	mu.Lock()
	defer mu.Unlock()
	cgroupV2Root = root
}

// ResetRoots restores the default procfs and cgroup v2 roots.
func ResetRoots() {
	mu.Lock()
	defer mu.Unlock()
	procfsRoot = "/proc"
	cgroupV2Root = "/sys/fs/cgroup"
}

func getProcfsRoot() string {
	mu.RLock()
	defer mu.RUnlock()
	return procfsRoot
}

func getCgroupV2Root() string {
	mu.RLock()
	defer mu.RUnlock()
	return cgroupV2Root
}

// CompositeProcessKey represents the multi-attribute identity of an operating system process.
// It guards against PID recycling, machine reboots, and cross-host signals.
type CompositeProcessKey struct {
	Host           string `json:"host"`
	PID            int    `json:"pid"`
	BootID         string `json:"boot_id"`
	StartTimeTicks uint64 `json:"start_time_ticks"`
}

// String returns a human-readable representation of the composite process key.
func (k *CompositeProcessKey) String() string {
	if k == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s:%s:%d:%d", k.Host, k.BootID, k.PID, k.StartTimeTicks)
}

// Equals checks if two composite keys represent the exact same process instance.
func (k *CompositeProcessKey) Equals(other *CompositeProcessKey) bool {
	if k == nil || other == nil {
		return k == other
	}
	return k.Host == other.Host &&
		k.PID == other.PID &&
		k.BootID == other.BootID &&
		k.StartTimeTicks == other.StartTimeTicks
}

// hasProcfs checks if procfs is available at the configured procfsRoot.
func hasProcfs() bool {
	bootIDPath := filepath.Join(getProcfsRoot(), "sys", "kernel", "random", "boot_id")
	_, err := os.Stat(bootIDPath)
	return err == nil
}

// readProcBootID reads the unique kernel boot ID from /proc/sys/kernel/random/boot_id.
func readProcBootID() (string, error) {
	path := filepath.Join(getProcfsRoot(), "sys", "kernel", "random", "boot_id")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read boot_id: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// readProcStartTicks reads field 22 (starttime in clock ticks) from /proc/<pid>/stat.
func readProcStartTicks(pid int) (uint64, error) {
	path := filepath.Join(getProcfsRoot(), strconv.Itoa(pid), "stat")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("%w: pid %d", ErrProcessNotFound, pid)
		}
		return 0, fmt.Errorf("read proc stat: %w", err)
	}

	content := strings.TrimSpace(string(data))
	lastParen := strings.LastIndex(content, ")")
	if lastParen == -1 || lastParen+2 >= len(content) {
		return 0, fmt.Errorf("malformed stat for pid %d", pid)
	}

	rest := strings.Fields(content[lastParen+2:])
	// Field 3 is rest[0]; Field 22 (starttime) is rest[19] (22 - 3 = 19)
	if len(rest) < 20 {
		return 0, fmt.Errorf("insufficient fields in stat for pid %d: got %d", pid, len(rest))
	}

	ticks, err := strconv.ParseUint(rest[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse starttime for pid %d: %w", pid, err)
	}
	return ticks, nil
}

// GetProcessIdentity retrieves the composite identity for the specified PID.
// On Linux: reads /proc/sys/kernel/random/boot_id and /proc/[pid]/stat field 22 (starttime).
// On Darwin: falls back to sysctl kern.boottime and ps -p [pid] -o lstart= / clock ticks.
func GetProcessIdentity(pid int) (*CompositeProcessKey, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidPID, pid)
	}

	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}

	// Use procfs if on Linux or if a custom procfsRoot has boot_id
	if runtime.GOOS == "linux" || hasProcfs() {
		bootID, err := readProcBootID()
		if err != nil {
			return nil, err
		}
		ticks, err := readProcStartTicks(pid)
		if err != nil {
			return nil, err
		}
		return &CompositeProcessKey{
			Host:           host,
			PID:            pid,
			BootID:         bootID,
			StartTimeTicks: ticks,
		}, nil
	}

	// Fallback to platform-specific implementation (e.g. Darwin sysctl + ps)
	return getPlatformProcessIdentity(host, pid)
}

// VerifyProcessIdentity validates that the process currently occupying key.PID
// has matching BootID and StartTimeTicks.
// Returns (true, nil) on success.
// Returns (false, ErrIdentityMismatch) on boot ID or host mismatch.
// Returns (false, ErrProcessReused) on start time ticks mismatch.
// Returns (false, ErrProcessNotFound) if the process is no longer running.
func VerifyProcessIdentity(key *CompositeProcessKey) (bool, error) {
	if key == nil {
		return false, errors.New("composite process key is nil")
	}
	if key.PID <= 0 {
		return false, fmt.Errorf("%w: %d", ErrInvalidPID, key.PID)
	}

	current, err := GetProcessIdentity(key.PID)
	if err != nil {
		return false, err
	}

	if key.BootID != "" && current.BootID != key.BootID {
		return false, fmt.Errorf("%w: boot ID mismatch (expected %s, got %s)",
			ErrIdentityMismatch, key.BootID, current.BootID)
	}

	if key.StartTimeTicks != 0 && current.StartTimeTicks != key.StartTimeTicks {
		return false, fmt.Errorf("%w: start time ticks mismatch for pid %d (expected %d, got %d)",
			ErrProcessReused, key.PID, key.StartTimeTicks, current.StartTimeTicks)
	}

	if key.Host != "" && current.Host != "" && key.Host != current.Host {
		return false, fmt.Errorf("%w: host mismatch (expected %s, got %s)",
			ErrIdentityMismatch, key.Host, current.Host)
	}

	return true, nil
}

// SafeKill verifies identity before issuing signal. Returns error ErrIdentityMismatch
// or ErrProcessReused if identity changed, preventing accidental kills across reboots
// or PID recycling.
func SafeKill(key *CompositeProcessKey, sig syscall.Signal) error {
	if key == nil {
		return errors.New("composite process key is nil")
	}

	ok, err := VerifyProcessIdentity(key)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIdentityMismatch
	}

	return platformKill(key.PID, sig)
}

// EnforceMemoryLimit enforces a memory limit on the specified process in bytes.
// It attempts Cgroup v2 memory limit interface (memory.max) first,
// and falls back to rlimit enforcement.
func EnforceMemoryLimit(pid int, limitBytes uint64) error {
	if pid <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidPID, pid)
	}
	if limitBytes == 0 {
		return errors.New("limitBytes must be greater than 0")
	}

	// 1. Try Cgroup v2 memory limit interface
	if err := enforceCgroupV2MemoryLimit(pid, limitBytes); err == nil {
		return nil
	}

	// 2. Rlimit enforcement
	return enforceRlimitPlatform(pid, limitBytes)
}

// enforceCgroupV2MemoryLimit inspects /proc/<pid>/cgroup for cgroup v2 path and writes to memory.max.
func enforceCgroupV2MemoryLimit(pid int, limitBytes uint64) error {
	cgroupFile := filepath.Join(getProcfsRoot(), strconv.Itoa(pid), "cgroup")
	data, err := os.ReadFile(cgroupFile)
	if err != nil {
		return fmt.Errorf("read cgroup: %w", err)
	}

	var cgPath string
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "0::") {
			cgPath = strings.TrimPrefix(line, "0::")
			found = true
			break
		}
	}
	if !found {
		return errors.New("cgroup v2 hierarchy not found")
	}

	memMaxFile := filepath.Join(getCgroupV2Root(), cgPath, "memory.max")
	if _, err := os.Stat(filepath.Dir(memMaxFile)); err != nil {
		return fmt.Errorf("cgroup directory not found: %w", err)
	}

	content := []byte(strconv.FormatUint(limitBytes, 10) + "\n")
	if err := os.WriteFile(memMaxFile, content, 0644); err != nil {
		return fmt.Errorf("write memory.max: %w", err)
	}
	return nil
}
