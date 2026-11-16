package stdioguard

import (
	"os"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	stdioGuardMu         sync.Mutex
	stdioGuardRefs       int
	stdioGuardSavedFD    = -1
	stdioGuardOrigStdout *os.File
)

func dupCloseOnExec(fd int) (int, error) {
	syscall.ForkLock.RLock()
	newFD, err := unix.Dup(fd)
	if err == nil {
		unix.CloseOnExec(newFD)
	}
	syscall.ForkLock.RUnlock()
	return newFD, err
}

func GuardStdio() (*os.File, func(), error) {
	stdioGuardMu.Lock()
	defer stdioGuardMu.Unlock()

	if stdioGuardRefs == 0 {
		origStdout := os.Stdout
		savedFD, err := dupCloseOnExec(1)
		if err != nil {
			return origStdout, func() {}, err
		}
		if err := unix.Dup2(2, 1); err != nil {
			_ = unix.Close(savedFD)
			return origStdout, func() {}, err
		}
		callerFD, err := dupCloseOnExec(savedFD)
		if err != nil {
			_ = unix.Dup2(savedFD, 1)
			_ = unix.Close(savedFD)
			return origStdout, func() {}, err
		}
		stdioGuardSavedFD = savedFD
		stdioGuardOrigStdout = origStdout
		stdioGuardRefs = 1
		os.Stdout = os.Stderr
		protoOut := os.NewFile(uintptr(callerFD), "|mcp-stdio-out")
		return protoOut, makeStdioGuardRestore(protoOut), nil
	}

	callerFD, err := dupCloseOnExec(stdioGuardSavedFD)
	if err != nil {
		return stdioGuardOrigStdout, func() {}, err
	}
	stdioGuardRefs++
	protoOut := os.NewFile(uintptr(callerFD), "|mcp-stdio-out")
	return protoOut, makeStdioGuardRestore(protoOut), nil
}

func makeStdioGuardRestore(protoOut *os.File) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			stdioGuardMu.Lock()
			defer stdioGuardMu.Unlock()
			_ = protoOut.Close()
			if stdioGuardRefs <= 0 {
				return
			}
			stdioGuardRefs--
			if stdioGuardRefs == 0 {
				_ = unix.Dup2(stdioGuardSavedFD, 1)
				_ = unix.Close(stdioGuardSavedFD)
				stdioGuardSavedFD = -1
				os.Stdout = stdioGuardOrigStdout
				stdioGuardOrigStdout = nil
			}
		})
	}
}
