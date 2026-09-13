package process

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// validUnitNameRe matches systemd unit names: alphanumeric, @, ., _, :, -.
var validUnitNameRe = regexp.MustCompile(`^[a-zA-Z0-9@._:\-]+$`)

// isValidUnitName returns true if name looks like a valid systemd unit name.
// Used to prevent argument injection when passing unit names to shell commands.
func isValidUnitName(name string) bool {
	return name != "" && len(name) <= 256 && validUnitNameRe.MatchString(name)
}

// criticalProcessNames lists process names that require explicit confirmation
// before being signaled.
var criticalProcessNames = []string{
	"init",
	"systemd",
	"sshd",
	"dbus-daemon",
	"dbus-broker",
	"polkitd",
	"systemd-journald",
	"systemd-udevd",
	"NetworkManager",
}

// getProcessComm reads the command name for a given pid from /proc/<pid>/comm.
// Falls back to empty string if /proc is unavailable or process does not exist.
func getProcessComm(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// isCriticalProcess checks if a process is a critical system process that requires
// confirmation before being signaled.
func isCriticalProcess(pid int) (bool, string) {
	if pid == 1 {
		comm := getProcessComm(1)
		if comm == "" {
			comm = "systemd"
		}
		return true, comm
	}
	if pid == os.Getpid() {
		return true, "process-mcp"
	}
	comm := getProcessComm(pid)
	if comm != "" {
		for _, crit := range criticalProcessNames {
			if comm == crit || strings.EqualFold(comm, crit) {
				return true, comm
			}
		}
	}
	return false, comm
}

// ---------------------------------------------------------------------------
// Parsing helpers — extracted from handler closures for testability.
// ---------------------------------------------------------------------------

// parsePsLine parses a single line of `ps aux` output into a ProcessInfo.
// Returns ok=false if the line has fewer than 11 fields.
func parsePsLine(line string) (ProcessInfo, bool) {
	fields := strings.Fields(line)
	if len(fields) < 11 {
		return ProcessInfo{}, false
	}

	pid, _ := strconv.Atoi(fields[1])
	cpu, _ := strconv.ParseFloat(fields[2], 64)
	mem, _ := strconv.ParseFloat(fields[3], 64)
	vsz, _ := strconv.Atoi(fields[4])
	rss, _ := strconv.Atoi(fields[5])

	return ProcessInfo{
		User:    fields[0],
		PID:     pid,
		CPU:     cpu,
		Mem:     mem,
		VSZ:     vsz,
		RSS:     rss,
		TTY:     fields[6],
		Stat:    fields[7],
		Start:   fields[8],
		Time:    fields[9],
		Command: strings.Join(fields[10:], " "),
	}, true
}

// parseSsLines parses the output of `ss -tlnp` into a slice of PortEntry.
// filterPort, if > 0, restricts results to that port number.
func parseSsLines(output string, filterPort int) []PortEntry {
	lines := strings.Split(output, "\n")
	var ports []PortEntry
	for i, line := range lines {
		if i < 1 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "LISTEN" {
			continue
		}

		localAddr := fields[3]
		addr, portNum, ok := parseAddrPort(localAddr)
		if !ok {
			continue
		}
		if filterPort > 0 && portNum != filterPort {
			continue
		}

		process := ""
		for _, f := range fields {
			if strings.HasPrefix(f, "users:") {
				process = f
				break
			}
		}

		ports = append(ports, PortEntry{
			Protocol: "tcp",
			Address:  addr,
			Port:     portNum,
			Process:  process,
		})
	}
	return ports
}

// parseNetConnections parses the output of `ss -tupnH` or `ss -tupn` for a specific targetPid.
func parseNetConnections(output string, targetPid int, protoFilter string) []NetConnectionEntry {
	lines := strings.Split(output, "\n")
	var conns []NetConnectionEntry
	protoFilter = strings.ToLower(strings.TrimSpace(protoFilter))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Netid") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}

		proto := strings.ToLower(fields[0])
		if protoFilter != "" && protoFilter != "all" && proto != protoFilter {
			continue
		}

		pid := extractPidFromSsLine(line)
		if targetPid > 0 && pid != targetPid {
			continue
		}

		state := fields[1]
		localAddr, localPort := parseAddrPortAny(fields[4])
		remoteAddr, remotePort := parseAddrPortAny(fields[5])

		conns = append(conns, NetConnectionEntry{
			Proto:      proto,
			LocalAddr:  localAddr,
			LocalPort:  localPort,
			RemoteAddr: remoteAddr,
			RemotePort: remotePort,
			State:      state,
		})
	}
	return conns
}

