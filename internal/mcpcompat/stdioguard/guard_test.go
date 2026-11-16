package stdioguard

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestGuardStdioHelperProcess(t *testing.T) {
	mode := os.Getenv("OWM_STDIOGUARD_HELPER")
	if mode == "" {
		return
	}
	outerOut, outerRestore, err := GuardStdio()
	if err != nil {
		os.Exit(2)
	}
	innerOut, innerRestore, err := GuardStdio()
	if err != nil {
		os.Exit(3)
	}
	fmt.Println("STRAY_STDOUT_LINE")
	child := exec.Command("sh", "-c", "printf 'CHILD_FD1_LINE\\n'")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	_ = child.Run()
	_, _ = outerOut.WriteString("PROTO_OUTER\n")
	_, _ = innerOut.WriteString("PROTO_INNER\n")
	innerRestore()
	innerRestore()
	outerRestore()
	outerRestore()
	fmt.Println("AFTER_RESTORE_STDOUT")
	os.Exit(0)
}

func TestGuardStdioIsolatesAndRestores(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stdio fd duplication requires linux or darwin")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^TestGuardStdioHelperProcess$")
	cmd.Env = append(os.Environ(), "OWM_STDIOGUARD_HELPER=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("helper failed: %v stderr=%q stdout=%q", err, stderr.String(), stdout.String())
	}
	gotOut := stdout.String()
	gotErr := stderr.String()
	if strings.Contains(gotOut, "STRAY_STDOUT_LINE") || strings.Contains(gotOut, "CHILD_FD1_LINE") {
		t.Fatalf("stdout received stray fd1 writes: %q", gotOut)
	}
	for _, want := range []string{"PROTO_OUTER\n", "PROTO_INNER\n", "AFTER_RESTORE_STDOUT\n"} {
		if !strings.Contains(gotOut, want) {
			t.Fatalf("stdout %q missing %q", gotOut, want)
		}
	}
	for _, want := range []string{"STRAY_STDOUT_LINE", "CHILD_FD1_LINE"} {
		if !strings.Contains(gotErr, want) {
			t.Fatalf("stderr %q missing %q", gotErr, want)
		}
	}
}
