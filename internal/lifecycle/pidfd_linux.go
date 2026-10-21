package lifecycle

import (
	"fmt"
	"os"
	"syscall"
)

const (
	// Linux system call numbers on x86_64
	sysPidfdOpen       = 434
	sysPidfdSendSignal = 424

	// PIDFD_NONBLOCK flag for pidfd_open
	pidfdNonblock = 0x800
)

// PIDFD wraps an open Linux pidfd file descriptor.
type PIDFD struct {
	fd uintptr
}

// OpenPIDFD opens a pidfd for the given process ID.
func OpenPIDFD(pid int, nonblock bool) (*PIDFD, error) {
	flags := uint(0)
	if nonblock {
		flags |= pidfdNonblock
	}

	r1, _, errno := syscall.Syscall(sysPidfdOpen, uintptr(pid), uintptr(flags), 0)
	if errno != 0 {
		return nil, fmt.Errorf("pidfd_open(%d): %w", pid, errno)
	}
	return &PIDFD{fd: r1}, nil
}

// FD returns the raw file descriptor.
func (p *PIDFD) FD() uintptr {
	return p.fd
}

// Close closes the pidfd file descriptor.
func (p *PIDFD) Close() error {
	if p.fd == 0 {
		return nil
	}
	fd := p.fd
	p.fd = 0
	return syscall.Close(int(fd))
}

// SendSignal sends a signal to the process referenced by this pidfd.
// This is race-free because the kernel guarantees the signal is only delivered
// to the exact task instance referenced when the pidfd was created, even if
// the PID has since been reused.
func (p *PIDFD) SendSignal(sig syscall.Signal) error {
	if p.fd == 0 {
		return fmt.Errorf("pidfd is closed")
	}

	// sys_pidfd_send_signal(pidfd, sig, info, flags)
	_, _, errno := syscall.Syscall6(
		sysPidfdSendSignal,
		p.fd,
		uintptr(sig),
		0, // siginfo_t* (nil for default info)
		0, // flags (must be 0)
		0,
		0,
	)
	if errno != 0 {
		return fmt.Errorf("pidfd_send_signal(%d, %v): %w", p.fd, sig, errno)
	}
	return nil
}

// SignalProcessSafely validates the process's composite identity (host, boot_id, pid, start_ticks)
// before opening a pidfd and delivering the signal. If the PID has been recycled or the machine
// rebooted, the call fails closed without sending any signal.
func SignalProcessSafely(key ProcessKey, sig syscall.Signal) error {
	if err := key.ValidateCurrent(); err != nil {
		return fmt.Errorf("process validation failed: %w", err)
	}

	pidfd, err := OpenPIDFD(key.PID, false)
	if err != nil {
		// Fallback to validated traditional signal if pidfd_open is unsupported
		return syscall.Kill(key.PID, sig)
	}
	defer func() { _ = pidfd.Close() }()

	// Re-verify after opening the fd to close any minute race window
	currentTicks, err := ReadProcessStartTicks(key.PID)
	if err != nil || currentTicks != key.StartTicks {
		return fmt.Errorf("pid was recycled during fd open: %w", os.ErrNotExist)
	}

	return pidfd.SendSignal(sig)
}