func parseAddrPortAny(addrStr string) (addr string, port int) {
	lastColon := strings.LastIndex(addrStr, ":")
	if lastColon < 0 {
		return addrStr, 0
	}
	addr = addrStr[:lastColon]
	p, err := strconv.Atoi(addrStr[lastColon+1:])
	if err != nil {
		return addr, 0
	}
	return addr, p
}

// parseAddrPort extracts address and port from an "addr:port" string.
// Uses the last colon as delimiter to handle IPv6 addresses.
func parseAddrPort(localAddr string) (addr string, port int, ok bool) {
	lastColon := strings.LastIndex(localAddr, ":")
	if lastColon < 0 {
		return "", 0, false
	}
	addr = localAddr[:lastColon]
	port, err := strconv.Atoi(localAddr[lastColon+1:])
	if err != nil {
		return "", 0, false
	}
	return addr, port, true
}

// extractPidFromSsLine extracts a PID from an ss output line containing "pid=N".
// Returns 0 if no PID is found.
func extractPidFromSsLine(line string) int {
	if _, after, ok := strings.Cut(line, "pid="); ok {
		if end := strings.IndexAny(after, ",)"); end > 0 {
			pid, _ := strconv.Atoi(after[:end])
			return pid
		}
		// No delimiter found — try the whole remaining string
		pid, _ := strconv.Atoi(strings.TrimSpace(after))
		return pid
	}
	return 0
}

// parseGpuInfo parses comma-separated nvidia-smi --query-gpu output into GpuInfo.
// Returns nil if fewer than 7 fields are present.
func parseGpuInfo(csvLine string) *GpuInfo {
	fields := strings.Split(csvLine, ", ")
	if len(fields) < 7 {
		return nil
	}
	temp, _ := strconv.Atoi(strings.TrimSpace(fields[2]))
	util, _ := strconv.Atoi(strings.TrimSpace(fields[3]))
	memUsed, _ := strconv.Atoi(strings.TrimSpace(fields[4]))
	memTotal, _ := strconv.Atoi(strings.TrimSpace(fields[5]))
	power, _ := strconv.ParseFloat(strings.TrimSpace(fields[6]), 64)

	return &GpuInfo{
		Vendor:        "NVIDIA",
		DriverVersion: strings.TrimSpace(fields[0]),
		Name:          strings.TrimSpace(fields[1]),
		Temperature:   temp,
		Utilization:   util,
		MemoryUsed:    memUsed,
		MemoryTotal:   memTotal,
		PowerDraw:     power,
	}
}

// parseGpuProcessLine parses a single line of nvidia-smi --query-compute-apps output.
// Returns ok=false if fewer than 3 comma-separated fields.
func parseGpuProcessLine(line string) (GpuProcess, bool) {
	pfields := strings.Split(line, ", ")
	if len(pfields) < 3 {
		return GpuProcess{}, false
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(pfields[0]))
	mem, _ := strconv.Atoi(strings.TrimSpace(pfields[2]))
	return GpuProcess{
		PID:        pid,
		Name:       strings.TrimSpace(pfields[1]),
		MemoryUsed: mem,
	}, true
}

// parseRocmGpuInfo parses rocm-smi JSON output into GpuInfo and processes.
func parseRocmGpuInfo(data []byte) (*GpuInfo, []GpuProcess) {
	var raw map[string]map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil
	}

	for _, card := range raw {
		name := "AMD Radeon"
		if s, ok := card["Card series"].(string); ok && s != "" {
			name = s
		} else if s, ok := card["Card Model"].(string); ok && s != "" {
			name = s
		}

		driver := "rocm"
		if d, ok := card["Driver version"].(string); ok && d != "" {
			driver = d
		}

		temp := 0
		for k, v := range card {
			if strings.Contains(strings.ToLower(k), "temperature") {
				if s, ok := v.(string); ok {
					if f, err := strconv.ParseFloat(s, 64); err == nil {
						temp = int(f)
						break
					}
				}
			}
		}

		util := 0
		for k, v := range card {
			if strings.Contains(strings.ToLower(k), "gpu use") {
				if s, ok := v.(string); ok {
					if i, err := strconv.Atoi(strings.TrimSuffix(s, "%")); err == nil {
						util = i
						break
					}
				}
			}
		}

		memTotalMB := 0
		memUsedMB := 0
		for k, v := range card {
			if strings.Contains(strings.ToLower(k), "vram total memory") {
				if s, ok := v.(string); ok {
					if bytes, err := strconv.ParseInt(s, 10, 64); err == nil {
						memTotalMB = int(bytes / (1024 * 1024))
					}
				}
			}
			if strings.Contains(strings.ToLower(k), "vram total used") {
				if s, ok := v.(string); ok {
					if bytes, err := strconv.ParseInt(s, 10, 64); err == nil {
						memUsedMB = int(bytes / (1024 * 1024))
					}
				}
			}
		}

		power := 0.0
		for k, v := range card {
			if strings.Contains(strings.ToLower(k), "power") {
				if s, ok := v.(string); ok {
					if p, err := strconv.ParseFloat(s, 64); err == nil {
						power = p
						break
					}
				}
			}
		}

		info := &GpuInfo{
			Vendor:        "AMD",
			DriverVersion: driver,
			Name:          name,
			Temperature:   temp,
			Utilization:   util,
			MemoryUsed:    memUsedMB,
			MemoryTotal:   memTotalMB,
			PowerDraw:     power,
		}

		return info, []GpuProcess{}
	}

	return nil, nil
}

