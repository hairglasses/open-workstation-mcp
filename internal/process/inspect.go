package process

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
)

type ProcInspectInput struct {
	PID              int `json:"pid" jsonschema:"required,description=Process ID to deeply inspect in /proc"`
	SampleDurationMs int `json:"sample_duration_ms,omitempty" jsonschema:"description=Duration in ms to sample CPU ticks for delta calculation. Default 0. Max 1000."`
}

type ProcStatTelemetry struct {
	PID        int    `json:"pid"`
	Comm       string `json:"comm"`
	State      string `json:"state"`
	PPID       int    `json:"ppid"`
	PGRP       int    `json:"pgrp"`
	Session    int    `json:"session"`
	TTY        int    `json:"tty"`
	TPGID      int    `json:"tpgid"`
	Flags      uint   `json:"flags"`
	MinFlt     uint64 `json:"minflt"`
	CMinFlt    uint64 `json:"cminflt"`
	MajFlt     uint64 `json:"majflt"`
	CMajFlt    uint64 `json:"cmajflt"`
	UTime      uint64 `json:"utime"`
	STime      uint64 `json:"stime"`
	CUTime     int64  `json:"cutime"`
	CSTime     int64  `json:"cstime"`
	Priority   int64  `json:"priority"`
	Nice       int64  `json:"nice"`
	NumThreads int    `json:"num_threads"`
	StartTime  uint64 `json:"starttime"`
	VSize      uint64 `json:"vsize"`
	RSSPages   int64  `json:"rss_pages"`
}

type ProcStatusSummary struct {
	Uid                      []int `json:"uid,omitempty"`
	Gid                      []int `json:"gid,omitempty"`
	FDSize                   int   `json:"fd_size"`
	Threads                  int   `json:"threads"`
	VmPeakKB                 int   `json:"vm_peak_kb"`
	VmSizeKB                 int   `json:"vm_size_kb"`
	VmHWMKB                  int   `json:"vm_hwm_kb"`
	VmRSSKB                  int   `json:"vm_rss_kb"`
	RssAnonKB                int   `json:"rss_anon_kb"`
	RssFileKB                int   `json:"rss_file_kb"`
	RssShmemKB               int   `json:"rss_shmem_kb"`
	VmDataKB                 int   `json:"vm_data_kb"`
	VmStkKB                  int   `json:"vm_stk_kb"`
	VmExeKB                  int   `json:"vm_exe_kb"`
	VmSwapKB                 int   `json:"vm_swap_kb"`
	VoluntaryCtxtSwitches    int64 `json:"voluntary_ctxt_switches"`
	NonvoluntaryCtxtSwitches int64 `json:"nonvoluntary_ctxt_switches"`
}

type ProcCpuTelemetry struct {
	UTime            uint64  `json:"utime"`
	STime            uint64  `json:"stime"`
	TotalTicks       uint64  `json:"total_ticks"`
	SampleDurationMs int     `json:"sample_duration_ms"`
	DeltaUTime       uint64  `json:"delta_utime"`
	DeltaSTime       uint64  `json:"delta_stime"`
	DeltaTotalTicks  uint64  `json:"delta_total_ticks"`
	CPUPercent       float64 `json:"cpu_percent"`
}

type ProcInspectOutput struct {
	PID              int               `json:"pid"`
	PPID             int               `json:"ppid"`
	Comm             string            `json:"comm"`
	State            string            `json:"state"`
	StateDescription string            `json:"state_description"`
	Cmdline          []string          `json:"cmdline"`
	Exe              string            `json:"exe,omitempty"`
	Cwd              string            `json:"cwd,omitempty"`
	NumThreads       int               `json:"num_threads"`
	FDCount          int               `json:"fd_count"`
	FDMax            int               `json:"fd_max,omitempty"`
	FDSize           int               `json:"fd_size,omitempty"`
	RSSBytes         uint64            `json:"rss_bytes"`
	VIRTBytes        uint64            `json:"virt_bytes"`
	RSSMB            float64           `json:"rss_mb"`
	VIRTMB           float64           `json:"virt_mb"`
	SwapMB           float64           `json:"swap_mb"`
	PeakRSSMB        float64           `json:"peak_rss_mb,omitempty"`
	PeakVIRTMB       float64           `json:"peak_virt_mb,omitempty"`
	Stat             ProcStatTelemetry `json:"stat"`
	Status           ProcStatusSummary `json:"status"`
	CPUTelemetry     ProcCpuTelemetry  `json:"cpu_telemetry"`
}

