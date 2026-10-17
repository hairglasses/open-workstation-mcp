// Command systemd-mcp is an MCP server for systemd service and timer
// management via the Model Context Protocol (stdio transport).
//
// Usage:
//
//	systemd-mcp
package systemd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/hairglasses-studio/mcpkit/handler"
	"github.com/hairglasses-studio/mcpkit/middleware/gate"
	"github.com/hairglasses-studio/mcpkit/registry"
)

// ---------------------------------------------------------------------------
// I/O types
// ---------------------------------------------------------------------------

// ── systemd_status ─────────────────────────────────────────────────────────

type StatusInput struct {
	Unit   string `json:"unit" jsonschema:"required,description=Systemd unit name (e.g. makima.service or shader-rotate.timer)"`
	System bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
}

type StatusOutput struct {
	Unit                 string `json:"unit"`
	ActiveState          string `json:"active_state"`
	SubState             string `json:"sub_state"`
	Description          string `json:"description"`
	LoadState            string `json:"load_state"`
	FragmentPath         string `json:"fragment_path,omitempty"`
	ActiveEnterTimestamp string `json:"active_enter_timestamp,omitempty"`
	MainPID              int    `json:"main_pid,omitempty"`
	MemoryCurrent        string `json:"memory_current,omitempty"`
	CPUUsageNSec         string `json:"cpu_usage_nsec,omitempty"`
}

// ── systemd_start ──────────────────────────────────────────────────────────

type StartInput struct {
	Unit   string `json:"unit" jsonschema:"required,description=Systemd unit name to start"`
	System bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
}

type StartOutput struct {
	Unit    string `json:"unit"`
	Message string `json:"message"`
}

// ── systemd_stop ───────────────────────────────────────────────────────────

type StopInput struct {
	Unit    string `json:"unit" jsonschema:"required,description=Systemd unit name to stop"`
	System  bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"description=Required for critical services (sshd, NetworkManager, systemd-*, dbus, polkit)"`
}

type StopOutput struct {
	Unit    string `json:"unit"`
	Message string `json:"message"`
}

// ── systemd_restart ────────────────────────────────────────────────────────

type RestartInput struct {
	Unit   string `json:"unit" jsonschema:"required,description=Systemd unit name to restart"`
	System bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
}

type RestartOutput struct {
	Unit    string `json:"unit"`
	Message string `json:"message"`
}

// ── systemd_enable ─────────────────────────────────────────────────────────

type EnableInput struct {
	Unit   string `json:"unit" jsonschema:"required,description=Systemd unit name to enable"`
	System bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
}

type EnableOutput struct {
	Unit    string `json:"unit"`
	Message string `json:"message"`
}

// ── systemd_disable ────────────────────────────────────────────────────────

type DisableInput struct {
	Unit    string `json:"unit" jsonschema:"required,description=Systemd unit name to disable"`
	System  bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"description=Required for critical services (sshd, NetworkManager, systemd-*, dbus, polkit)"`
}

type DisableOutput struct {
	Unit    string `json:"unit"`
	Message string `json:"message"`
}

// ── systemd_logs ───────────────────────────────────────────────────────────

type LogsInput struct {
	Unit     string `json:"unit" jsonschema:"required,description=Systemd unit name to fetch logs for"`
	Lines    int    `json:"lines,omitempty" jsonschema:"description=Number of log lines to return. Default 50."`
	Since    string `json:"since,omitempty" jsonschema:"description=Show logs since this time (e.g. '1h ago' or '2024-01-01')"`
	Until    string `json:"until,omitempty" jsonschema:"description=Show logs until this time (e.g. 'now' or '2026-09-01')"`
	Pattern  string `json:"pattern,omitempty" jsonschema:"description=Grep filter pattern for log messages (-g/--grep)"`
	Priority string `json:"priority,omitempty" jsonschema:"description=Filter by log priority (e.g. 'err', 'warning', 'info', or 0-7)"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"description=Cursor to paginate from (fetches logs after this cursor)"`
	Offset   int    `json:"offset,omitempty" jsonschema:"description=Number of lines to skip for offset-based pagination"`
	Reverse  bool   `json:"reverse,omitempty" jsonschema:"description=Show newest entries first (-r/--reverse)"`
	System   bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
}

type LogsOutput struct {
	Unit       string `json:"unit"`
	Lines      int    `json:"lines"`
	Offset     int    `json:"offset,omitempty"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more,omitempty"`
	Logs       string `json:"logs"`
}

