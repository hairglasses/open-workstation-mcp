package systemd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
)

type backendFailure struct {
	Operation       string
	Scope           string
	PrimaryBackend  string
	PrimaryErr      error
	FallbackBackend string
	FallbackErr     error
}

func (e *backendFailure) Error() string {
	parts := make([]string, 0, 2)
	if e.PrimaryErr != nil {
		parts = append(parts, fmt.Sprintf("%s failed: %v", e.PrimaryBackend, e.PrimaryErr))
	}
	if e.FallbackErr != nil && e.FallbackBackend != "" {
		parts = append(parts, fmt.Sprintf("%s failed: %v", e.FallbackBackend, e.FallbackErr))
	}
	return fmt.Sprintf("%s failed for %s scope: %s", e.Operation, e.Scope, strings.Join(parts, "; "))
}

func (e *backendFailure) Unwrap() []error {
	errs := make([]error, 0, 2)
	if e.PrimaryErr != nil {
		errs = append(errs, e.PrimaryErr)
	}
	if e.FallbackErr != nil {
		errs = append(errs, e.FallbackErr)
	}
	return errs
}

func backendErrorCode(primaryErr, fallbackErr error) string {
	if isPermissionErr(primaryErr) || isPermissionErr(fallbackErr) {
		return handler.ErrPermission
	}
	return handler.ErrUpstreamError
}

func newBackendFailure(operation string, system bool, primaryBackend string, primaryErr error, fallbackBackend string, fallbackErr error) error {
	failure := &backendFailure{
		Operation:       operation,
		Scope:           scopeName(system),
		PrimaryBackend:  primaryBackend,
		PrimaryErr:      primaryErr,
		FallbackBackend: fallbackBackend,
		FallbackErr:     fallbackErr,
	}
	return fmt.Errorf("[%s] %w", backendErrorCode(primaryErr, fallbackErr), failure)
}

func newSingleBackendFailure(operation string, system bool, backend string, err error) error {
	return newBackendFailure(operation, system, backend, err, "", nil)
}

func isPermissionErr(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{
		"permission denied",
		"access denied",
		"authentication is required",
		"interactive authentication required",
		"not authorized",
		"authorization failed",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isUnitMissingErr(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{
		"could not be found",
		"no such unit",
		"nosuchunit",
		"loadstate=not-found",
		"unit not found",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func requireDBusBackend(system bool) (*SystemdDBus, error) {
	sdb := getDBus(system)
	if sdb == nil {
		return nil, fmt.Errorf("dbus backend unavailable for %s scope: dbus connection not initialized", scopeName(system))
	}
	if err := probeDBusManager(sdb); err != nil {
		return nil, fmt.Errorf("dbus backend unavailable for %s scope: %w", scopeName(system), err)
	}
	return sdb, nil
}

func requireSystemctlBackend(system bool) error {
	if err := probeSystemctl(!system); err != nil {
		return fmt.Errorf("systemctl backend unavailable for %s scope: %w", scopeName(system), err)
	}
	return nil
}

func requireJournalctlBackend(system bool) error {
	if err := probeJournalctl(!system); err != nil {
		return fmt.Errorf("journalctl backend unavailable for %s scope: %w", scopeName(system), err)
	}
	return nil
}

func parseStatusOutput(unit, out string) StatusOutput {
	result := StatusOutput{Unit: unit}
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		switch key {
		case "ActiveState":
			result.ActiveState = val
		case "SubState":
			result.SubState = val
		case "Description":
			result.Description = val
		case "LoadState":
			result.LoadState = val
		case "FragmentPath":
			result.FragmentPath = val
		case "ActiveEnterTimestamp":
			result.ActiveEnterTimestamp = val
		case "MainPID":
			result.MainPID, _ = strconv.Atoi(val)
		case "MemoryCurrent":
			if val != "[not set]" {
				result.MemoryCurrent = val
			}
		case "CPUUsageNSec":
			if val != "[not set]" {
				result.CPUUsageNSec = val
			}
		}
	}
	return result
}

// validateUnitName ensures unit names do not contain path traversal, shell metacharacters, or illegal lengths.
func validateUnitName(unit string) error {
	trimmed := strings.TrimSpace(unit)
	if trimmed == "" {
		return fmt.Errorf("[%s] unit name cannot be empty", handler.ErrInvalidParam)
	}
	if len(trimmed) > 256 {
		return fmt.Errorf("[%s] unit name exceeds maximum length of 256 characters", handler.ErrInvalidParam)
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "..") {
		return fmt.Errorf("[%s] invalid unit name %q: path traversal not allowed", handler.ErrInvalidParam, unit)
	}
	// Permitted characters for systemd unit names: letters, numbers, @, -, _, ., :, \
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '@' || r == '-' || r == '_' || r == '.' || r == ':' || r == '\\' {
			continue
		}
		return fmt.Errorf("[%s] invalid unit name %q: illegal or shell metacharacter '%c' not allowed", handler.ErrInvalidParam, unit, r)
	}
	return nil
}

// validatePriority ensures priority strings are valid syslog/systemd priority levels or ranges.
func validatePriority(priority string) error {
	trimmed := strings.TrimSpace(priority)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) > 32 {
		return fmt.Errorf("[%s] priority exceeds maximum length of 32 characters", handler.ErrInvalidParam)
	}
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return fmt.Errorf("[%s] invalid priority %q: contains illegal characters", handler.ErrInvalidParam, priority)
	}
	return nil
}