// parseKernelVersion extracts the kernel version from /proc/version content.
// Returns empty string if fewer than 3 fields.
func parseKernelVersion(procVersion string) string {
	fields := strings.Fields(procVersion)
	if len(fields) >= 3 {
		return fields[2]
	}
	return ""
}

// parseUptime parses /proc/uptime content into a human-readable string.
// Returns empty string if the content is empty or unparseable.
func parseUptime(procUptime string) string {
	fields := strings.Fields(procUptime)
	if len(fields) < 1 {
		return ""
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return ""
	}
	totalSecs := int(secs)
	days := totalSecs / 86400
	hours := (totalSecs % 86400) / 3600
	mins := (totalSecs % 3600) / 60
	return strings.Join([]string{
		strconv.Itoa(days) + "d",
		strconv.Itoa(hours) + "h",
		strconv.Itoa(mins) + "m",
	}, " ")
}

// parseLoadAvg parses /proc/loadavg content into 3 float64 values.
// Returns nil if fewer than 3 fields.
func parseLoadAvg(procLoadAvg string) []float64 {
	fields := strings.Fields(procLoadAvg)
	if len(fields) < 3 {
		return nil
	}
	avg := make([]float64, 3)
	avg[0], _ = strconv.ParseFloat(fields[0], 64)
	avg[1], _ = strconv.ParseFloat(fields[1], 64)
	avg[2], _ = strconv.ParseFloat(fields[2], 64)
	return avg
}

// parseCpuCount counts "processor" lines in /proc/cpuinfo content.
func parseCpuCount(procCpuinfo string) int {
	count := 0
	for line := range strings.SplitSeq(procCpuinfo, "\n") {
		if strings.HasPrefix(line, "processor") {
			count++
		}
	}
	return count
}

// DetailedMemInfo contains parsed /proc/meminfo metrics.
type DetailedMemInfo struct {
	TotalKB     int
	AvailKB     int
	FreeKB      int
	UsedKB      int
	BuffersKB   int
	CachedKB    int
	SwapTotalKB int
	SwapFreeKB  int
	SwapUsedKB  int
}

// parseDetailedMemInfo parses /proc/meminfo content into a DetailedMemInfo struct.
func parseDetailedMemInfo(procMeminfo string) DetailedMemInfo {
	memMap := make(map[string]int)
	for line := range strings.SplitSeq(procMeminfo, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			val, _ := strconv.Atoi(parts[1])
			key := strings.TrimSuffix(parts[0], ":")
			memMap[key] = val
		}
	}
	total := memMap["MemTotal"]
	avail := memMap["MemAvailable"]
	free := memMap["MemFree"]
	buffers := memMap["Buffers"]
	cached := memMap["Cached"]
	swapTotal := memMap["SwapTotal"]
	swapFree := memMap["SwapFree"]

	used := total - avail
	if used < 0 {
		used = total - free - buffers - cached
		if used < 0 {
			used = 0
		}
	}
	swapUsed := swapTotal - swapFree
	if swapUsed < 0 {
		swapUsed = 0
	}

	return DetailedMemInfo{
		TotalKB:     total,
		AvailKB:     avail,
		FreeKB:      free,
		UsedKB:      used,
		BuffersKB:   buffers,
		CachedKB:    cached,
		SwapTotalKB: swapTotal,
		SwapFreeKB:  swapFree,
		SwapUsedKB:  swapUsed,
	}
}

// parseOOMScore reads /proc/[pid]/oom_score and /proc/[pid]/oom_score_adj.
func parseOOMScore(pid int) (score int, adj int) {
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/oom_score", pid)); err == nil {
		score, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/oom_score_adj", pid)); err == nil {
		adj, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	return score, adj
}

