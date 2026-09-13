//go:build linux

package fencing

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func getPlatformProcessIdentity(host string, pid int) (*CompositeProcessKey, error) {
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

func platformKill(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

func enforceRlimitPlatform(pid int, limitBytes uint64) error {
	rlim := unix.Rlimit{
		Cur: limitBytes,
		Max: limitBytes,
	}
	if err := unix.Prlimit(pid, unix.RLIMIT_AS, &rlim, nil); err == nil {
		return nil
	}
	if pid == 0 || pid == os.Getpid() {
		return unix.Setrlimit(unix.RLIMIT_AS, &rlim)
	}
	return fmt.Errorf("prlimit failed for pid %d", pid)
}
