// Command process-mcp is an MCP server for Linux process management,
// port inspection, GPU status, and system information via the Model
// Context Protocol (stdio transport).
//
// Usage:
//
//	process-mcp
package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/middleware/gate"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runCmd(name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = SafeCommandEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", "command timed out after 10s", ctx.Err()
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func readProcFile(name string) (string, error) {
	data, err := os.ReadFile("/proc/" + name)
	if err != nil {
		return "", fmt.Errorf("read /proc/%s: %w", name, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// readBootID reads the unique Linux kernel boot ID from /proc/sys/kernel/random/boot_id.
func readBootID() (string, error) {
	return readProcFile("sys/kernel/random/boot_id")
}

// readProcessStartTicks reads field 22 (starttime in clock ticks) from /proc/<pid>/stat.
func readProcessStartTicks(pid int) (uint64, error) {
	statStr, err := readProcFile(fmt.Sprintf("%d/stat", pid))
	if err != nil {
		return 0, err
	}
	lastParen := strings.LastIndex(statStr, ")")
	if lastParen == -1 || lastParen+2 >= len(statStr) {
		return 0, fmt.Errorf("malformed stat for pid %d", pid)
	}
	rest := strings.Fields(statStr[lastParen+2:])
	if len(rest) < 20 {
		return 0, fmt.Errorf("insufficient fields in stat for pid %d: got %d", pid, len(rest))
	}
	ticks, err := strconv.ParseUint(rest[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse starttime for pid %d: %w", pid, err)
	}
	return ticks, nil
}

// readProcessComm reads the process comm name from /proc/<pid>/comm.
func readProcessComm(pid int) (string, error) {
	return readProcFile(fmt.Sprintf("%d/comm", pid))
}

// ReadBootID reads the current kernel boot ID from /proc/sys/kernel/random/boot_id.
func ReadBootID() (string, error) {
	return readBootID()
}

// ReadProcessStartTicks reads the starttime in clock ticks (field 22) from /proc/<pid>/stat.
func ReadProcessStartTicks(pid int) (uint64, error) {
	return readProcessStartTicks(pid)
}

// ReadProcessComm reads the process comm name from /proc/<pid>/comm.
func ReadProcessComm(pid int) (string, error) {
	return readProcessComm(pid)
}

// ---------------------------------------------------------------------------
// ps_list
// ---------------------------------------------------------------------------

type PsListInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"description=Filter processes by command substring"`
	SortBy string `json:"sort_by,omitempty" jsonschema:"description=Sort by: cpu (default)\\, mem\\, or pid"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=Max processes to return. Default 20."`
}

type ProcessInfo struct {
	User    string  `json:"user"`
	PID     int     `json:"pid"`
	CPU     float64 `json:"cpu"`
	Mem     float64 `json:"mem"`
	VSZ     int     `json:"vsz"`
	RSS     int     `json:"rss"`
	TTY     string  `json:"tty"`
	Stat    string  `json:"stat"`
	Start   string  `json:"start"`
	Time    string  `json:"time"`
	Command string  `json:"command"`
}

type PsListOutput struct {
	Processes []ProcessInfo `json:"processes"`
	Total     int           `json:"total"`
}

// ---------------------------------------------------------------------------
// ps_tree
// ---------------------------------------------------------------------------

type PsTreeInput struct {
	PID int `json:"pid" jsonschema:"description=Process ID to show tree for,required"`
}

type PsTreeOutput struct {
	PID  int    `json:"pid"`
	Tree string `json:"tree"`
}

// ---------------------------------------------------------------------------
// kill_process
// ---------------------------------------------------------------------------

type KillProcessInput struct {
	PID          int    `json:"pid" jsonschema:"description=Process ID to signal,required"`
	Signal       string `json:"signal,omitempty" jsonschema:"description=Signal name: TERM (default)\\, KILL\\, HUP\\, INT\\, USR1\\, USR2\\, STOP\\, CONT"`
	Confirm      bool   `json:"confirm,omitempty" jsonschema:"description=Required confirmation to signal critical system processes (e.g. init\\, systemd\\, sshd\\, dbus)"`
	Host         string `json:"host,omitempty" jsonschema:"description=Host where process lives. If non-empty and != local hostname\\, refuse signaling."`
	BootID       string `json:"boot_id,omitempty" jsonschema:"description=Expected system boot ID. If non-empty and != /proc/sys/kernel/random/boot_id\\, refuse signaling."`
	StartTime    uint64 `json:"start_time,omitempty" jsonschema:"description=Expected process starttime in clock ticks (from /proc/<pid>/stat field 22). If > 0 and does not match current starttime\\, refuse signaling."`
	ExpectedComm string `json:"expected_comm,omitempty" jsonschema:"description=Expected process comm name. If non-empty and != /proc/<pid>/comm\\, refuse signaling."`
}

type KillProcessOutput struct {
	PID    int    `json:"pid"`
	Signal string `json:"signal"`
	Result string `json:"result"`
}

var validSignals = map[string]bool{
	"TERM": true, "KILL": true, "HUP": true, "INT": true,
	"USR1": true, "USR2": true, "STOP": true, "CONT": true,
}

// ---------------------------------------------------------------------------
// port_list
// ---------------------------------------------------------------------------

type PortListInput struct {
	Port int `json:"port,omitempty" jsonschema:"description=Filter by specific port number"`
}

type PortEntry struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Process  string `json:"process"`
}

type PortListOutput struct {
	Ports []PortEntry `json:"ports"`
	Total int         `json:"total"`
}

// ---------------------------------------------------------------------------
// gpu_status
// ---------------------------------------------------------------------------

type GpuStatusInput struct {
	IncludeProcesses *bool `json:"include_processes,omitempty" jsonschema:"description=Optional flag to include per-process GPU memory metrics (default true)"`
}

type GpuInfo struct {
	Vendor        string  `json:"vendor,omitempty"`
	DriverVersion string  `json:"driver_version"`
	Name          string  `json:"name"`
	Temperature   int     `json:"temperature"`
	Utilization   int     `json:"utilization"`
	MemoryUsed    int     `json:"memory_used_mb"`
	MemoryTotal   int     `json:"memory_total_mb"`
	PowerDraw     float64 `json:"power_draw_w"`
}

type GpuProcess struct {
	PID        int    `json:"pid"`
	Name       string `json:"name"`
	MemoryUsed int    `json:"memory_used_mb"`
}

type GpuStatusOutput struct {
	GPU       *GpuInfo     `json:"gpu,omitempty"`
	Processes []GpuProcess `json:"processes"`
}

// ---------------------------------------------------------------------------
// system_info
// ---------------------------------------------------------------------------

type SystemInfoInput struct {
	IncludeCGroup *bool `json:"include_cgroup,omitempty" jsonschema:"description=Optional flag to include cgroup controller information (default true)"`
}

type SystemInfoOutput struct {
	Hostname    string            `json:"hostname"`
	Kernel      string            `json:"kernel"`
	Uptime      string            `json:"uptime"`
	LoadAvg     []float64         `json:"load_avg"`
	CPUCount    int               `json:"cpu_count"`
	MemTotalMB  int               `json:"mem_total_mb"`
	MemAvailMB  int               `json:"mem_available_mb"`
	SwapTotalMB int               `json:"swap_total_mb"`
	SwapUsedMB  int               `json:"swap_used_mb"`
	CGroup      string            `json:"cgroup,omitempty"`
	Namespaces  map[string]string `json:"namespaces,omitempty"`
}

// ---------------------------------------------------------------------------
// mem_map
// ---------------------------------------------------------------------------

type MemMapInput struct {
	PID   int `json:"pid" jsonschema:"description=Process ID to inspect memory mappings for,required"`
	Limit int `json:"limit,omitempty" jsonschema:"description=Max memory regions to return. Default 50."`
}

type MemoryRegion struct {
	AddressRange string `json:"address_range"`
	Permissions  string `json:"permissions"`
	Offset       string `json:"offset"`
	Device       string `json:"device"`
	Inode        string `json:"inode"`
	Pathname     string `json:"pathname,omitempty"`
	Type         string `json:"type,omitempty"`
}

type MemMapOutput struct {
	PID          int            `json:"pid"`
	RssKB        int            `json:"rss_kb,omitempty"`
	PssKB        int            `json:"pss_kb,omitempty"`
	SharedClean  int            `json:"shared_clean_kb,omitempty"`
	SharedDirty  int            `json:"shared_dirty_kb,omitempty"`
	PrivateClean int            `json:"private_clean_kb,omitempty"`
	PrivateDirty int            `json:"private_dirty_kb,omitempty"`
	Anonymous    int            `json:"anonymous_kb,omitempty"`
	SwapKB       int            `json:"swap_kb,omitempty"`
	TotalRegions int            `json:"total_regions"`
	Regions      []MemoryRegion `json:"regions"`
}

// ---------------------------------------------------------------------------
// fd_list
// ---------------------------------------------------------------------------

type FdListInput struct {
	PID   int `json:"pid" jsonschema:"description=Process ID to inspect open file descriptors for,required"`
	Limit int `json:"limit,omitempty" jsonschema:"description=Max file descriptors to return. Default 100."`
}

type FdEntry struct {
	FD     string `json:"fd"`
	Target string `json:"target"`
	Type   string `json:"type,omitempty"`
}

type FdListOutput struct {
	PID   int       `json:"pid"`
	FDs   []FdEntry `json:"fds"`
	Total int       `json:"total"`
}

// ---------------------------------------------------------------------------
// net_connections
// ---------------------------------------------------------------------------

type NetConnectionsInput struct {
	PID   int    `json:"pid" jsonschema:"description=Process ID to inspect network connections for,required"`
	Proto string `json:"proto,omitempty" jsonschema:"description=Protocol filter: all (default)\\, tcp\\, or udp"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=Max connections to return. Default 50."`
}

type NetConnectionEntry struct {
	Proto      string `json:"proto"`
	LocalAddr  string `json:"local_addr"`
	LocalPort  int    `json:"local_port"`
	RemoteAddr string `json:"remote_addr"`
	RemotePort int    `json:"remote_port"`
	State      string `json:"state"`
	Inode      string `json:"inode,omitempty"`
}

type NetConnectionsOutput struct {
	PID         int                  `json:"pid"`
	Connections []NetConnectionEntry `json:"connections"`
	Total       int                  `json:"total"`
}

// ---------------------------------------------------------------------------
// investigate_port
// ---------------------------------------------------------------------------

type InvestigatePortInput struct {
	Port     int `json:"port" jsonschema:"required,description=TCP port number to investigate"`
	LogLines int `json:"log_lines,omitempty" jsonschema:"description=Number of journal log lines to fetch. Default 20."`
}

type InvestigatePortOutput struct {
	Port          int          `json:"port"`
	Process       *ProcessInfo `json:"process,omitempty"`
	Tree          string       `json:"tree,omitempty"`
	SystemdUnit   string       `json:"systemd_unit,omitempty"`
	SystemdStatus string       `json:"systemd_status,omitempty"`
	RecentLogs    string       `json:"recent_logs,omitempty"`
}

// ---------------------------------------------------------------------------
// investigate_service
// ---------------------------------------------------------------------------

type InvestigateServiceInput struct {
	Unit     string `json:"unit" jsonschema:"required,description=Systemd unit name to investigate"`
	System   bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
	LogLines int    `json:"log_lines,omitempty" jsonschema:"description=Number of journal log lines to fetch. Default 20."`
}

type InvestigateServiceOutput struct {
	Unit        string       `json:"unit"`
	ActiveState string       `json:"active_state"`
	SubState    string       `json:"sub_state"`
	MainPID     int          `json:"main_pid,omitempty"`
	Process     *ProcessInfo `json:"process,omitempty"`
	Ports       []PortEntry  `json:"ports,omitempty"`
	RecentLogs  string       `json:"recent_logs,omitempty"`
}

// ---------------------------------------------------------------------------
// investigate_memory
// ---------------------------------------------------------------------------

type InvestigateMemoryInput struct {
	TopCount    int `json:"top_count,omitempty" jsonschema:"description=Number of top memory-consuming processes to inspect. Default 10\\, max 50."`
	ThresholdMB int `json:"threshold_mb,omitempty" jsonschema:"description=Filter processes with RSS greater than or equal to this threshold in MB. Default 0."`
}

type SystemMemorySummary struct {
	TotalMB     int     `json:"total_mb"`
	AvailableMB int     `json:"available_mb"`
	UsedMB      int     `json:"used_mb"`
	FreeMB      int     `json:"free_mb"`
	BuffersMB   int     `json:"buffers_mb"`
	CachedMB    int     `json:"cached_mb"`
	SwapTotalMB int     `json:"swap_total_mb"`
	SwapUsedMB  int     `json:"swap_used_mb"`
	SwapFreeMB  int     `json:"swap_free_mb"`
	UsedPercent float64 `json:"used_percent"`
	SwapPercent float64 `json:"swap_percent"`
}

type ProcessMemoryDetail struct {
	PID          int     `json:"pid"`
	User         string  `json:"user"`
	Command      string  `json:"command"`
	CPUPercent   float64 `json:"cpu_percent"`
	MemPercent   float64 `json:"mem_percent"`
	RssMB        float64 `json:"rss_mb"`
	VszMB        float64 `json:"vsz_mb"`
	PssMB        float64 `json:"pss_mb,omitempty"`
	SharedClean  float64 `json:"shared_clean_mb,omitempty"`
	SharedDirty  float64 `json:"shared_dirty_mb,omitempty"`
	PrivateClean float64 `json:"private_clean_mb,omitempty"`
	PrivateDirty float64 `json:"private_dirty_mb,omitempty"`
	SwapMB       float64 `json:"swap_mb,omitempty"`
	OOMScore     int     `json:"oom_score"`
	OOMScoreAdj  int     `json:"oom_score_adj"`
}

type OOMRiskAssessment struct {
	RiskLevel        string  `json:"risk_level"`
	AvailablePercent float64 `json:"available_percent"`
	SwapPressure     string  `json:"swap_pressure"`
	TopOOMCandidates []int   `json:"top_oom_candidates,omitempty"`
	Summary          string  `json:"summary"`
}

type InvestigateMemoryOutput struct {
	SystemMemory SystemMemorySummary   `json:"system_memory"`
	TopProcesses []ProcessMemoryDetail `json:"top_processes"`
	OOMRisk      OOMRiskAssessment     `json:"oom_risk"`
	ProcessCount int                   `json:"process_count"`
}

// ---------------------------------------------------------------------------
// investigate_cpu
// ---------------------------------------------------------------------------

type InvestigateCpuInput struct {
	TopCount     int     `json:"top_count,omitempty" jsonschema:"description=Number of top CPU-consuming processes to inspect. Default 10\\, max 50."`
	ThresholdCPU float64 `json:"threshold_cpu,omitempty" jsonschema:"description=Filter processes with CPU% greater than or equal to this threshold. Default 0."`
}

type SystemCpuSummary struct {
	Load1          float64 `json:"load1"`
	Load5          float64 `json:"load5"`
	Load15         float64 `json:"load15"`
	NumCPU         int     `json:"num_cpu"`
	LoadPerCore    float64 `json:"load_per_core"`
	SaturationRisk string  `json:"saturation_risk"`
}

type ProcessCpuDetail struct {
	PID        int     `json:"pid"`
	User       string  `json:"user"`
	Command    string  `json:"command"`
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float64 `json:"mem_percent"`
	State      string  `json:"state"`
	Threads    int     `json:"threads,omitempty"`
	CGroup     string  `json:"cgroup,omitempty"`
}

type InvestigateCpuOutput struct {
	SystemCPU    SystemCpuSummary   `json:"system_cpu"`
	TopProcesses []ProcessCpuDetail `json:"top_processes"`
	ProcessCount int                `json:"process_count"`
	Diagnosis    string             `json:"diagnosis"`
}

// ---------------------------------------------------------------------------
// ps_search
// ---------------------------------------------------------------------------

type PsSearchInput struct {
	Query    string `json:"query,omitempty" jsonschema:"description=General search keyword matching command\\, args\\, or executable"`
	OpenFile string `json:"open_file,omitempty" jsonschema:"description=Find processes with an open file descriptor matching this path substring"`
	Env      string `json:"env,omitempty" jsonschema:"description=Find processes with an environment variable matching KEY or KEY=VALUE"`
	Port     int    `json:"port,omitempty" jsonschema:"description=Find processes listening on or connected to this TCP/UDP port"`
	User     string `json:"user,omitempty" jsonschema:"description=Filter processes by username"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=Max processes to return. Default 20."`
}

type ProcessSearchMatch struct {
	PID        int      `json:"pid"`
	User       string   `json:"user"`
	Command    string   `json:"command"`
	MatchedOn  []string `json:"matched_on"`
	CPUPercent float64  `json:"cpu_percent"`
	MemPercent float64  `json:"mem_percent"`
	State      string   `json:"state"`
}

type PsSearchOutput struct {
	Matches     []ProcessSearchMatch `json:"matches"`
	Total       int                  `json:"total"`
	QueryNotice string               `json:"query_notice,omitempty"`
}

func searchProcesses(input PsSearchInput) (PsSearchOutput, error) {
	q := strings.TrimSpace(input.Query)
	openFile := strings.TrimSpace(input.OpenFile)
	envFilter := strings.TrimSpace(input.Env)
	userFilter := strings.TrimSpace(input.User)
	port := input.Port

	if q == "" && openFile == "" && envFilter == "" && userFilter == "" && port == 0 {
		return PsSearchOutput{}, fmt.Errorf("[%s] at least one search filter must be specified (query, open_file, env, port, or user)", handler.ErrInvalidParam)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	portPids := make(map[int]bool)
	if port > 0 {
		ssOut, _, err := runCmd("ss", "-tupnH")
		if err != nil {
			ssOut, _, _ = runCmd("ss", "-tupn")
		}
		for _, line := range strings.Split(ssOut, "\n") {
			if strings.Contains(line, fmt.Sprintf(":%d", port)) {
				if pid := extractPidFromSsLine(line); pid > 0 {
					portPids[pid] = true
				}
			}
		}
	}

	psOut, _, err := runCmd("ps", "aux")
	if err != nil {
		return PsSearchOutput{}, fmt.Errorf("[%s] ps command failed: %w", handler.ErrAPIError, err)
	}

	lines := strings.Split(psOut, "\n")
	var matches []ProcessSearchMatch

	for i, line := range lines {
		if i == 0 {
			continue
		}
		p, ok := parsePsLine(line)
		if !ok {
			continue
		}

		if userFilter != "" && !strings.EqualFold(p.User, userFilter) {
			continue
		}

		var matchedOn []string

		if port > 0 {
			if portPids[p.PID] {
				matchedOn = append(matchedOn, fmt.Sprintf("port: %d", port))
			} else {
				continue
			}
		}

		if q != "" {
			if strings.Contains(strings.ToLower(p.Command), strings.ToLower(q)) {
				matchedOn = append(matchedOn, fmt.Sprintf("query: %s", q))
			} else if openFile == "" && envFilter == "" {
				continue
			}
		}

		if openFile != "" {
			fdDir := fmt.Sprintf("/proc/%d/fd", p.PID)
			entries, err := os.ReadDir(fdDir)
			if err == nil {
				matchedFile := false
				for _, e := range entries {
					link, err := os.Readlink(filepath.Join(fdDir, e.Name()))
					if err == nil && strings.Contains(strings.ToLower(link), strings.ToLower(openFile)) {
						matchedOn = append(matchedOn, fmt.Sprintf("open_file: %s", link))
						matchedFile = true
						break
					}
				}
				if !matchedFile && q == "" && envFilter == "" {
					continue
				}
			} else if q == "" && envFilter == "" {
				continue
			}
		}

		if envFilter != "" {
			envData, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", p.PID))
			if err == nil {
				matchedEnv := false
				for part := range strings.SplitSeq(string(envData), "\x00") {
					isolated := IsolateProcessEnv(part)
					if strings.Contains(strings.ToLower(isolated), strings.ToLower(envFilter)) {
						matchedOn = append(matchedOn, fmt.Sprintf("env: %s", isolated))
						matchedEnv = true
						break
					}
				}
				if !matchedEnv && q == "" && openFile == "" {
					continue
				}
			} else if q == "" && openFile == "" {
				continue
			}
		}

		if len(matchedOn) > 0 {
			matches = append(matches, ProcessSearchMatch{
				PID:        p.PID,
				User:       p.User,
				Command:    p.Command,
				MatchedOn:  matchedOn,
				CPUPercent: p.CPU,
				MemPercent: p.Mem,
				State:      p.Stat,
			})
			if len(matches) >= limit {
				break
			}
		}
	}

	notice := ""
	if len(matches) == 0 {
		notice = "No processes matched the specified filter criteria."
	}

	return PsSearchOutput{
		Matches:     matches,
		Total:       len(matches),
		QueryNotice: notice,
	}, nil
}

// ---------------------------------------------------------------------------
// ProcessModule
// ---------------------------------------------------------------------------

type ProcessModule struct{}

func (m *ProcessModule) Name() string { return "process" }
func (m *ProcessModule) Description() string {
	return "Linux process management, ports, GPU, and system info"
}

func (m *ProcessModule) Tools() []registry.ToolDefinition {
	psList := handler.TypedHandler[PsListInput, PsListOutput](
		"ps_list",
		"Call this when you need to inspect running processes, check CPU or memory hogs, or filter active tasks by command substring. Returns top processes sorted by CPU, memory, or PID.",
		func(_ context.Context, input PsListInput) (PsListOutput, error) {
			sortFlag := "--sort=-pcpu"
			switch input.SortBy {
			case "mem":
				sortFlag = "--sort=-pmem"
			case "pid":
				sortFlag = "--sort=pid"
			case "cpu", "":
			default:
				return PsListOutput{}, fmt.Errorf("[%s] sort_by must be cpu, mem, or pid", handler.ErrInvalidParam)
			}

			limit := input.Limit
			if limit <= 0 {
				limit = 20
			}

			out, _, err := runCmd("ps", "aux", sortFlag)
			if err != nil {
				return PsListOutput{}, fmt.Errorf("[%s] ps command failed: %w", handler.ErrAPIError, err)
			}

			lines := strings.Split(out, "\n")
			var processes []ProcessInfo
			for i, line := range lines {
				if i == 0 {
					continue
				}
				p, ok := parsePsLine(line)
				if !ok {
					continue
				}
				if input.Filter != "" && !strings.Contains(strings.ToLower(p.Command), strings.ToLower(input.Filter)) {
					continue
				}
				processes = append(processes, p)
				if len(processes) >= limit {
					break
				}
			}

			if processes == nil {
				processes = []ProcessInfo{}
			}
			return PsListOutput{Processes: processes, Total: len(processes)}, nil
		},
	)
	psList.Category = "process"
	psList.SearchTerms = []string{"top processes", "running processes", "cpu usage", "memory usage"}

	psTree := handler.TypedHandler[PsTreeInput, PsTreeOutput](
		"ps_tree",
		"Call this when you need to view parent-child ancestry and the complete process tree for a specific PID using pstree (with ps --forest fallback).",
		func(_ context.Context, input PsTreeInput) (PsTreeOutput, error) {
			if input.PID <= 0 {
				return PsTreeOutput{}, fmt.Errorf("[%s] pid must be a positive integer", handler.ErrInvalidParam)
			}

			pidStr := strconv.Itoa(input.PID)
			out, _, err := runCmd("pstree", "-p", pidStr)
			if err == nil {
				return PsTreeOutput{PID: input.PID, Tree: out}, nil
			}

			out, _, err = runCmd("ps", "-ef", "--forest")
			if err != nil {
				return PsTreeOutput{}, fmt.Errorf("[%s] ps --forest failed: %w", handler.ErrAPIError, err)
			}

			var filtered []string
			for line := range strings.SplitSeq(out, "\n") {
				if strings.Contains(line, pidStr) {
					filtered = append(filtered, line)
				}
			}
			if len(filtered) == 0 {
				return PsTreeOutput{}, fmt.Errorf("[%s] process not found: pid %d", handler.ErrNotFound, input.PID)
			}

			return PsTreeOutput{PID: input.PID, Tree: strings.Join(filtered, "\n")}, nil
		},
	)
	psTree.Category = "process"
	psTree.MaxResultChars = 8000

	killProcess := handler.TypedHandler[KillProcessInput, KillProcessOutput](
		"kill_process",
		"Call this when you need to stop, terminate, interrupt, or signal a running process by PID. Supports TERM (default), KILL, HUP, INT, USR1, USR2, STOP, and CONT signals.",
		func(_ context.Context, input KillProcessInput) (KillProcessOutput, error) {
			if input.PID <= 0 {
				return KillProcessOutput{}, fmt.Errorf("[%s] pid must be a positive integer", handler.ErrInvalidParam)
			}

			sig := input.Signal
			if sig == "" {
				sig = "TERM"
			}
			sig = strings.ToUpper(sig)
			if !validSignals[sig] {
				return KillProcessOutput{}, fmt.Errorf("[%s] invalid signal %q; must be one of: TERM, KILL, HUP, INT, USR1, USR2, STOP, CONT", handler.ErrInvalidParam, sig)
			}

			// Host identity verification: if host is specified, it must match local hostname.
			if input.Host != "" {
				localHost, err := os.Hostname()
				if err != nil || input.Host != localHost {
					slog.Error("foreign host mismatch", "pid", input.PID, "expected_host", input.Host, "current_host", localHost)
					return KillProcessOutput{}, fmt.Errorf("[%s] foreign host mismatch: expected %q, current host %q", handler.ErrPermission, input.Host, localHost)
				}
			}

			// Boot ID identity verification: if boot_id is specified, it must match current system boot ID.
			if input.BootID != "" {
				currentBootID, err := readBootID()
				if err != nil || input.BootID != currentBootID {
					slog.Error("boot ID mismatch", "pid", input.PID, "expected_boot_id", input.BootID, "current_boot_id", currentBootID)
					return KillProcessOutput{}, fmt.Errorf("[%s] boot ID mismatch: expected %q, current %q", handler.ErrPermission, input.BootID, currentBootID)
				}
			}

			// Start time identity verification: if start_time > 0, verify against /proc/<pid>/stat field 22.
			if input.StartTime > 0 {
				currentStart, err := readProcessStartTicks(input.PID)
				if err != nil {
					if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
						slog.Error("process not found for start time check", "pid", input.PID)
						return KillProcessOutput{}, fmt.Errorf("[%s] process not found: pid %d", handler.ErrNotFound, input.PID)
					}
					return KillProcessOutput{}, fmt.Errorf("[%s] failed reading process start time: %w", handler.ErrAPIError, err)
				}
				if input.StartTime != currentStart {
					slog.Error("PID reuse detected", "pid", input.PID, "expected_start_time", input.StartTime, "current_start_time", currentStart)
					return KillProcessOutput{}, fmt.Errorf("[%s] PID reuse detected: start time mismatch for pid %d (expected %d, current %d)", handler.ErrPermission, input.PID, input.StartTime, currentStart)
				}
			}

			// Comm identity verification: if expected_comm is specified, verify against /proc/<pid>/comm.
			if input.ExpectedComm != "" {
				comm, err := readProcessComm(input.PID)
				if err != nil {
					if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
						slog.Error("process not found for comm check", "pid", input.PID)
						return KillProcessOutput{}, fmt.Errorf("[%s] process not found: pid %d", handler.ErrNotFound, input.PID)
					}
					return KillProcessOutput{}, fmt.Errorf("[%s] failed reading process comm: %w", handler.ErrAPIError, err)
				}
				if input.ExpectedComm != comm {
					slog.Error("comm mismatch", "pid", input.PID, "expected_comm", input.ExpectedComm, "current_comm", comm)
					return KillProcessOutput{}, fmt.Errorf("[%s] comm mismatch: expected %q, current %q", handler.ErrPermission, input.ExpectedComm, comm)
				}
			}

			if input.PID == 1 && !input.Confirm {
				slog.Error("permission denied: refusing to signal init/systemd without confirm", "pid", input.PID)
				return KillProcessOutput{}, fmt.Errorf("[%s] permission denied: refusing to signal critical system process: pid 1 without confirm: true", handler.ErrPermission)
			}
			if input.PID == os.Getpid() && !input.Confirm {
				slog.Error("refusing to signal self without confirm", "pid", input.PID)
				return KillProcessOutput{}, fmt.Errorf("[%s] refusing to signal process-mcp server process: pid %d without confirm: true", handler.ErrPermission, input.PID)
			}

			if isCrit, comm := isCriticalProcess(input.PID); isCrit && !input.Confirm {
				slog.Error("critical system process requires confirm", "pid", input.PID, "process", comm)
				return KillProcessOutput{}, fmt.Errorf("[%s] signaling critical process %q (pid %d) requires confirm: true", handler.ErrPermission, comm, input.PID)
			}

			slog.Info("sending signal", "pid", input.PID, "signal", sig)
			pidStr := strconv.Itoa(input.PID)
			_, stderr, err := runCmd("kill", "-"+sig, pidStr)
			if err != nil {
				if strings.Contains(stderr, "No such process") {
					slog.Error("process not found", "pid", input.PID, "signal", sig)
					return KillProcessOutput{}, fmt.Errorf("[%s] process not found: pid %d", handler.ErrNotFound, input.PID)
				}
				if strings.Contains(stderr, "Operation not permitted") {
					slog.Error("permission denied", "pid", input.PID, "signal", sig)
					return KillProcessOutput{}, fmt.Errorf("[%s] permission denied: pid %d", handler.ErrPermission, input.PID)
				}
				slog.Error("kill failed", "pid", input.PID, "signal", sig, "error", stderr)
				return KillProcessOutput{}, fmt.Errorf("[%s] kill failed: %s", handler.ErrAPIError, stderr)
			}

			slog.Info("signal sent", "pid", input.PID, "signal", sig)
			return KillProcessOutput{
				PID:    input.PID,
				Signal: sig,
				Result: fmt.Sprintf("sent %s to pid %d", sig, input.PID),
			}, nil
		},
	)
	killProcess.Category = "process"
	killProcess.IsWrite = true

	portList := handler.TypedHandler[PortListInput, PortListOutput](
		"port_list",
		"Call this when you need to find open or listening TCP ports, check socket bindings, or identify which process owns a specific port number via ss.",
		func(_ context.Context, input PortListInput) (PortListOutput, error) {
			out, _, err := runCmd("ss", "-tlnp")
			if err != nil {
				return PortListOutput{}, fmt.Errorf("[%s] ss command failed: %w", handler.ErrAPIError, err)
			}

			ports := parseSsLines(out, input.Port)
			if ports == nil {
				ports = []PortEntry{}
			}
			return PortListOutput{Ports: ports, Total: len(ports)}, nil
		},
	)
	portList.Category = "network"
	portList.SearchTerms = []string{"open ports", "listening ports", "network sockets"}

	gpuStatus := handler.TypedHandler[GpuStatusInput, GpuStatusOutput](
		"gpu_status",
		"Call this when you need GPU telemetry, VRAM utilization, GPU process allocation, temperature, or driver status via nvidia-smi (NVIDIA) or rocm-smi (AMD).",
		func(_ context.Context, input GpuStatusInput) (GpuStatusOutput, error) {
			hasNvidia := false
			if _, err := exec.LookPath("nvidia-smi"); err == nil {
				hasNvidia = true
			}
			hasRocm := false
			if _, err := exec.LookPath("rocm-smi"); err == nil {
				hasRocm = true
			}

			if !hasNvidia && !hasRocm {
				return GpuStatusOutput{}, fmt.Errorf("[%s] no GPU management utility found: neither nvidia-smi nor rocm-smi is installed", handler.ErrNotFound)
			}

			var result GpuStatusOutput

			if hasNvidia {
				out, stderr, err := runCmd("nvidia-smi",
					"--query-gpu=driver_version,name,temperature.gpu,utilization.gpu,memory.used,memory.total,power.draw",
					"--format=csv,noheader,nounits")
				if err != nil {
					return GpuStatusOutput{}, fmt.Errorf("[%s] nvidia-smi query failed: %s", handler.ErrAPIError, stderr)
				}

				result.GPU = parseGpuInfo(out)

				if input.IncludeProcesses == nil || *input.IncludeProcesses {
					out, _, err = runCmd("nvidia-smi", "--query-compute-apps=pid,name,used_memory", "--format=csv,noheader,nounits")
					if err == nil && out != "" {
						for line := range strings.SplitSeq(out, "\n") {
							if gp, ok := parseGpuProcessLine(line); ok {
								result.Processes = append(result.Processes, gp)
							}
						}
					}
				}
			} else if hasRocm {
				out, stderr, err := runCmd("rocm-smi", "--showid", "--showdriverversion", "--showproductname", "--showtemp", "--showuse", "--showmeminfo", "vram", "--showpower", "--json")
				if err != nil {
					return GpuStatusOutput{}, fmt.Errorf("[%s] rocm-smi query failed: %s", handler.ErrAPIError, stderr)
				}

				gpu, procs := parseRocmGpuInfo([]byte(out))
				result.GPU = gpu
				if input.IncludeProcesses == nil || *input.IncludeProcesses {
					result.Processes = procs
				}
			}

			if result.Processes == nil {
				result.Processes = []GpuProcess{}
			}
			return result, nil
		},
	)
	gpuStatus.Category = "gpu"
	gpuStatus.SearchTerms = []string{"nvidia", "amd", "rocm", "gpu usage", "gpu processes", "cuda processes", "vram"}

	systemInfo := handler.TypedHandler[SystemInfoInput, SystemInfoOutput](
		"system_info",
		"Call this when you need host system telemetry, including hostname, Linux kernel version, uptime, load averages, CPU count, memory, and swap status.",
		func(_ context.Context, input SystemInfoInput) (SystemInfoOutput, error) {
			var info SystemInfoOutput
			info.Hostname, _ = os.Hostname()

			if ver, err := readProcFile("version"); err == nil {
				info.Kernel = parseKernelVersion(ver)
			}
			if raw, err := readProcFile("uptime"); err == nil {
				info.Uptime = parseUptime(raw)
			}
			if raw, err := readProcFile("loadavg"); err == nil {
				info.LoadAvg = parseLoadAvg(raw)
			}
			if raw, err := readProcFile("cpuinfo"); err == nil {
				info.CPUCount = parseCpuCount(raw)
			}
			if raw, err := readProcFile("meminfo"); err == nil {
				memTotal, memAvail, swapTotal, swapFree := parseMemInfo(raw)
				info.MemTotalMB = memTotal / 1024
				info.MemAvailMB = memAvail / 1024
				info.SwapTotalMB = swapTotal / 1024
				info.SwapUsedMB = (swapTotal - swapFree) / 1024
			}
			if input.IncludeCGroup == nil || *input.IncludeCGroup {
				if raw, err := readProcFile("self/cgroup"); err == nil {
					info.CGroup = strings.TrimSpace(raw)
				}
			}
			if nsEntries, err := os.ReadDir("/proc/self/ns"); err == nil {
				info.Namespaces = make(map[string]string)
				for _, e := range nsEntries {
					if link, err := os.Readlink("/proc/self/ns/" + e.Name()); err == nil {
						info.Namespaces[e.Name()] = link
					}
				}
			}

			return info, nil
		},
	)
	systemInfo.Category = "system"

	investigatePort := handler.TypedHandler[InvestigatePortInput, InvestigatePortOutput](
		"investigate_port",
		"Call this when you need a single-shot diagnosis of a TCP port. Composed investigation identifies the listening process, inspects its tree, checks systemd unit status, and fetches recent journal logs.",
		func(_ context.Context, input InvestigatePortInput) (InvestigatePortOutput, error) {
			if input.Port <= 0 || input.Port > 65535 {
				return InvestigatePortOutput{}, fmt.Errorf("[%s] port must be 1-65535", handler.ErrInvalidParam)
			}
			logLines := input.LogLines
			if logLines <= 0 {
				logLines = 20
			}

			result := InvestigatePortOutput{Port: input.Port}
			ssOut, _, _ := runCmd("ss", "-tlnp", fmt.Sprintf("sport = :%d", input.Port))
			var pid int
			for line := range strings.SplitSeq(ssOut, "\n") {
				if !strings.Contains(line, "LISTEN") {
					continue
				}
				if p := extractPidFromSsLine(line); p > 0 {
					pid = p
					break
				}
			}
			if pid == 0 {
				return result, fmt.Errorf("[%s] no process listening on port %d", handler.ErrNotFound, input.Port)
			}

			psOut, _, _ := runCmd("ps", "-p", strconv.Itoa(pid), "-o", "user,pid,pcpu,pmem,vsz,rss,tty,stat,start,time,command", "--no-headers")
			if psOut != "" {
				if p, ok := parsePsLine(psOut); ok {
					result.Process = &p
				}
			}

			treeOut, _, err := runCmd("pstree", "-p", strconv.Itoa(pid))
			if err == nil {
				result.Tree = treeOut
			}

			unitOut, _, _ := runCmd("systemctl", "--user", "status", strconv.Itoa(pid))
			if unitOut != "" {
				result.SystemdUnit = extractSystemdUnit(unitOut)
				result.SystemdStatus = unitOut
			}
			if result.SystemdUnit != "" && isValidUnitName(result.SystemdUnit) {
				logsOut, _, _ := runCmd("journalctl", "--user-unit", result.SystemdUnit, "-n", strconv.Itoa(logLines), "--no-pager")
				result.RecentLogs = logsOut
			}

			return result, nil
		},
	)
	investigatePort.Category = "investigation"
	investigatePort.MaxResultChars = 9000
	investigatePort.SearchTerms = []string{"debug port", "what is using this port", "port investigation"}

	investigateService := handler.TypedHandler[InvestigateServiceInput, InvestigateServiceOutput](
		"investigate_service",
		"Call this when you need a single-shot diagnosis of a systemd service. Composed investigation inspects unit status, maps running processes, detects listening TCP ports, and fetches recent journal logs.",
		func(_ context.Context, input InvestigateServiceInput) (InvestigateServiceOutput, error) {
			if input.Unit == "" {
				return InvestigateServiceOutput{}, fmt.Errorf("[%s] unit is required", handler.ErrInvalidParam)
			}
			if !isValidUnitName(input.Unit) {
				return InvestigateServiceOutput{}, fmt.Errorf("[%s] invalid unit name %q", handler.ErrInvalidParam, input.Unit)
			}
			logLines := input.LogLines
			if logLines <= 0 {
				logLines = 20
			}

			scope := "--user"
			journalFlag := "--user-unit"
			if input.System {
				scope = ""
				journalFlag = "-u"
			}

			result := InvestigateServiceOutput{Unit: input.Unit}
			var statusArgs []string
			if scope != "" {
				statusArgs = append(statusArgs, scope)
			}
			statusArgs = append(statusArgs, "show", "--property=ActiveState,SubState,MainPID", input.Unit)
			statusOut, statusErrOut, statusErr := runCmd("systemctl", statusArgs...)
			if !input.System && statusErr != nil &&
				(strings.Contains(statusErrOut, "Failed to connect to user scope bus") ||
					strings.Contains(statusErrOut, "$DBUS_SESSION_BUS_ADDRESS") ||
					strings.Contains(statusErrOut, "$XDG_RUNTIME_DIR")) {
				statusOut, _, _ = runCmd("systemctl", "show", "--property=ActiveState,SubState,MainPID", input.Unit)
				journalFlag = "-u"
			}
			props := parseSystemdProperties(statusOut)
			result.ActiveState = props["ActiveState"]
			result.SubState = props["SubState"]
			result.MainPID, _ = strconv.Atoi(props["MainPID"])

			if result.MainPID > 0 {
				psOut, _, _ := runCmd("ps", "-p", strconv.Itoa(result.MainPID), "-o", "user,pid,pcpu,pmem,vsz,rss,tty,stat,start,time,command", "--no-headers")
				if psOut != "" {
					if p, ok := parsePsLine(psOut); ok {
						result.Process = &p
					}
				}

				ssOut, _, _ := runCmd("ss", "-tlnp")
				result.Ports = filterSsPortsByPid(ssOut, strconv.Itoa(result.MainPID))
			}

			if result.Ports == nil {
				result.Ports = []PortEntry{}
			}

			logsOut, _, _ := runCmd("journalctl", journalFlag, input.Unit, "-n", strconv.Itoa(logLines), "--no-pager")
			result.RecentLogs = logsOut
			return result, nil
		},
	)
	investigateService.Category = "investigation"
	investigateService.MaxResultChars = 9000
	investigateService.SearchTerms = []string{"debug service", "service investigation", "why is my service failing"}

	fdList := handler.TypedHandler[FdListInput, FdListOutput](
		"fd_list",
		"Call this when you need to inspect open file descriptors for a specific PID, including open files, sockets, pipes, and device handles via /proc/PID/fd.",
		func(_ context.Context, input FdListInput) (FdListOutput, error) {
			if input.PID <= 0 {
				return FdListOutput{}, fmt.Errorf("[%s] pid must be greater than 0", handler.ErrInvalidParam)
			}
			limit := input.Limit
			if limit <= 0 {
				limit = 100
			}

			procPath := fmt.Sprintf("/proc/%d", input.PID)
			if _, err := os.Stat(procPath); os.IsNotExist(err) {
				return FdListOutput{}, fmt.Errorf("[%s] process %d not found", handler.ErrNotFound, input.PID)
			}

			fdDir := fmt.Sprintf("/proc/%d/fd", input.PID)
			entries, err := os.ReadDir(fdDir)
			if err != nil {
				if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
					return FdListOutput{}, fmt.Errorf("[%s] permission denied reading /proc/%d/fd", handler.ErrPermission, input.PID)
				}
				if os.IsNotExist(err) {
					return FdListOutput{}, fmt.Errorf("[%s] process %d not found", handler.ErrNotFound, input.PID)
				}
				return FdListOutput{}, fmt.Errorf("[%s] failed to read /proc/%d/fd: %w", handler.ErrAPIError, input.PID, err)
			}

			type parsedFD struct {
				num   int
				entry FdEntry
			}
			var parsed []parsedFD

			for _, e := range entries {
				fdName := e.Name()
				num, err := strconv.Atoi(fdName)
				if err != nil {
					continue
				}
				linkTarget, err := os.Readlink(filepath.Join(fdDir, fdName))
				if err != nil {
					linkTarget = "(unknown)"
				}
				fdType := "file"
				if strings.HasPrefix(linkTarget, "socket:") {
					fdType = "socket"
				} else if strings.HasPrefix(linkTarget, "pipe:") {
					fdType = "pipe"
				} else if strings.HasPrefix(linkTarget, "anon_inode:") {
					fdType = "anon_inode"
				} else if strings.HasPrefix(linkTarget, "/dev/") {
					fdType = "device"
				}

				parsed = append(parsed, parsedFD{
					num: num,
					entry: FdEntry{
						FD:     fdName,
						Target: linkTarget,
						Type:   fdType,
					},
				})
			}

			sort.Slice(parsed, func(i, j int) bool {
				return parsed[i].num < parsed[j].num
			})

			total := len(parsed)
			if limit > total {
				limit = total
			}
			fds := make([]FdEntry, 0, limit)
			for i := 0; i < limit; i++ {
				fds = append(fds, parsed[i].entry)
			}

			return FdListOutput{
				PID:   input.PID,
				FDs:   fds,
				Total: total,
			}, nil
		},
	)
	fdList.Category = "inspection"
	fdList.SearchTerms = []string{"open files", "file descriptors", "proc fd", "socket handles"}

	memMap := handler.TypedHandler[MemMapInput, MemMapOutput](
		"mem_map",
		"Call this when you need detailed memory mapping regions, RSS/PSS breakdown, shared vs private memory, or virtual address layout for a process via /proc/PID/maps and smaps_rollup.",
		func(_ context.Context, input MemMapInput) (MemMapOutput, error) {
			if input.PID <= 0 {
				return MemMapOutput{}, fmt.Errorf("[%s] pid must be greater than 0", handler.ErrInvalidParam)
			}
			limit := input.Limit
			if limit <= 0 {
				limit = 50
			}
			if limit > 200 {
				limit = 200
			}

			procPath := fmt.Sprintf("/proc/%d", input.PID)
			if _, err := os.Stat(procPath); os.IsNotExist(err) {
				return MemMapOutput{}, fmt.Errorf("[%s] process %d not found", handler.ErrNotFound, input.PID)
			}

			out := MemMapOutput{PID: input.PID}

			if smaps, err := os.ReadFile(filepath.Join(procPath, "smaps_rollup")); err == nil {
				for line := range strings.SplitSeq(string(smaps), "\n") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						val, _ := strconv.Atoi(parts[1])
						switch parts[0] {
						case "Rss:":
							out.RssKB = val
						case "Pss:":
							out.PssKB = val
						case "Shared_Clean:":
							out.SharedClean = val
						case "Shared_Dirty:":
							out.SharedDirty = val
						case "Private_Clean:":
							out.PrivateClean = val
						case "Private_Dirty:":
							out.PrivateDirty = val
						case "Anonymous:":
							out.Anonymous = val
						case "Swap:":
							out.SwapKB = val
						}
					}
				}
			}

			mapsData, err := os.ReadFile(filepath.Join(procPath, "maps"))
			if err != nil {
				if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
					return MemMapOutput{}, fmt.Errorf("[%s] permission denied reading /proc/%d/maps", handler.ErrPermission, input.PID)
				}
				return MemMapOutput{}, fmt.Errorf("[%s] failed to read /proc/%d/maps: %w", handler.ErrAPIError, input.PID, err)
			}

			var regions []MemoryRegion
			for line := range strings.SplitSeq(string(mapsData), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) < 5 {
					continue
				}
				reg := MemoryRegion{
					AddressRange: fields[0],
					Permissions:  fields[1],
					Offset:       fields[2],
					Device:       fields[3],
					Inode:        fields[4],
				}
				if len(fields) >= 6 {
					reg.Pathname = strings.Join(fields[5:], " ")
				}
				switch {
				case strings.HasPrefix(reg.Pathname, "[heap]"):
					reg.Type = "heap"
				case strings.HasPrefix(reg.Pathname, "[stack"):
					reg.Type = "stack"
				case reg.Pathname == "" || strings.HasPrefix(reg.Pathname, "[anon"):
					reg.Type = "anon"
				case strings.Contains(reg.Pathname, ".so"):
					reg.Type = "library"
				default:
					reg.Type = "file"
				}
				regions = append(regions, reg)
			}

			out.TotalRegions = len(regions)
			if limit > len(regions) {
				limit = len(regions)
			}
			out.Regions = regions[:limit]

			return out, nil
		},
	)
	memMap.Category = "inspection"
	memMap.SearchTerms = []string{"memory map", "smaps", "proc maps", "address space", "vma"}
	memMap.MaxResultChars = 9000

	netConnections := handler.TypedHandler[NetConnectionsInput, NetConnectionsOutput](
		"net_connections",
		"Call this when you need to inspect network connections (TCP/UDP sockets, local/remote addresses, ports, and connection states) for a specific process PID.",
		func(_ context.Context, input NetConnectionsInput) (NetConnectionsOutput, error) {
			if input.PID <= 0 {
				return NetConnectionsOutput{}, fmt.Errorf("[%s] pid must be a positive integer", handler.ErrInvalidParam)
			}
			limit := input.Limit
			if limit <= 0 {
				limit = 50
			}

			procPath := fmt.Sprintf("/proc/%d", input.PID)
			if _, err := os.Stat(procPath); err != nil {
				if os.IsNotExist(err) {
					return NetConnectionsOutput{}, fmt.Errorf("[%s] process not found: pid %d", handler.ErrNotFound, input.PID)
				}
				if os.IsPermission(err) {
					return NetConnectionsOutput{}, fmt.Errorf("[%s] permission denied accessing /proc/%d", handler.ErrPermission, input.PID)
				}
				return NetConnectionsOutput{}, fmt.Errorf("[%s] stat /proc/%d: %w", handler.ErrAPIError, input.PID, err)
			}

			out, _, err := runCmd("ss", "-tupnH")
			if err != nil {
				out, _, err = runCmd("ss", "-tupn")
				if err != nil {
					return NetConnectionsOutput{}, fmt.Errorf("[%s] ss command failed: %w", handler.ErrAPIError, err)
				}
			}

			conns := parseNetConnections(out, input.PID, input.Proto)
			total := len(conns)
			if len(conns) > limit {
				conns = conns[:limit]
			}
			if conns == nil {
				conns = []NetConnectionEntry{}
			}

			return NetConnectionsOutput{
				PID:         input.PID,
				Connections: conns,
				Total:       total,
			}, nil
		},
	)
	netConnections.Category = "network"
	netConnections.SearchTerms = []string{"network connections", "process sockets", "tcp connections", "udp connections", "open sockets"}
	netConnections.MaxResultChars = 9000

	investigateMemory := handler.TypedHandler[InvestigateMemoryInput, InvestigateMemoryOutput](
		"investigate_memory",
		"Call this when you need a single-shot diagnosis of system and process memory. Composed investigation checks total/available RAM, swap pressure, top memory consumers, PSS/shared/private breakdown, and Linux OOM killer risk.",
		func(_ context.Context, input InvestigateMemoryInput) (InvestigateMemoryOutput, error) {
			topCount := input.TopCount
			if topCount <= 0 {
				topCount = 10
			}
			if topCount > 50 {
				topCount = 50
			}

			rawMem, err := readProcFile("meminfo")
			if err != nil {
				return InvestigateMemoryOutput{}, fmt.Errorf("[%s] failed to read /proc/meminfo: %w", handler.ErrAPIError, err)
			}
			memInfo := parseDetailedMemInfo(rawMem)

			var usedPct, swapPct float64
			if memInfo.TotalKB > 0 {
				usedPct = float64(memInfo.UsedKB) / float64(memInfo.TotalKB) * 100.0
			}
			if memInfo.SwapTotalKB > 0 {
				swapPct = float64(memInfo.SwapUsedKB) / float64(memInfo.SwapTotalKB) * 100.0
			}

			sysMem := SystemMemorySummary{
				TotalMB:     memInfo.TotalKB / 1024,
				AvailableMB: memInfo.AvailKB / 1024,
				UsedMB:      memInfo.UsedKB / 1024,
				FreeMB:      memInfo.FreeKB / 1024,
				BuffersMB:   memInfo.BuffersKB / 1024,
				CachedMB:    memInfo.CachedKB / 1024,
				SwapTotalMB: memInfo.SwapTotalKB / 1024,
				SwapUsedMB:  memInfo.SwapUsedKB / 1024,
				SwapFreeMB:  memInfo.SwapFreeKB / 1024,
				UsedPercent: math.Round(usedPct*100) / 100,
				SwapPercent: math.Round(swapPct*100) / 100,
			}

			psOut, _, err := runCmd("ps", "aux", "--sort=-rss")
			if err != nil {
				return InvestigateMemoryOutput{}, fmt.Errorf("[%s] ps command failed: %w", handler.ErrAPIError, err)
			}

			lines := strings.Split(psOut, "\n")
			var details []ProcessMemoryDetail
			for i, line := range lines {
				if i == 0 {
					continue
				}
				p, ok := parsePsLine(line)
				if !ok {
					continue
				}
				rssMB := float64(p.RSS) / 1024.0
				if input.ThresholdMB > 0 && rssMB < float64(input.ThresholdMB) {
					continue
				}

				d := ProcessMemoryDetail{
					PID:        p.PID,
					User:       p.User,
					Command:    p.Command,
					CPUPercent: p.CPU,
					MemPercent: p.Mem,
					RssMB:      math.Round(rssMB*100) / 100,
					VszMB:      math.Round(float64(p.VSZ)/1024.0*100) / 100,
				}

				pssKB, scKB, sdKB, pcKB, pdKB, swapKB, ok := parseProcessSmapsRollup(p.PID)
				if ok {
					d.PssMB = math.Round(float64(pssKB)/1024.0*100) / 100
					d.SharedClean = math.Round(float64(scKB)/1024.0*100) / 100
					d.SharedDirty = math.Round(float64(sdKB)/1024.0*100) / 100
					d.PrivateClean = math.Round(float64(pcKB)/1024.0*100) / 100
					d.PrivateDirty = math.Round(float64(pdKB)/1024.0*100) / 100
					d.SwapMB = math.Round(float64(swapKB)/1024.0*100) / 100
				}
				d.OOMScore, d.OOMScoreAdj = parseOOMScore(p.PID)

				details = append(details, d)
				if len(details) >= topCount {
					break
				}
			}

			if details == nil {
				details = []ProcessMemoryDetail{}
			}

			var availPct float64
			if memInfo.TotalKB > 0 {
				availPct = float64(memInfo.AvailKB) / float64(memInfo.TotalKB) * 100.0
			}
			availPct = math.Round(availPct*100) / 100

			var riskLevel string
			switch {
			case availPct < 5.0:
				riskLevel = "critical"
			case availPct < 15.0:
				riskLevel = "high"
			case availPct < 30.0:
				riskLevel = "moderate"
			default:
				riskLevel = "low"
			}

			var swapPressure string
			switch {
			case swapPct > 85.0:
				swapPressure = "critical"
			case swapPct > 60.0:
				swapPressure = "high"
			case swapPct > 20.0:
				swapPressure = "moderate"
			default:
				swapPressure = "none"
			}

			if (swapPressure == "critical" || swapPressure == "high") && riskLevel != "critical" {
				riskLevel = "high"
			}

			candidates := make([]ProcessMemoryDetail, len(details))
			copy(candidates, details)
			sort.Slice(candidates, func(i, j int) bool {
				return candidates[i].OOMScore > candidates[j].OOMScore
			})

			var topCandidates []int
			for i := 0; i < len(candidates) && i < 3; i++ {
				if candidates[i].OOMScore > 0 {
					topCandidates = append(topCandidates, candidates[i].PID)
				}
			}
			if topCandidates == nil {
				topCandidates = []int{}
			}

			var summary string
			if riskLevel == "low" {
				summary = fmt.Sprintf("Memory pressure low: %.1f%% available (%d MB free of %d MB), swap pressure %s (%.1f%% used). No immediate OOM risk.",
					availPct, sysMem.AvailableMB, sysMem.TotalMB, swapPressure, swapPct)
			} else {
				summary = fmt.Sprintf("Memory pressure %s: %.1f%% available (%d MB free of %d MB), swap pressure %s (%.1f%% used). Top OOM candidates: %v.",
					strings.ToUpper(riskLevel), availPct, sysMem.AvailableMB, sysMem.TotalMB, swapPressure, swapPct, topCandidates)
			}

			oom := OOMRiskAssessment{
				RiskLevel:        riskLevel,
				AvailablePercent: availPct,
				SwapPressure:     swapPressure,
				TopOOMCandidates: topCandidates,
				Summary:          summary,
			}

			return InvestigateMemoryOutput{
				SystemMemory: sysMem,
				TopProcesses: details,
				OOMRisk:      oom,
				ProcessCount: len(details),
			}, nil
		},
	)
	investigateMemory.Category = "composed"
	investigateMemory.SearchTerms = []string{"memory leak", "oom risk", "high memory", "ram usage", "smaps", "top memory processes", "investigate memory"}
	investigateMemory.MaxResultChars = 9000
	investigateCpu := handler.TypedHandler[InvestigateCpuInput, InvestigateCpuOutput](
		"investigate_cpu",
		"Call this when you need a single-shot diagnosis of system and process CPU load. Composed investigation checks load averages, per-core saturation, top CPU consumers, process scheduling state (R/S/D/Z), thread counts, and cgroups.",
		func(_ context.Context, input InvestigateCpuInput) (InvestigateCpuOutput, error) {
			topCount := input.TopCount
			if topCount <= 0 {
				topCount = 10
			}
			if topCount > 50 {
				topCount = 50
			}

			loadData, err := readProcFile("loadavg")
			if err != nil {
				return InvestigateCpuOutput{}, fmt.Errorf("[%s] failed to read /proc/loadavg: %w", handler.ErrAPIError, err)
			}
			loads := parseLoadAvg(loadData)
			if len(loads) < 3 {
				loads = []float64{0, 0, 0}
			}

			numCPU := runtime.NumCPU()
			if cpuData, err := readProcFile("cpuinfo"); err == nil {
				if c := parseCpuCount(cpuData); c > 0 {
					numCPU = c
				}
			}

			loadPerCore := 0.0
			if numCPU > 0 {
				loadPerCore = math.Round((loads[0]/float64(numCPU))*100) / 100
			}

			saturationRisk := "LOW"
			switch {
			case loadPerCore >= 2.0:
				saturationRisk = "CRITICAL"
			case loadPerCore >= 1.2:
				saturationRisk = "HIGH"
			case loadPerCore >= 0.8:
				saturationRisk = "ELEVATED"
			}

			sysCPU := SystemCpuSummary{
				Load1:          loads[0],
				Load5:          loads[1],
				Load15:         loads[2],
				NumCPU:         numCPU,
				LoadPerCore:    loadPerCore,
				SaturationRisk: saturationRisk,
			}

			psOut, _, err := runCmd("ps", "aux", "--sort=-pcpu")
			if err != nil {
				return InvestigateCpuOutput{}, fmt.Errorf("[%s] ps command failed: %w", handler.ErrAPIError, err)
			}

			lines := strings.Split(psOut, "\n")
			var details []ProcessCpuDetail
			dStateCount := 0
			zombieCount := 0
			var topProcess ProcessCpuDetail

			for i, line := range lines {
				if i == 0 {
					continue
				}
				p, ok := parsePsLine(line)
				if !ok {
					continue
				}
				if input.ThresholdCPU > 0 && p.CPU < input.ThresholdCPU {
					continue
				}

				if strings.HasPrefix(p.Stat, "D") {
					dStateCount++
				}
				if strings.HasPrefix(p.Stat, "Z") {
					zombieCount++
				}

				threads := 1
				if statusData, err := readProcFile(fmt.Sprintf("%d/status", p.PID)); err == nil {
					for sLine := range strings.SplitSeq(statusData, "\n") {
						if strings.HasPrefix(sLine, "Threads:") {
							f := strings.Fields(sLine)
							if len(f) >= 2 {
								if tVal, err := strconv.Atoi(f[1]); err == nil {
									threads = tVal
								}
							}
							break
						}
					}
				}

				cgroup := ""
				if cgData, err := readProcFile(fmt.Sprintf("%d/cgroup", p.PID)); err == nil {
					cgLines := strings.Split(strings.TrimSpace(cgData), "\n")
					if len(cgLines) > 0 {
						parts := strings.Split(cgLines[len(cgLines)-1], ":")
						if len(parts) >= 3 {
							cgroup = parts[2]
						}
					}
				}

				d := ProcessCpuDetail{
					PID:        p.PID,
					User:       p.User,
					Command:    p.Command,
					CPUPercent: p.CPU,
					MemPercent: p.Mem,
					State:      p.Stat,
					Threads:    threads,
					CGroup:     cgroup,
				}

				if len(details) == 0 {
					topProcess = d
				}

				details = append(details, d)
				if len(details) >= topCount {
					break
				}
			}

			diagnosis := fmt.Sprintf("CPU load and scheduling nominal across %d cores (load per core: %.2f).", numCPU, loadPerCore)
			if saturationRisk == "CRITICAL" {
				diagnosis = fmt.Sprintf("CRITICAL CPU saturation: load per core is %.2f across %d cores. Top consumer PID %d (%s) at %.1f%% CPU.", loadPerCore, numCPU, topProcess.PID, topProcess.Command, topProcess.CPUPercent)
			} else if saturationRisk == "HIGH" || saturationRisk == "ELEVATED" {
				diagnosis = fmt.Sprintf("%s CPU load: load per core is %.2f. Top consumer PID %d (%s) at %.1f%% CPU.", saturationRisk, loadPerCore, topProcess.PID, topProcess.Command, topProcess.CPUPercent)
			} else if dStateCount > 0 {
				diagnosis = fmt.Sprintf("Disk IO-wait pressure: %d process(es) in uninterruptible sleep (D state).", dStateCount)
			} else if zombieCount > 0 {
				diagnosis = fmt.Sprintf("Process cleanup alert: %d zombie process(es) detected.", zombieCount)
			}

			return InvestigateCpuOutput{
				SystemCPU:    sysCPU,
				TopProcesses: details,
				ProcessCount: len(details),
				Diagnosis:    diagnosis,
			}, nil
		},
	)
	investigateCpu.Category = "composed"
	investigateCpu.SearchTerms = []string{"cpu usage", "high cpu", "cpu load", "load average", "runaway process", "investigate cpu"}
	investigateCpu.MaxResultChars = 9000

	psSearch := handler.TypedHandler[PsSearchInput, PsSearchOutput](
		"ps_search",
		"Find running processes matching multiple criteria: command substring, open file path (via /proc/PID/fd), environment variable (via /proc/PID/environ), port binding, or user.",
		func(_ context.Context, input PsSearchInput) (PsSearchOutput, error) {
			return searchProcesses(input)
		},
	)
	psSearch.Category = "inspection"
	psSearch.SearchTerms = []string{"find process", "ps search", "lsof", "open file process", "environ process", "process by port"}
	psSearch.MaxResultChars = 9000

	procInspect := handler.TypedHandler[ProcInspectInput, ProcInspectOutput](
		"proc_inspect",
		"Deep inspection of a process in /proc: full stat telemetry, status summary, cmdline arguments, open fd counts, memory RSS/VIRT breakdown, and CPU tick deltas.",
		func(_ context.Context, input ProcInspectInput) (ProcInspectOutput, error) {
			return InspectProcess(input)
		},
	)
	procInspect.Category = "inspection"
	procInspect.SearchTerms = []string{"proc inspect", "process telemetry", "cpu ticks", "fd count", "memory rss", "process stat"}
	procInspect.MaxResultChars = 9000

	livenessGate := handler.TypedHandler[LivenessGateInput, LivenessGateOutput](
		"liveness_gate",
		"Evaluate configurable threshold gates for agent process health, stall and zombie detection, memory and fd leak thresholds, and non-invasive health assertions.",
		func(_ context.Context, input LivenessGateInput) (LivenessGateOutput, error) {
			return EvaluateLiveness(input)
		},
	)
	livenessGate.Category = "safety"
	livenessGate.SearchTerms = []string{"liveness gate", "health gate", "stall detection", "zombie detection", "memory leak threshold", "process health"}
	livenessGate.MaxResultChars = 9000

	dbusProbe := handler.TypedHandler[DbusProbeInput, DbusProbeOutput](
		"dbus_probe",
		"Query systemd unit states, properties, daemon lifecycle, and bus events over system or session D-Bus without shelling out.",
		func(_ context.Context, input DbusProbeInput) (DbusProbeOutput, error) {
			return ProbeDbus(input)
		},
	)
	dbusProbe.Category = "inspection"
	dbusProbe.SearchTerms = []string{"dbus probe", "systemd dbus", "unit state", "daemon lifecycle", "property change", "dbus events"}
	dbusProbe.MaxResultChars = 9000

	return []registry.ToolDefinition{
		psList,
		psTree,
		killProcess,
		portList,
		gpuStatus,
		systemInfo,
		investigatePort,
		investigateService,
		fdList,
		memMap,
		netConnections,
		investigateMemory,
		investigateCpu,
		psSearch,
		procInspect,
		livenessGate,
		dbusProbe,
	}
}

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

func Setup() (*registry.ToolRegistry, *registry.MCPServer) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("service", "process-mcp"))

	slog.Info("server starting", "name", "process-mcp", "version", "1.0.0")

	reg := registry.NewToolRegistry(registry.Config{
		Middleware: []registry.Middleware{
			gate.Middleware(gate.Config{Gate: gate.PauseWrites}),
			registry.AuditMiddleware(""),
			registry.SafetyTierMiddleware(),
		},
	})
	mod := &ProcessModule{}
	reg.RegisterModule(mod)
	slog.Info("tools registered", "module", mod.Name(), "count", len(mod.Tools()))

	s := registry.NewMCPServer("process-mcp", "1.0.0")
	reg.RegisterWithServer(s)
	buildProcessResourceRegistry().RegisterWithServer(s)
	buildProcessPromptRegistry().RegisterWithServer(s)

	return reg, s
}
