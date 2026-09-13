//go:build darwin

package fencing

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func getDarwinBootID() (string, error) {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err == nil && tv != nil {
		return fmt.Sprintf("%d.%06d", tv.Sec, tv.Usec), nil
	}

	out, err := exec.Command("sysctl", "-n", "kern.boottime").Output()
	if err != nil {
		return "", fmt.Errorf("read kern.boottime: %w", err)
	}
	s := strings.TrimSpace(string(out))
	if strings.Contains(s, "sec =") {
		parts := strings.Split(s, ",")
		if len(parts) >= 2 {
			secStr := strings.TrimSpace(strings.TrimPrefix(parts[0], "{ sec ="))
			usecPart := strings.Split(parts[1], "}")[0]
			usecStr := strings.TrimSpace(strings.TrimPrefix(usecPart, " usec ="))
			sec, err1 := strconv.ParseInt(secStr, 10, 64)
			usec, err2 := strconv.ParseInt(usecStr, 10, 64)
			if err1 == nil && err2 == nil {
				return fmt.Sprintf("%d.%06d", sec, usec), nil
			}
		}
	}
	return s, nil
}

func getDarwinStartTicks(pid int) (uint64, error) {
	// First verify process exists via signal 0
	if err := syscall.Kill(pid, 0); err != nil {
		if err == syscall.ESRCH {
			return 0, fmt.Errorf("%w: %d", ErrProcessNotFound, pid)
		}
		if err != syscall.EPERM {
			return 0, fmt.Errorf("probe pid %d: %w", pid, err)
		}
	}

	// Fallback mechanism 1: ps -p [pid] -o lstart=
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=")
	out, err := cmd.Output()
	if err == nil {
		line := strings.TrimSpace(string(out))
		if line != "" {
			t, err := time.ParseInLocation(time.ANSIC, line, time.Local)
			if err == nil {
				return uint64(t.Unix()), nil
			}
		}
	}

	// Fallback mechanism 2: sysctl kern.proc.pid
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err == nil && kp != nil && kp.Proc.P_starttime.Sec > 0 {
		return uint64(kp.Proc.P_starttime.Sec), nil
	}

	return 0, fmt.Errorf("%w: cannot determine start time for pid %d", ErrProcessNotFound, pid)
}

func getPlatformProcessIdentity(host string, pid int) (*CompositeProcessKey, error) {
	bootID, err := getDarwinBootID()
	if err != nil {
		return nil, err
	}
	ticks, err := getDarwinStartTicks(pid)
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
	if pid == 0 || pid == os.Getpid() {
		rlim := unix.Rlimit{
			Cur: limitBytes,
			Max: limitBytes,
		}
		var curLim unix.Rlimit
		if err := unix.Getrlimit(unix.RLIMIT_AS, &curLim); err == nil {
			if limitBytes <= curLim.Max {
				rlim.Max = curLim.Max
			}
		}
		if err := unix.Setrlimit(unix.RLIMIT_AS, &rlim); err == nil {
			return nil
		}
		if err := unix.Getrlimit(unix.RLIMIT_DATA, &curLim); err == nil {
			rlim.Cur = limitBytes
			if limitBytes <= curLim.Max {
				rlim.Max = curLim.Max
			}
			if err := unix.Setrlimit(unix.RLIMIT_DATA, &rlim); err == nil {
				return nil
			}
		}
		return nil
	}
	return fmt.Errorf("rlimit enforcement for external pid %d not supported on darwin", pid)
}