func parseProcStat(statContent string) (ProcStatTelemetry, error) {
	lastParen := strings.LastIndex(statContent, ")")
	if lastParen == -1 || lastParen+2 >= len(statContent) {
		return ProcStatTelemetry{}, fmt.Errorf("malformed stat content")
	}

	firstParen := strings.Index(statContent, "(")
	if firstParen == -1 || firstParen >= lastParen {
		return ProcStatTelemetry{}, fmt.Errorf("malformed comm in stat")
	}

	pidStr := strings.TrimSpace(statContent[:firstParen])
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return ProcStatTelemetry{}, fmt.Errorf("parse stat pid: %w", err)
	}

	comm := statContent[firstParen+1 : lastParen]
	fields := strings.Fields(statContent[lastParen+2:])
	if len(fields) < 22 {
		return ProcStatTelemetry{}, fmt.Errorf("stat fields length too short: %d", len(fields))
	}

	state := fields[0]
	ppid, _ := strconv.Atoi(fields[1])
	pgrp, _ := strconv.Atoi(fields[2])
	session, _ := strconv.Atoi(fields[3])
	tty, _ := strconv.Atoi(fields[4])
	tpgid, _ := strconv.Atoi(fields[5])
	flags64, _ := strconv.ParseUint(fields[6], 10, 64)
	minflt, _ := strconv.ParseUint(fields[7], 10, 64)
	cminflt, _ := strconv.ParseUint(fields[8], 10, 64)
	majflt, _ := strconv.ParseUint(fields[9], 10, 64)
	cmajflt, _ := strconv.ParseUint(fields[10], 10, 64)
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	cutime, _ := strconv.ParseInt(fields[13], 10, 64)
	cstime, _ := strconv.ParseInt(fields[14], 10, 64)
	priority, _ := strconv.ParseInt(fields[15], 10, 64)
	nice, _ := strconv.ParseInt(fields[16], 10, 64)
	numThreads, _ := strconv.Atoi(fields[17])
	starttime, _ := strconv.ParseUint(fields[19], 10, 64)
	vsize, _ := strconv.ParseUint(fields[20], 10, 64)
	rss, _ := strconv.ParseInt(fields[21], 10, 64)

	return ProcStatTelemetry{
		PID:        pid,
		Comm:       comm,
		State:      state,
		PPID:       ppid,
		PGRP:       pgrp,
		Session:    session,
		TTY:        tty,
		TPGID:      tpgid,
		Flags:      uint(flags64),
		MinFlt:     minflt,
		CMinFlt:    cminflt,
		MajFlt:     majflt,
		CMajFlt:    cmajflt,
		UTime:      utime,
		STime:      stime,
		CUTime:     cutime,
		CSTime:     cstime,
		Priority:   priority,
		Nice:       nice,
		NumThreads: numThreads,
		StartTime:  starttime,
		VSize:      vsize,
		RSSPages:   rss,
	}, nil
}