// ── systemd_list_units ─────────────────────────────────────────────────────

type ListUnitsInput struct {
	State  string `json:"state,omitempty" jsonschema:"description=Filter by unit state (e.g. active, inactive, failed)"`
	System bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
}

type ListUnitsOutput struct {
	Units json.RawMessage `json:"units"`
}

// AsUnits parses the underlying units JSON into a strongly typed slice of UnitInfo.
func (o ListUnitsOutput) AsUnits() ([]UnitInfo, error) {
	var units []UnitInfo
	if len(o.Units) == 0 {
		return units, nil
	}
	err := json.Unmarshal(o.Units, &units)
	return units, err
}

// ── systemd_list_timers ────────────────────────────────────────────────────

type ListTimersInput struct {
	System bool `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
}

type ListTimersOutput struct {
	Timers json.RawMessage `json:"timers"`
}

// AsTimers parses the underlying timers JSON into a strongly typed slice of TimerInfo.
func (o ListTimersOutput) AsTimers() ([]TimerInfo, error) {
	var timers []TimerInfo
	if len(o.Timers) == 0 {
		return timers, nil
	}
	err := json.Unmarshal(o.Timers, &timers)
	return timers, err
}

// ── systemd_failed ─────────────────────────────────────────────────────────

type FailedInput struct {
	System bool `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
}

type FailedOutput struct {
	Units json.RawMessage `json:"units"`
}

// AsUnits parses the underlying failed units JSON into a strongly typed slice of UnitInfo.
func (o FailedOutput) AsUnits() ([]UnitInfo, error) {
	var units []UnitInfo
	if len(o.Units) == 0 {
		return units, nil
	}
	err := json.Unmarshal(o.Units, &units)
	return units, err
}

// ── systemd_investigate_unit ──────────────────────────────────────────────

type InvestigateUnitInput struct {
	Unit     string `json:"unit" jsonschema:"required,description=Systemd unit name to diagnose (e.g. pipewire.service, dbus.service)"`
	System   bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
	LogLines int    `json:"log_lines,omitempty" jsonschema:"description=Number of recent journal log lines to fetch. Default: 20."`
}

type UnitDependencies struct {
	Requires   []string `json:"requires,omitempty"`
	Wants      []string `json:"wants,omitempty"`
	RequiredBy []string `json:"required_by,omitempty"`
	WantedBy   []string `json:"wanted_by,omitempty"`
	After      []string `json:"after,omitempty"`
	Before     []string `json:"before,omitempty"`
}

type InvestigateUnitOutput struct {
	Unit         string           `json:"unit"`
	Status       StatusOutput     `json:"status"`
	Dependencies UnitDependencies `json:"dependencies"`
	RecentLogs   string           `json:"recent_logs"`
}

// ── systemd_create_timer ──────────────────────────────────────────────────

type CreateTimerInput struct {
	System      bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
	Unit        string `json:"unit,omitempty" jsonschema:"description=Optional transient unit name (without .timer suffix)"`
	Description string `json:"description,omitempty" jsonschema:"description=Optional human-readable description for the timer"`
	OnCalendar  string `json:"on_calendar,omitempty" jsonschema:"description=Calendar event expression (e.g. 'hourly', 'daily', '*-*-* 04:00:00', 'Mon..Fri 10:00')"`
	OnBootSec   string `json:"on_boot_sec,omitempty" jsonschema:"description=Monotonic timer relative to machine boot (e.g. '15min', '1h')"`
	OnActiveSec string `json:"on_active_sec,omitempty" jsonschema:"description=Monotonic timer relative to activation (e.g. '30s', '5min')"`
	Command     string `json:"command" jsonschema:"required,description=Command string or executable to run when the timer elapses"`
}

type CreateTimerOutput struct {
	TimerUnit   string `json:"timer_unit"`
	ServiceUnit string `json:"service_unit"`
	Created     bool   `json:"created"`
	Output      string `json:"output"`
}

// ── systemd_analyze ───────────────────────────────────────────────────────

type AnalyzeInput struct {
	System  bool   `json:"system,omitempty" jsonschema:"description=Target system scope instead of user scope. Default: user scope."`
	Command string `json:"command,omitempty" jsonschema:"description=Analysis sub-command: 'time'\\, 'blame' (default)\\, 'critical-chain'\\, or 'verify'"`
	Unit    string `json:"unit,omitempty" jsonschema:"description=Optional unit name filter for critical-chain or verify"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=Max results to return for blame. Default 20."`
}