func readUnitStatus(system bool, unit string) (StatusOutput, error) {
	if err := validateUnitName(unit); err != nil {
		return StatusOutput{}, err
	}
	result, primaryErr := readUnitStatusViaDBus(system, unit)
	switch {
	case primaryErr == nil && result.LoadState == "not-found":
		return result, fmt.Errorf("[%s] unit %s not found", handler.ErrNotFound, unit)
	case primaryErr == nil:
		return result, nil
	case isUnitMissingErr(primaryErr):
		return StatusOutput{}, fmt.Errorf("[%s] unit %s not found", handler.ErrNotFound, unit)
	}

	result, fallbackErr := readUnitStatusViaSystemctl(system, unit)
	switch {
	case fallbackErr == nil && result.LoadState == "not-found":
		return result, fmt.Errorf("[%s] unit %s not found", handler.ErrNotFound, unit)
	case fallbackErr == nil:
		return result, nil
	case isUnitMissingErr(fallbackErr):
		return StatusOutput{}, fmt.Errorf("[%s] unit %s not found", handler.ErrNotFound, unit)
	default:
		return StatusOutput{}, newBackendFailure("systemd_status", system, "dbus", primaryErr, "systemctl", fallbackErr)
	}
}

func readUnitStatusViaDBus(system bool, unit string) (StatusOutput, error) {
	sdb, err := requireDBusBackend(system)
	if err != nil {
		return StatusOutput{}, err
	}
	us, err := sdb.GetUnitStatus(unit)
	if err != nil {
		return StatusOutput{}, fmt.Errorf("dbus status %s: %w", unit, err)
	}
	return unitStatusToOutput(unit, us), nil
}

func readUnitStatusViaSystemctl(system bool, unit string) (StatusOutput, error) {
	if err := requireSystemctlBackend(system); err != nil {
		return StatusOutput{}, err
	}
	out, err := runSystemctl(!system, "show",
		"--property=ActiveState,SubState,Description,LoadState,FragmentPath,ActiveEnterTimestamp,MainPID,MemoryCurrent,CPUUsageNSec",
		unit,
	)
	if err != nil {
		return StatusOutput{}, fmt.Errorf("systemctl status %s: %w", unit, err)
	}
	return parseStatusOutput(unit, out), nil
}

