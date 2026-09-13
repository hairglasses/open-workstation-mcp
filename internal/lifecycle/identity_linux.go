package lifecycle

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ProcessKey represents the composite identity of a process:
// K_p = (host_id, boot_id, pid, start_ticks).
// This guarantees that stale references across machine reboots or PID recycling
// are deterministically detected and rejected.
type ProcessKey struct {
	HostID     string `json:"host_id"`
	BootID     string `json:"boot_id"`
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
}

// String returns a canonical string representation of the process key.
func (k ProcessKey) String() string {
	return fmt.Sprintf("%s:%s:%d:%d", k.HostID, k.BootID, k.PID, k.StartTicks)
}

// Equals returns true if two process keys are completely identical.
func (k ProcessKey) Equals(other ProcessKey) bool {
	return k.HostID == other.HostID &&
		k.BootID == other.BootID &&
		k.PID == other.PID &&
		k.StartTicks == other.StartTicks
}

// ReadBootID reads the unique Linux kernel boot ID from /proc/sys/kernel/random/boot_id.
func ReadBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("read boot_id: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// ReadProcessStartTicks reads field 22 (starttime) from /proc/<PID>/stat.
func ReadProcessStartTicks(pid int) (uint64, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return 0, fmt.Errorf("read proc stat: %w", err)
	}

	content := string(data)
	// The executable name is in parentheses (e.g. "(bash)") and may contain spaces/parens.
	lastParen := strings.LastIndex(content, ")")
	if lastParen == -1 || lastParen+2 >= len(content) {
		return 0, fmt.Errorf("malformed stat for pid %d", pid)
	}

	// Fields after the closing parenthesis:
	// Field 3 (state) is index 0 in restFields
	// Field 22 (starttime) is index 19 in restFields (22 - 3 = 19)
	rest := strings.Fields(content[lastParen+2:])
	if len(rest) < 20 {
		return 0, fmt.Errorf("insufficient fields in stat for pid %d: got %d", pid, len(rest))
	}

	ticks, err := strconv.ParseUint(rest[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse starttime for pid %d: %w", pid, err)
	}
	return ticks, nil
}

// GetProcessKey constructs a composite ProcessKey for the given host and PID.
func GetProcessKey(hostID string, pid int) (ProcessKey, error) {
	if hostID == "" {
		h, err := os.Hostname()
		if err == nil {
			hostID = h
		} else {
			hostID = "unknown-host"
		}
	}

	bootID, err := ReadBootID()
	if err != nil {
		return ProcessKey{}, err
	}

	ticks, err := ReadProcessStartTicks(pid)
	if err != nil {
		return ProcessKey{}, err
	}

	return ProcessKey{
		HostID:     hostID,
		BootID:     bootID,
		PID:        pid,
		StartTicks: ticks,
	}, nil
}

// ValidateCurrent verifies that the process currently running at k.PID
// matches the recorded BootID and StartTicks. If the process has died,
// the machine rebooted, or the PID was recycled by another process,
// ValidateCurrent returns an error.
func (k ProcessKey) ValidateCurrent() error {
	currentBootID, err := ReadBootID()
	if err != nil {
		return fmt.Errorf("validate boot_id: %w", err)
	}
	if currentBootID != k.BootID {
		return fmt.Errorf("boot_id mismatch: recorded %s, current %s (machine was rebooted)", k.BootID, currentBootID)
	}

	currentTicks, err := ReadProcessStartTicks(k.PID)
	if err != nil {
		return fmt.Errorf("process %d is not running: %w", k.PID, err)
	}

	if currentTicks != k.StartTicks {
		return fmt.Errorf("pid reuse detected: pid %d start ticks mismatch (recorded %d, current %d)",
			k.PID, k.StartTicks, currentTicks)
	}

	return nil
}