func parseProcStatus(statusContent string) ProcStatusSummary {
	var summary ProcStatusSummary
	lines := strings.Split(statusContent, "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "FDSize":
			summary.FDSize, _ = strconv.Atoi(val)
		case "Threads":
			summary.Threads, _ = strconv.Atoi(val)
		case "VmPeak":
			summary.VmPeakKB, _ = parseKBVal(val)
		case "VmSize":
			summary.VmSizeKB, _ = parseKBVal(val)
		case "VmHWM":
			summary.VmHWMKB, _ = parseKBVal(val)
		case "VmRSS":
			summary.VmRSSKB, _ = parseKBVal(val)
		case "RssAnon":
			summary.RssAnonKB, _ = parseKBVal(val)
		case "RssFile":
			summary.RssFileKB, _ = parseKBVal(val)
		case "RssShmem":
			summary.RssShmemKB, _ = parseKBVal(val)
		case "VmData":
			summary.VmDataKB, _ = parseKBVal(val)
		case "VmStk":
			summary.VmStkKB, _ = parseKBVal(val)
		case "VmExe":
			summary.VmExeKB, _ = parseKBVal(val)
		case "VmSwap":
			summary.VmSwapKB, _ = parseKBVal(val)
		case "Uid":
			fields := strings.Fields(val)
			for _, f := range fields {
				if id, err := strconv.Atoi(f); err == nil {
					summary.Uid = append(summary.Uid, id)
				}
			}
		case "Gid":
			fields := strings.Fields(val)
			for _, f := range fields {
				if id, err := strconv.Atoi(f); err == nil {
					summary.Gid = append(summary.Gid, id)
				}
			}
		case "voluntary_ctxt_switches":
			summary.VoluntaryCtxtSwitches, _ = strconv.ParseInt(val, 10, 64)
		case "nonvoluntary_ctxt_switches":
			summary.NonvoluntaryCtxtSwitches, _ = strconv.ParseInt(val, 10, 64)
		}
	}
	return summary
}

func parseKBVal(val string) (int, error) {
	fields := strings.Fields(val)
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty kb value")
	}
	return strconv.Atoi(fields[0])
}

func parseProcCmdline(pid int) []string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		return nil
	}
	rawArgs := bytes.Split(data, []byte{0})
	var args []string
	for _, arg := range rawArgs {
		if len(arg) > 0 {
			args = append(args, string(arg))
		}
	}
	return args
}

func countProcFDs(pid int) (int, error) {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

func parseProcFDMax(pid int) int {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/limits", pid))
	if err != nil {
		return 0
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Max open files") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				limit, err := strconv.Atoi(fields[3])
				if err == nil {
					return limit
				}
			}
		}
	}
	return 0
}

func formatProcState(stateChar string) string {
	switch stateChar {
	case "R":
		return "R (running)"
	case "S":
		return "S (sleeping)"
	case "D":
		return "D (disk sleep / uninterruptible)"
	case "Z":
		return "Z (zombie)"
	case "T":
		return "T (stopped)"
	case "t":
		return "t (tracing stop)"
	case "X":
		return "X (dead)"
	case "I":
		return "I (idle)"
	default:
		return stateChar
	}
}

func sampleCpuTicks(pid int, durationMs int) (ProcCpuTelemetry, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data0, err := os.ReadFile(statPath)
	if err != nil {
		return ProcCpuTelemetry{}, fmt.Errorf("read initial stat: %w", err)
	}
	s0, err := parseProcStat(string(data0))
	if err != nil {
		return ProcCpuTelemetry{}, err
	}

	if durationMs <= 0 {
		return ProcCpuTelemetry{
			UTime:      s0.UTime,
			STime:      s0.STime,
			TotalTicks: s0.UTime + s0.STime,
		}, nil
	}

	if durationMs > 1000 {
		durationMs = 1000
	}

	t0 := time.Now()
	time.Sleep(time.Duration(durationMs) * time.Millisecond)
	data1, err := os.ReadFile(statPath)
	if err != nil {
		return ProcCpuTelemetry{
			UTime:      s0.UTime,
			STime:      s0.STime,
			TotalTicks: s0.UTime + s0.STime,
		}, nil
	}
	t1 := time.Now()
	s1, err := parseProcStat(string(data1))
	if err != nil {
		return ProcCpuTelemetry{
			UTime:      s0.UTime,
			STime:      s0.STime,
			TotalTicks: s0.UTime + s0.STime,
		}, nil
	}

	deltaU := uint64(0)
	if s1.UTime >= s0.UTime {
		deltaU = s1.UTime - s0.UTime
	}
	deltaS := uint64(0)
	if s1.STime >= s0.STime {
		deltaS = s1.STime - s0.STime
	}
	deltaTotal := deltaU + deltaS

	elapsed := t1.Sub(t0).Seconds()
	cpuPercent := 0.0
	if elapsed > 0 {
		cpuPercent = (float64(deltaTotal) / (elapsed * 100.0)) * 100.0
		cpuPercent = math.Round(cpuPercent*10) / 10
	}

	return ProcCpuTelemetry{
		UTime:            s1.UTime,
		STime:            s1.STime,
		TotalTicks:       s1.UTime + s1.STime,
		SampleDurationMs: durationMs,
		DeltaUTime:       deltaU,
		DeltaSTime:       deltaS,
		DeltaTotalTicks:  deltaTotal,
		CPUPercent:       cpuPercent,
	}, nil
}