func listUnits(system bool, state string) (ListUnitsOutput, error) {
	out, primaryErr := listUnitsViaDBus(system, state)
	if primaryErr == nil {
		return out, nil
	}

	out, fallbackErr := listUnitsViaSystemctl(system, state)
	if fallbackErr == nil {
		return out, nil
	}
	return ListUnitsOutput{}, newBackendFailure("systemd_list_units", system, "dbus", primaryErr, "systemctl", fallbackErr)
}

func listUnitsViaDBus(system bool, state string) (ListUnitsOutput, error) {
	sdb, err := requireDBusBackend(system)
	if err != nil {
		return ListUnitsOutput{}, err
	}

	var units []UnitInfo
	if state != "" {
		units, err = sdb.ListUnitsFiltered([]string{state})
	} else {
		units, err = sdb.ListUnits()
	}
	if err != nil {
		return ListUnitsOutput{}, fmt.Errorf("dbus list units: %w", err)
	}
	raw, err := unitsToJSON(units)
	if err != nil {
		return ListUnitsOutput{}, fmt.Errorf("marshal dbus units: %w", err)
	}
	return ListUnitsOutput{Units: raw}, nil
}

// ensureJSONArray guarantees the raw string represents a valid JSON array.
func ensureJSONArray(raw string) json.RawMessage {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return json.RawMessage("[]")
	}
	return json.RawMessage(trimmed)
}

func listUnitsViaSystemctl(system bool, state string) (ListUnitsOutput, error) {
	if err := requireSystemctlBackend(system); err != nil {
		return ListUnitsOutput{}, err
	}

	args := []string{"list-units", "--output=json"}
	if state != "" {
		args = append(args, "--state="+state)
	}
	out, err := runSystemctl(!system, args...)
	if err != nil {
		return ListUnitsOutput{}, fmt.Errorf("systemctl list units: %w", err)
	}
	return ListUnitsOutput{Units: ensureJSONArray(out)}, nil
}

func listTimers(system bool) (ListTimersOutput, error) {
	out, primaryErr := listTimersViaDBus(system)
	if primaryErr == nil {
		return out, nil
	}

	out, fallbackErr := listTimersViaSystemctl(system)
	if fallbackErr == nil {
		return out, nil
	}
	return ListTimersOutput{}, newBackendFailure("systemd_list_timers", system, "dbus", primaryErr, "systemctl", fallbackErr)
}

func listTimersViaDBus(system bool) (ListTimersOutput, error) {
	sdb, err := requireDBusBackend(system)
	if err != nil {
		return ListTimersOutput{}, err
	}
	timers, err := sdb.ListTimers()
	if err != nil {
		return ListTimersOutput{}, fmt.Errorf("dbus list timers: %w", err)
	}
	raw, err := timersToJSON(timers)
	if err != nil {
		return ListTimersOutput{}, fmt.Errorf("marshal dbus timers: %w", err)
	}
	return ListTimersOutput{Timers: raw}, nil
}

func listTimersViaSystemctl(system bool) (ListTimersOutput, error) {
	if err := requireSystemctlBackend(system); err != nil {
		return ListTimersOutput{}, err
	}
	out, err := runSystemctl(!system, "list-timers", "--output=json", "--no-pager")
	if err != nil {
		return ListTimersOutput{}, fmt.Errorf("systemctl list timers: %w", err)
	}
	return ListTimersOutput{Timers: ensureJSONArray(out)}, nil
}

