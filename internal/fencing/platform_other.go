//go:build !darwin && !linux

package fencing

import (
	"fmt"
	"syscall"
)

func getPlatformProcessIdentity(host string, pid int) (*CompositeProcessKey, error) {
	return nil, fmt.Errorf("platform not supported")
}

func platformKill(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

func enforceRlimitPlatform(pid int, limitBytes uint64) error {
	return fmt.Errorf("platform not supported")
}