func InspectProcess(input ProcInspectInput) (ProcInspectOutput, error) {
	if input.PID <= 0 {
		return ProcInspectOutput{}, fmt.Errorf("[%s] pid must be greater than 0", handler.ErrInvalidParam)
	}

	statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", input.PID))
	if err != nil {
		if os.IsNotExist(err) {
			return ProcInspectOutput{}, fmt.Errorf("[%s] process %d not found in /proc", handler.ErrNotFound, input.PID)
		}
		return ProcInspectOutput{}, fmt.Errorf("[%s] failed reading /proc/%d/stat: %w", handler.ErrAPIError, input.PID, err)
	}

	stat, err := parseProcStat(string(statData))
	if err != nil {
		return ProcInspectOutput{}, fmt.Errorf("[%s] failed parsing /proc/%d/stat: %w", handler.ErrAPIError, input.PID, err)
	}

	statusData, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", input.PID))
	status := parseProcStatus(string(statusData))

	cmdline := parseProcCmdline(input.PID)
	fdCount, _ := countProcFDs(input.PID)
	fdMax := parseProcFDMax(input.PID)

	exe, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", input.PID))
	cwd, _ := os.Readlink(fmt.Sprintf("/proc/%d/cwd", input.PID))

	pageSize := int64(os.Getpagesize())
	if pageSize <= 0 {
		pageSize = 4096
	}

	rssBytes := uint64(0)
	if status.VmRSSKB > 0 {
		rssBytes = uint64(status.VmRSSKB) * 1024
	} else if stat.RSSPages > 0 {
		rssBytes = uint64(stat.RSSPages * pageSize)
	}

	virtBytes := stat.VSize
	if virtBytes == 0 && status.VmSizeKB > 0 {
		virtBytes = uint64(status.VmSizeKB) * 1024
	}

	rssMB := math.Round((float64(rssBytes)/(1024*1024))*100) / 100
	virtMB := math.Round((float64(virtBytes)/(1024*1024))*100) / 100
	swapMB := math.Round((float64(status.VmSwapKB)/1024)*100) / 100
	peakRssMB := math.Round((float64(status.VmHWMKB)/1024)*100) / 100
	peakVirtMB := math.Round((float64(status.VmPeakKB)/1024)*100) / 100

	cpuTelem, _ := sampleCpuTicks(input.PID, input.SampleDurationMs)

	numThreads := stat.NumThreads
	if numThreads <= 0 && status.Threads > 0 {
		numThreads = status.Threads
	}

	return ProcInspectOutput{
		PID:              stat.PID,
		PPID:             stat.PPID,
		Comm:             stat.Comm,
		State:            stat.State,
		StateDescription: formatProcState(stat.State),
		Cmdline:          cmdline,
		Exe:              exe,
		Cwd:              cwd,
		NumThreads:       numThreads,
		FDCount:          fdCount,
		FDMax:            fdMax,
		FDSize:           status.FDSize,
		RSSBytes:         rssBytes,
		VIRTBytes:        virtBytes,
		RSSMB:            rssMB,
		VIRTMB:           virtMB,
		SwapMB:           swapMB,
		PeakRSSMB:        peakRssMB,
		PeakVIRTMB:       peakVirtMB,
		Stat:             stat,
		Status:           status,
		CPUTelemetry:     cpuTelem,
	}, nil
}