func readUnitLogs(input LogsInput) (LogsOutput, error) {
	if err := validateUnitName(input.Unit); err != nil {
		return LogsOutput{}, err
	}
	if err := validatePriority(input.Priority); err != nil {
		return LogsOutput{}, err
	}
	lines := input.Lines
	if lines <= 0 {
		lines = 50
	}
	if err := requireJournalctlBackend(input.System); err != nil {
		return LogsOutput{}, newSingleBackendFailure("systemd_logs", input.System, "journalctl", err)
	}

	fetchLines := lines
	if input.Offset > 0 {
		fetchLines += input.Offset
	}

	args := []string{"-u", input.Unit, "-n", strconv.Itoa(fetchLines)}
	if !input.System {
		args = []string{"--user-unit", input.Unit, "-n", strconv.Itoa(fetchLines)}
	}
	if input.Since != "" {
		args = append(args, "--since", input.Since)
	}
	if input.Until != "" {
		args = append(args, "--until", input.Until)
	}
	if input.Pattern != "" {
		args = append(args, "--grep="+input.Pattern)
	}
	if input.Priority != "" {
		args = append(args, "--priority="+input.Priority)
	}
	if input.Cursor != "" {
		args = append(args, "--after-cursor="+input.Cursor)
	}
	if input.Reverse {
		args = append(args, "--reverse")
	}
	args = append(args, "--show-cursor", "--no-pager")

	out, err := runJournalctl(!input.System, args...)
	if err != nil {
		return LogsOutput{}, newSingleBackendFailure("systemd_logs", input.System, "journalctl", fmt.Errorf("journalctl logs %s: %w", input.Unit, err))
	}

	var cleanLines []string
	nextCursor := ""
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "-- cursor: ") {
			nextCursor = strings.TrimPrefix(line, "-- cursor: ")
			continue
		}
		cleanLines = append(cleanLines, line)
	}

	if input.Offset > 0 {
		if input.Offset >= len(cleanLines) {
			cleanLines = nil
		} else {
			cleanLines = cleanLines[input.Offset:]
		}
	}

	if len(cleanLines) > lines {
		cleanLines = cleanLines[:lines]
	}

	hasMore := nextCursor != "" && len(cleanLines) >= lines

	return LogsOutput{
		Unit:       input.Unit,
		Lines:      lines,
		Offset:     input.Offset,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Logs:       strings.Join(cleanLines, "\n"),
	}, nil
}

func failedUnits(system bool) (FailedOutput, error) {
	out, primaryErr := failedUnitsViaDBus(system)
	if primaryErr == nil {
		return out, nil
	}

	out, fallbackErr := failedUnitsViaSystemctl(system)
	if fallbackErr == nil {
		return out, nil
	}
	return FailedOutput{}, newBackendFailure("systemd_failed", system, "dbus", primaryErr, "systemctl", fallbackErr)
}

func failedUnitsViaDBus(system bool) (FailedOutput, error) {
	sdb, err := requireDBusBackend(system)
	if err != nil {
		return FailedOutput{}, err
	}
	units, err := sdb.GetFailedUnits()
	if err != nil {
		return FailedOutput{}, fmt.Errorf("dbus failed units: %w", err)
	}
	raw, err := unitsToJSON(units)
	if err != nil {
		return FailedOutput{}, fmt.Errorf("marshal failed units: %w", err)
	}
	return FailedOutput{Units: raw}, nil
}

func failedUnitsViaSystemctl(system bool) (FailedOutput, error) {
	if err := requireSystemctlBackend(system); err != nil {
		return FailedOutput{}, err
	}
	out, err := runSystemctl(!system, "--failed", "--output=json", "--no-pager")
	if err != nil {
		return FailedOutput{}, fmt.Errorf("systemctl failed units: %w", err)
	}
	return FailedOutput{Units: ensureJSONArray(out)}, nil
}

var (
	writeRateLimitMu sync.Mutex
	lastWriteTimes   = make(map[string]time.Time)
	minWriteInterval = 1 * time.Second
)

// checkWriteRateLimit returns an error if a mutating action on the unit was performed too recently.
func checkWriteRateLimit(system bool, operation, unit string) error {
	if operation != "start" && operation != "stop" && operation != "restart" {
		return nil
	}

	writeRateLimitMu.Lock()
	defer writeRateLimitMu.Unlock()

	key := fmt.Sprintf("%t:%s", system, unit)
	now := time.Now()
	if lastTime, exists := lastWriteTimes[key]; exists {
		if elapsed := now.Sub(lastTime); elapsed < minWriteInterval {
			return fmt.Errorf("[%s] rate limit exceeded: %s on unit %s requested too rapidly (%v elapsed, min interval %v)",
				handler.ErrRateLimited, operation, unit, elapsed.Round(time.Millisecond), minWriteInterval)
		}
	}
	lastWriteTimes[key] = now
	return nil
}

