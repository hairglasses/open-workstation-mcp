package process

import (
	"os"
	"testing"
)

func TestParseProcStat_Synthetic(t *testing.T) {
	raw := "1234 (test app (worker)) S 100 200 300 0 -1 4194304 10 20 30 40 500 250 100 50 20 0 4 0 987654 104857600 2560 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	stat, err := parseProcStat(raw)
	if err != nil {
		t.Fatalf("parseProcStat failed: %v", err)
	}

	if stat.PID != 1234 {
		t.Errorf("expected PID=1234, got %d", stat.PID)
	}
	if stat.Comm != "test app (worker)" {
		t.Errorf("expected Comm='test app (worker)', got %q", stat.Comm)
	}
	if stat.State != "S" {
		t.Errorf("expected State='S', got %q", stat.State)
	}
	if stat.PPID != 100 {
		t.Errorf("expected PPID=100, got %d", stat.PPID)
	}
	if stat.UTime != 500 {
		t.Errorf("expected UTime=500, got %d", stat.UTime)
	}
	if stat.STime != 250 {
		t.Errorf("expected STime=250, got %d", stat.STime)
	}
	if stat.NumThreads != 4 {
		t.Errorf("expected NumThreads=4, got %d", stat.NumThreads)
	}
	if stat.StartTime != 987654 {
		t.Errorf("expected StartTime=987654, got %d", stat.StartTime)
	}
	if stat.VSize != 104857600 {
		t.Errorf("expected VSize=104857600, got %d", stat.VSize)
	}
	if stat.RSSPages != 2560 {
		t.Errorf("expected RSSPages=2560, got %d", stat.RSSPages)
	}
}

func TestParseProcStat_Malformed(t *testing.T) {
	cases := []string{
		"",
		"invalid",
		"1234 no parens S 1 2 3",
		"1234 () S 1 2",
	}
	for _, c := range cases {
		_, err := parseProcStat(c)
		if err == nil {
			t.Errorf("expected error for malformed input: %q", c)
		}
	}
}

func TestParseProcStatus_Synthetic(t *testing.T) {
	raw := `Name:	my-service
State:	R (running)
Tgid:	4567
Pid:	4567
PPid:	1
FDSize:	128
Groups:	1000 
VmPeak:	   20480 kB
VmSize:	   16384 kB
VmHWM:	    8192 kB
VmRSS:	    4096 kB
RssAnon:	    2048 kB
RssFile:	    2048 kB
RssShmem:	       0 kB
VmData:	    1024 kB
VmStk:	     128 kB
VmExe:	      64 kB
VmSwap:	     512 kB
Threads:	3
Uid:	1000	1000	1000	1000
Gid:	1000	1000	1000	1000
voluntary_ctxt_switches:	4200
nonvoluntary_ctxt_switches:	15
`
	summary := parseProcStatus(raw)
	if summary.FDSize != 128 {
		t.Errorf("expected FDSize=128, got %d", summary.FDSize)
	}
	if summary.Threads != 3 {
		t.Errorf("expected Threads=3, got %d", summary.Threads)
	}
	if summary.VmPeakKB != 20480 {
		t.Errorf("expected VmPeakKB=20480, got %d", summary.VmPeakKB)
	}
	if summary.VmSizeKB != 16384 {
		t.Errorf("expected VmSizeKB=16384, got %d", summary.VmSizeKB)
	}
	if summary.VmHWMKB != 8192 {
		t.Errorf("expected VmHWMKB=8192, got %d", summary.VmHWMKB)
	}
	if summary.VmRSSKB != 4096 {
		t.Errorf("expected VmRSSKB=4096, got %d", summary.VmRSSKB)
	}
	if summary.RssAnonKB != 2048 {
		t.Errorf("expected RssAnonKB=2048, got %d", summary.RssAnonKB)
	}
	if summary.VmSwapKB != 512 {
		t.Errorf("expected VmSwapKB=512, got %d", summary.VmSwapKB)
	}
	if len(summary.Uid) != 4 || summary.Uid[0] != 1000 {
		t.Errorf("expected Uid=[1000 1000 1000 1000], got %v", summary.Uid)
	}
	if summary.VoluntaryCtxtSwitches != 4200 {
		t.Errorf("expected VoluntaryCtxtSwitches=4200, got %d", summary.VoluntaryCtxtSwitches)
	}
	if summary.NonvoluntaryCtxtSwitches != 15 {
		t.Errorf("expected NonvoluntaryCtxtSwitches=15, got %d", summary.NonvoluntaryCtxtSwitches)
	}
}

func TestInspectProcess_Self(t *testing.T) {
	pid := os.Getpid()
	out, err := InspectProcess(ProcInspectInput{
		PID:              pid,
		SampleDurationMs: 20,
	})
	if err != nil {
		t.Fatalf("InspectProcess(self) failed: %v", err)
	}

	if out.PID != pid {
		t.Errorf("expected PID=%d, got %d", pid, out.PID)
	}
	if out.Comm == "" {
		t.Error("expected non-empty Comm")
	}
	if out.State == "" {
		t.Error("expected non-empty State")
	}
	if out.NumThreads < 1 {
		t.Errorf("expected NumThreads >= 1, got %d", out.NumThreads)
	}
	if out.FDCount < 1 {
		t.Errorf("expected FDCount >= 1, got %d", out.FDCount)
	}
	if out.RSSBytes == 0 {
		t.Error("expected RSSBytes > 0")
	}
	if out.RSSMB <= 0 {
		t.Error("expected RSSMB > 0")
	}
	if out.VIRTMB <= 0 {
		t.Error("expected VIRTMB > 0")
	}
	if out.CPUTelemetry.TotalTicks == 0 && out.CPUTelemetry.UTime == 0 {
		t.Logf("CPUTelemetry: %+v", out.CPUTelemetry)
	}
	if out.CPUTelemetry.SampleDurationMs != 20 {
		t.Errorf("expected SampleDurationMs=20, got %d", out.CPUTelemetry.SampleDurationMs)
	}
}

func TestInspectProcess_Validation(t *testing.T) {
	_, err := InspectProcess(ProcInspectInput{PID: 0})
	if err == nil {
		t.Error("expected error for PID <= 0")
	}

	_, err = InspectProcess(ProcInspectInput{PID: -5})
	if err == nil {
		t.Error("expected error for negative PID")
	}

	_, err = InspectProcess(ProcInspectInput{PID: 999999999})
	if err == nil {
		t.Error("expected error for non-existent PID")
	}
}

func TestFormatProcState(t *testing.T) {
	cases := map[string]string{
		"R": "R (running)",
		"S": "S (sleeping)",
		"D": "D (disk sleep / uninterruptible)",
		"Z": "Z (zombie)",
		"T": "T (stopped)",
		"t": "t (tracing stop)",
		"X": "X (dead)",
		"I": "I (idle)",
		"Q": "Q",
	}
	for in, want := range cases {
		got := formatProcState(in)
		if got != want {
			t.Errorf("formatProcState(%q) = %q, want %q", in, got, want)
		}
	}
}