type BlameEntry struct {
	Duration string `json:"duration"`
	Unit     string `json:"unit"`
}

type AnalyzeOutput struct {
	Command       string       `json:"command"`
	System        bool         `json:"system"`
	StartupTime   string       `json:"startup_time,omitempty"`
	Blame         []BlameEntry `json:"blame,omitempty"`
	CriticalChain string       `json:"critical_chain,omitempty"`
	Verification  string       `json:"verification,omitempty"`
	RawOutput     string       `json:"raw_output"`
}

// ---------------------------------------------------------------------------
// Critical service guard
// ---------------------------------------------------------------------------

// criticalPrefixes lists service name prefixes that require explicit confirmation
// before being stopped or disabled.
var criticalPrefixes = []string{"sshd", "NetworkManager", "systemd-", "dbus", "polkit"}

func requireConfirmation(unit string, confirm bool, action string) error {
	for _, prefix := range criticalPrefixes {
		if strings.HasPrefix(unit, prefix) && !confirm {
			return fmt.Errorf("[%s] %sing critical service %q requires confirm: true",
				handler.ErrInvalidParam, action, unit)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// SystemdModule
// ---------------------------------------------------------------------------

type SystemdModule struct{}

func (m *SystemdModule) Name() string        { return "systemd" }
func (m *SystemdModule) Description() string { return "Systemd service and timer management" }

func (m *SystemdModule) Tools() []registry.ToolDefinition {
	// ── Read-only tools (IsWrite: false) ───────────────────────────────

	status := handler.TypedHandler[StatusInput, StatusOutput](
		"systemd_status",
		"Show detailed status of a systemd unit including active state, PID, memory, and CPU usage.",
		func(_ context.Context, input StatusInput) (StatusOutput, error) {
			return readUnitStatus(input.System, input.Unit)
		},
	)

	listUnits := handler.TypedHandler[ListUnitsInput, ListUnitsOutput](
		"systemd_list_units",
		"List systemd units, optionally filtered by state.",
		func(_ context.Context, input ListUnitsInput) (ListUnitsOutput, error) {
			return listUnits(input.System, input.State)
		},
	)

	listTimers := handler.TypedHandler[ListTimersInput, ListTimersOutput](
		"systemd_list_timers",
		"List active systemd timers with their next/last trigger times.",
		func(_ context.Context, input ListTimersInput) (ListTimersOutput, error) {
			return listTimers(input.System)
		},
	)

	logs := handler.TypedHandler[LogsInput, LogsOutput](
		"systemd_logs",
		"Fetch recent journal logs for a systemd unit with support for line limits, time bounds, offset, and cursor pagination.",
		func(_ context.Context, input LogsInput) (LogsOutput, error) {
			return readUnitLogs(input)
		},
	)
	logs.SearchTerms = []string{"journal", "journald", "service logs", "unit logs"}
	logs.MaxResultChars = 8000

	failed := handler.TypedHandler[FailedInput, FailedOutput](
		"systemd_failed",
		"List failed systemd units.",
		func(_ context.Context, input FailedInput) (FailedOutput, error) {
			return failedUnits(input.System)
		},
	)

	// ── Mutating tools (IsWrite: true, ComplexityModerate) ─────────────

	start := handler.TypedHandler[StartInput, StartOutput](
		"systemd_start",
		"Start a systemd unit.",
		func(_ context.Context, input StartInput) (StartOutput, error) {
			return startUnit(input.System, input.Unit)
		},
	)
	start.IsWrite = true
	start.Complexity = registry.ComplexityModerate

	restart := handler.TypedHandler[RestartInput, RestartOutput](
		"systemd_restart",
		"Restart a systemd unit.",
		func(_ context.Context, input RestartInput) (RestartOutput, error) {
			return restartUnit(input.System, input.Unit)
		},
	)
	restart.IsWrite = true
	restart.Complexity = registry.ComplexityModerate

	enable := handler.TypedHandler[EnableInput, EnableOutput](
		"systemd_enable",
		"Enable a systemd unit to start on boot/login.",
		func(_ context.Context, input EnableInput) (EnableOutput, error) {
			return enableUnit(input.System, input.Unit)
		},
	)
	enable.IsWrite = true
	enable.Complexity = registry.ComplexityModerate

	// ── Destructive tools (IsWrite: true, ComplexityComplex) ───────────

	stop := handler.TypedHandler[StopInput, StopOutput](
		"systemd_stop",
		"Stop a systemd unit. Critical services (sshd, NetworkManager, systemd-*, dbus, polkit) require confirm: true.",
		func(_ context.Context, input StopInput) (StopOutput, error) {
			if err := requireConfirmation(input.Unit, input.Confirm, "stopp"); err != nil {
				return StopOutput{}, err
			}
			return stopUnit(input.System, input.Unit)
		},
	)
	stop.IsWrite = true
	stop.Complexity = registry.ComplexityComplex

	disable := handler.TypedHandler[DisableInput, DisableOutput](
		"systemd_disable",
		"Disable a systemd unit from starting on boot/login. Critical services (sshd, NetworkManager, systemd-*, dbus, polkit) require confirm: true.",
		func(_ context.Context, input DisableInput) (DisableOutput, error) {
			if err := requireConfirmation(input.Unit, input.Confirm, "disabl"); err != nil {
				return DisableOutput{}, err
			}
			return disableUnit(input.System, input.Unit)
		},
	)
	disable.IsWrite = true
	disable.Complexity = registry.ComplexityComplex
	// ── Composed diagnostic tool ──────────────────────────────────────

	investigateUnitTool := handler.TypedHandler[InvestigateUnitInput, InvestigateUnitOutput](
		"systemd_investigate_unit",
		"Single-shot diagnosis of a systemd unit: status, recent journal logs, and dependency tree (Requires, Wants, After, Before).",
		func(_ context.Context, input InvestigateUnitInput) (InvestigateUnitOutput, error) {
			return investigateUnit(input.System, input.Unit, input.LogLines)
		},
	)
	investigateUnitTool.SearchTerms = []string{"debug unit", "unit investigation", "systemd debug", "service diagnostics", "investigate unit", "investigate service"}
	investigateUnitTool.MaxResultChars = 9000

	createTimerTool := handler.TypedHandler[CreateTimerInput, CreateTimerOutput](
		"systemd_create_timer",
		"Create a transient systemd timer with calendar or monotonic scheduling (OnCalendar, OnBootSec, OnActiveSec).",
		func(_ context.Context, input CreateTimerInput) (CreateTimerOutput, error) {
			return createTimer(input)
		},
	)
	createTimerTool.IsWrite = true
	createTimerTool.Complexity = registry.ComplexityModerate
	createTimerTool.SearchTerms = []string{"schedule task", "create timer", "cron", "transient timer", "systemd-run"}

	analyzeTool := handler.TypedHandler[AnalyzeInput, AnalyzeOutput](
		"systemd_analyze",
		"Analyze systemd boot performance, startup delays (blame), critical-chain dependency bottlenecks, or unit file verification.",
		func(_ context.Context, input AnalyzeInput) (AnalyzeOutput, error) {
			return analyzeSystemd(input)
		},
	)
	analyzeTool.Category = "diagnostic"
	analyzeTool.SearchTerms = []string{"boot time", "blame", "critical chain", "slow service", "systemd-analyze", "verify unit"}
	analyzeTool.MaxResultChars = 9000

	return []registry.ToolDefinition{
		status,
		start,
		stop,
		restart,
		enable,
		disable,
		logs,
		listUnits,
		listTimers,
		failed,
		investigateUnitTool,
		createTimerTool,
		analyzeTool,
	}
}

// ---------------------------------------------------------------------------
// Setup / main
// ---------------------------------------------------------------------------

func Setup() (*registry.ToolRegistry, *registry.MCPServer) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("service", "systemd-mcp"))

	slog.Info("server starting", "name", "systemd-mcp", "version", "1.2.0")

	initDBus()

	reg := registry.NewToolRegistry(registry.Config{
		Middleware: []registry.Middleware{
			gate.Middleware(gate.Config{Gate: gate.PauseWrites}),
			registry.AuditMiddleware(""),
			registry.SafetyTierMiddleware(),
		},
	})
	mod := &SystemdModule{}
	reg.RegisterModule(mod)
	slog.Info("tools registered", "module", mod.Name(), "count", len(mod.Tools()))

	s := registry.NewMCPServer("systemd-mcp", "1.2.0")
	reg.RegisterWithServer(s)
	buildSystemdResourceRegistry().RegisterWithServer(s)
	buildSystemdPromptRegistry().RegisterWithServer(s)

	return reg, s
}

func main() {
	_, s := Setup()
	if err := registry.ServeAuto(s); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func InitDBus() {
	initDBus()
}