// resetWriteRateLimits clears the in-memory write rate limiter state (useful in tests).
func resetWriteRateLimits() {
	writeRateLimitMu.Lock()
	defer writeRateLimitMu.Unlock()
	lastWriteTimes = make(map[string]time.Time)
}

func performWriteOperation(system bool, operation, unit, successMessage string, dbusAction func(*SystemdDBus) error, systemctlArgs ...string) (string, error) {
	if err := validateUnitName(unit); err != nil {
		return "", err
	}
	if err := checkWriteRateLimit(system, operation, unit); err != nil {
		return "", err
	}
	backend, primaryErr := performWriteViaDBus(system, operation, unit, successMessage, dbusAction)
	if primaryErr == nil {
		return backend, nil
	}
	if isUnitMissingErr(primaryErr) {
		return "", fmt.Errorf("[%s] unit %s not found", handler.ErrNotFound, unit)
	}

	backend, fallbackErr := performWriteViaSystemctl(system, operation, unit, successMessage, systemctlArgs...)
	if fallbackErr == nil {
		return backend, nil
	}
	if isUnitMissingErr(fallbackErr) {
		return "", fmt.Errorf("[%s] unit %s not found", handler.ErrNotFound, unit)
	}

	return "", newBackendFailure(operation, system, "dbus", primaryErr, "systemctl", fallbackErr)
}

func performWriteViaDBus(system bool, operation, unit, successMessage string, action func(*SystemdDBus) error) (string, error) {
	sdb, err := requireDBusBackend(system)
	if err != nil {
		return "", err
	}
	if err := action(sdb); err != nil {
		return "", fmt.Errorf("dbus %s %s: %w", operation, unit, err)
	}
	return "dbus", nil
}

func performWriteViaSystemctl(system bool, operation, unit, successMessage string, args ...string) (string, error) {
	if err := requireSystemctlBackend(system); err != nil {
		return "", err
	}
	writeArgs := append([]string{"--no-ask-password"}, args...)
	if _, err := runSystemctl(!system, writeArgs...); err != nil {
		return "", fmt.Errorf("systemctl %s %s: %w", operation, unit, err)
	}
	return "systemctl", nil
}

func startUnit(system bool, unit string) (StartOutput, error) {
	slog.Info("starting unit", "unit", unit, "system", system)
	backend, err := performWriteOperation(system, "systemd_start", unit, unit+" started", func(sdb *SystemdDBus) error {
		return sdb.StartUnit(unit, "replace")
	}, "start", unit)
	if err != nil {
		slog.Error("unit start failed", "unit", unit, "error", err)
		return StartOutput{}, err
	}
	slog.Info("unit started", "unit", unit, "via", backend)
	return StartOutput{Unit: unit, Message: unit + " started"}, nil
}

func restartUnit(system bool, unit string) (RestartOutput, error) {
	slog.Info("restarting unit", "unit", unit, "system", system)
	backend, err := performWriteOperation(system, "systemd_restart", unit, unit+" restarted", func(sdb *SystemdDBus) error {
		return sdb.RestartUnit(unit, "replace")
	}, "restart", unit)
	if err != nil {
		slog.Error("unit restart failed", "unit", unit, "error", err)
		return RestartOutput{}, err
	}
	slog.Info("unit restarted", "unit", unit, "via", backend)
	return RestartOutput{Unit: unit, Message: unit + " restarted"}, nil
}

func enableUnit(system bool, unit string) (EnableOutput, error) {
	slog.Info("enabling unit", "unit", unit, "system", system)
	backend, err := performWriteOperation(system, "systemd_enable", unit, unit+" enabled", func(sdb *SystemdDBus) error {
		return sdb.EnableUnit(unit)
	}, "enable", unit)
	if err != nil {
		slog.Error("unit enable failed", "unit", unit, "error", err)
		return EnableOutput{}, err
	}
	slog.Info("unit enabled", "unit", unit, "via", backend)
	return EnableOutput{Unit: unit, Message: unit + " enabled"}, nil
}