// parseProcessSmapsRollup reads /proc/[pid]/smaps_rollup and extracts PSS and memory breakdown in KB.
func parseProcessSmapsRollup(pid int) (pssKB, sharedCleanKB, sharedDirtyKB, privateCleanKB, privateDirtyKB, swapKB int, ok bool) {
	smaps, err := os.ReadFile(fmt.Sprintf("/proc/%d/smaps_rollup", pid))
	if err != nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	for line := range strings.SplitSeq(string(smaps), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			val, _ := strconv.Atoi(parts[1])
			switch parts[0] {
			case "Pss:":
				pssKB = val
			case "Shared_Clean:":
				sharedCleanKB = val
			case "Shared_Dirty:":
				sharedDirtyKB = val
			case "Private_Clean:":
				privateCleanKB = val
			case "Private_Dirty:":
				privateDirtyKB = val
			case "Swap:":
				swapKB = val
			}
		}
	}
	return pssKB, sharedCleanKB, sharedDirtyKB, privateCleanKB, privateDirtyKB, swapKB, true
}

// ProcessCpuEntry represents an inspected process's CPU usage and scheduling info.
type ProcessCpuEntry struct {
	PID        int     `json:"pid"`
	User       string  `json:"user"`
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float64 `json:"mem_percent"`
	Stat       string  `json:"stat"`
	Priority   int     `json:"priority"`
	Nice       int     `json:"nice"`
	Threads    int     `json:"threads"`
	CPUTime    string  `json:"cpu_time"`
	Command    string  `json:"command"`
	CGroup     string  `json:"cgroup,omitempty"`
}

// parsePsCpuLine parses a single line from `ps -eo pid,user,pcpu,pmem,stat,pri,ni,nlwp,time,command`.
func parsePsCpuLine(line string) (ProcessCpuEntry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return ProcessCpuEntry{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return ProcessCpuEntry{}, false
	}
	user := fields[1]
	cpu, _ := strconv.ParseFloat(fields[2], 64)
	mem, _ := strconv.ParseFloat(fields[3], 64)
	stat := fields[4]
	pri, _ := strconv.Atoi(fields[5])
	ni, _ := strconv.Atoi(fields[6])
	threads, _ := strconv.Atoi(fields[7])
	cpuTime := fields[8]
	cmd := strings.Join(fields[9:], " ")

	return ProcessCpuEntry{
		PID:        pid,
		User:       user,
		CPUPercent: cpu,
		MemPercent: mem,
		Stat:       stat,
		Priority:   pri,
		Nice:       ni,
		Threads:    threads,
		CPUTime:    cpuTime,
		Command:    cmd,
	}, true
}

// parseLoadTrend determines whether the 1-minute load average is rising, falling, or stable relative to the 5-minute average.
func parseLoadTrend(load1m, load5m float64) string {
	if load5m <= 0 {
		return "stable"
	}
	ratio := load1m / load5m
	switch {
	case ratio > 1.10:
		return "rising"
	case ratio < 0.90:
		return "falling"
	default:
		return "stable"
	}
}

// parseMemInfo parses /proc/meminfo content and returns MemTotal, MemAvailable,
// SwapTotal, and SwapFree in kilobytes.
func parseMemInfo(procMeminfo string) (memTotal, memAvail, swapTotal, swapFree int) {
	memMap := make(map[string]int)
	for line := range strings.SplitSeq(procMeminfo, "\n") {
		if strings.HasPrefix(line, "MemTotal:") ||
			strings.HasPrefix(line, "MemAvailable:") ||
			strings.HasPrefix(line, "SwapTotal:") ||
			strings.HasPrefix(line, "SwapFree:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				val, _ := strconv.Atoi(parts[1])
				key := strings.TrimSuffix(parts[0], ":")
				memMap[key] = val
			}
		}
	}
	return memMap["MemTotal"], memMap["MemAvailable"], memMap["SwapTotal"], memMap["SwapFree"]
}

// filterSsPortsByPid scans ss -tlnp output for LISTEN lines that contain
// the given PID string, and returns matching PortEntry items.
func filterSsPortsByPid(ssOutput string, pidStr string) []PortEntry {
	var ports []PortEntry
	for line := range strings.SplitSeq(ssOutput, "\n") {
		if !strings.Contains(line, pidStr) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "LISTEN" {
			continue
		}
		addr, portNum, ok := parseAddrPort(fields[3])
		if !ok {
			continue
		}
		ports = append(ports, PortEntry{
			Protocol: "tcp",
			Address:  addr,
			Port:     portNum,
		})
	}
	return ports
}

// extractSystemdUnit finds the first .service unit name from
// `systemctl status` output by scanning each line for words ending in ".service".
func extractSystemdUnit(statusOutput string) string {
	for line := range strings.SplitSeq(statusOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".service") || strings.Contains(line, ".service ") {
			parts := strings.FieldsSeq(line)
			for p := range parts {
				if strings.HasSuffix(p, ".service") {
					return p
				}
			}
		}
	}
	return ""
}

// parseSystemdProperties parses `systemctl show --property=...` output
// into key-value pairs.
func parseSystemdProperties(output string) map[string]string {
	props := make(map[string]string)
	for line := range strings.SplitSeq(output, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			props[parts[0]] = parts[1]
		}
	}
	return props
}