func stopUnit(system bool, unit string) (StopOutput, error) {
	slog.Info("stopping unit", "unit", unit, "system", system)
	backend, err := performWriteOperation(system, "systemd_stop", unit, unit+" stopped", func(sdb *SystemdDBus) error {
		return sdb.StopUnit(unit, "replace")
	}, "stop", unit)
	if err != nil {
		slog.Error("unit stop failed", "unit", unit, "error", err)
		return StopOutput{}, err
	}
	slog.Info("unit stopped", "unit", unit, "via", backend)
	return StopOutput{Unit: unit, Message: unit + " stopped"}, nil
}

func disableUnit(system bool, unit string) (DisableOutput, error) {
	slog.Info("disabling unit", "unit", unit, "system", system)
	backend, err := performWriteOperation(system, "systemd_disable", unit, unit+" disabled", func(sdb *SystemdDBus) error {
		return sdb.DisableUnit(unit)
	}, "disable", unit)
	if err != nil {
		slog.Error("unit disable failed", "unit", unit, "error", err)
		return DisableOutput{}, err
	}
	slog.Info("unit disabled", "unit", unit, "via", backend)
	return DisableOutput{Unit: unit, Message: unit + " disabled"}, nil
}

// ── systemd_investigate_unit ──────────────────────────────────────────────

func parseDependenciesOutput(out string) UnitDependencies {
	deps := UnitDependencies{}
	split := func(s string) []string {
		fields := strings.Fields(strings.TrimSpace(s))
		if len(fields) == 0 {
			return nil
		}
		return fields
	}
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]
		switch key {
		case "Requires":
			deps.Requires = split(val)
		case "Wants":
			deps.Wants = split(val)
		case "RequiredBy":
			deps.RequiredBy = split(val)
		case "WantedBy":
			deps.WantedBy = split(val)
		case "After":
			deps.After = split(val)
		case "Before":
			deps.Before = split(val)
		}
	}
	return deps
}

func readUnitDependencies(system bool, unit string) UnitDependencies {
	if err := requireSystemctlBackend(system); err != nil {
		return UnitDependencies{}
	}
	out, err := runSystemctl(!system, "show",
		"--property=Requires,Wants,RequiredBy,WantedBy,After,Before",
		unit,
	)
	if err != nil {
		return UnitDependencies{}
	}
	return parseDependenciesOutput(out)
}

func investigateUnit(system bool, unit string, logLines int) (InvestigateUnitOutput, error) {
	if err := validateUnitName(unit); err != nil {
		return InvestigateUnitOutput{}, err
	}
	if logLines <= 0 {
		logLines = 20
	}
	if logLines > 200 {
		logLines = 200
	}

	status, err := readUnitStatus(system, unit)
	if err != nil {
		return InvestigateUnitOutput{}, err
	}

	deps := readUnitDependencies(system, unit)

	logsOut, _ := readUnitLogs(LogsInput{
		Unit:   unit,
		System: system,
		Lines:  logLines,
	})

	return InvestigateUnitOutput{
		Unit:         unit,
		Status:       status,
		Dependencies: deps,
		RecentLogs:   logsOut.Logs,
	}, nil
}

// ---------------------------------------------------------------------------
// createTimer
// ---------------------------------------------------------------------------

func createTimer(input CreateTimerInput) (CreateTimerOutput, error) {
	if strings.TrimSpace(input.Command) == "" {
		return CreateTimerOutput{}, fmt.Errorf("[%s] command is required", handler.ErrInvalidParam)
	}
	if strings.TrimSpace(input.OnCalendar) == "" && strings.TrimSpace(input.OnBootSec) == "" && strings.TrimSpace(input.OnActiveSec) == "" {
		return CreateTimerOutput{}, fmt.Errorf("[%s] at least one timer schedule parameter must be provided (on_calendar, on_boot_sec, or on_active_sec)", handler.ErrInvalidParam)
	}

	unitName := strings.TrimSpace(input.Unit)
	if unitName != "" {
		unitName = strings.TrimSuffix(unitName, ".timer")
		unitName = strings.TrimSuffix(unitName, ".service")
		if err := validateUnitName(unitName + ".timer"); err != nil {
			return CreateTimerOutput{}, err
		}
	}

	cmdArgs := make([]string, 0, 10)
	if !input.System {
		cmdArgs = append(cmdArgs, "--user")
	}
	if unitName != "" {
		cmdArgs = append(cmdArgs, "--unit="+unitName)
	}
	if input.Description != "" {
		cmdArgs = append(cmdArgs, "--description="+input.Description)
	}
	if input.OnCalendar != "" {
		cmdArgs = append(cmdArgs, "--on-calendar="+input.OnCalendar)
	}
	if input.OnBootSec != "" {
		cmdArgs = append(cmdArgs, "--on-boot="+input.OnBootSec)
	}
	if input.OnActiveSec != "" {
		cmdArgs = append(cmdArgs, "--on-active="+input.OnActiveSec)
	}

	cmdArgs = append(cmdArgs, "/bin/sh", "-c", input.Command)

	stdout, stderr, err := runCmd("systemd-run", cmdArgs...)
	combined := strings.TrimSpace(stdout + "\n" + stderr)
	if err != nil {
		return CreateTimerOutput{}, fmt.Errorf("[%s] systemd-run failed: %s: %w", handler.ErrAPIError, combined, err)
	}

	timerUnit := unitName
	if timerUnit != "" {
		timerUnit += ".timer"
	} else {
		for _, line := range strings.Split(combined, "\n") {
			if strings.Contains(line, "Running timer as unit:") {
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					timerUnit = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	serviceUnit := strings.TrimSuffix(timerUnit, ".timer") + ".service"

	return CreateTimerOutput{
		TimerUnit:   timerUnit,
		ServiceUnit: serviceUnit,
		Created:     true,
		Output:      combined,
	}, nil
}

// ---------------------------------------------------------------------------
// analyzeSystemd
// ---------------------------------------------------------------------------

func analyzeSystemd(input AnalyzeInput) (AnalyzeOutput, error) {
	subcmd := strings.ToLower(strings.TrimSpace(input.Command))
	if subcmd == "" {
		subcmd = "blame"
	}

	cmdArgs := make([]string, 0, 6)
	cmdArgs = append(cmdArgs, subcmd)
	if !input.System {
		cmdArgs = append(cmdArgs, "--user")
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}

	unit := strings.TrimSpace(input.Unit)
	if unit != "" {
		if err := validateUnitName(unit); err != nil {
			return AnalyzeOutput{}, err
		}
		cmdArgs = append(cmdArgs, unit)
	}

	stdout, stderr, err := runCmd("systemd-analyze", cmdArgs...)
	combined := strings.TrimSpace(stdout + "\n" + stderr)
	if err != nil {
		return AnalyzeOutput{}, fmt.Errorf("[%s] systemd-analyze %s failed: %s: %w", handler.ErrAPIError, subcmd, combined, err)
	}

	out := AnalyzeOutput{
		Command:   subcmd,
		System:    input.System,
		RawOutput: combined,
	}

	switch subcmd {
	case "time":
		out.StartupTime = combined
	case "blame":
		lines := strings.Split(stdout, "\n")
		var entries []BlameEntry
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				u := parts[len(parts)-1]
				dur := strings.Join(parts[:len(parts)-1], " ")
				entries = append(entries, BlameEntry{
					Duration: dur,
					Unit:     u,
				})
				if len(entries) >= limit {
					break
				}
			}
		}
		out.Blame = entries
	case "critical-chain":
		out.CriticalChain = combined
	case "verify":
		out.Verification = combined
	}

	return out, nil
}
