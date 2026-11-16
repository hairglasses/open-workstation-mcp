package process

import (
	"fmt"
	"strings"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
)

type LivenessGateInput struct {
	PID              int     `json:"pid" jsonschema:"required,description=Process ID to assert liveness gates against"`
	MaxRssMB         float64 `json:"max_rss_mb,omitempty" jsonschema:"description=Threshold for resident memory in MB. Fails gate if RSS exceeds this."`
	MaxVszMB         float64 `json:"max_vsz_mb,omitempty" jsonschema:"description=Threshold for virtual memory in MB. Fails gate if VSZ exceeds this."`
	MaxFdCount       int     `json:"max_fd_count,omitempty" jsonschema:"description=Threshold for open file descriptors. Fails gate if FD count exceeds this."`
	MaxThreads       int     `json:"max_threads,omitempty" jsonschema:"description=Threshold for threads. Fails gate if thread count exceeds this."`
	MaxCpuPercent    float64 `json:"max_cpu_percent,omitempty" jsonschema:"description=Runaway CPU threshold. Fails gate if sampled CPU% exceeds this."`
	MinCpuTicksDelta uint64  `json:"min_cpu_ticks_delta,omitempty" jsonschema:"description=Stall detection threshold. Over sample window\\, fails gate if total ticks delta is below this."`
	SampleDurationMs int     `json:"sample_duration_ms,omitempty" jsonschema:"description=Sampling duration in ms for CPU metrics. Default 0 (or 100ms if CPU thresholds set). Max 1000."`
	DisallowZombie   *bool   `json:"disallow_zombie,omitempty" jsonschema:"description=Fail gate if process is in zombie state Z. Default true."`
	DisallowStall    *bool   `json:"disallow_stall,omitempty" jsonschema:"description=Fail gate if process is in uninterruptible disk sleep D. Default false."`
	CheckBootID      string  `json:"check_boot_id,omitempty" jsonschema:"description=Expected boot ID. Fails gate if current kernel boot ID does not match."`
	CheckStartTicks  uint64  `json:"check_start_ticks,omitempty" jsonschema:"description=Expected start ticks from /proc/<pid>/stat. Fails gate if PID has been recycled."`
}

type GateViolation struct {
	Gate     string `json:"gate"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Severity string `json:"severity"`
}

type LivenessMetrics struct {
	PID             int     `json:"pid"`
	Comm            string  `json:"comm"`
	State           string  `json:"state"`
	RSSMB           float64 `json:"rss_mb"`
	VIRTMB          float64 `json:"virt_mb"`
	FDCount         int     `json:"fd_count"`
	Threads         int     `json:"threads"`
	CpuPercent      float64 `json:"cpu_percent"`
	DeltaTotalTicks uint64  `json:"delta_total_ticks"`
}

type LivenessGateOutput struct {
	PID        int             `json:"pid"`
	Comm       string          `json:"comm"`
	Passed     bool            `json:"passed"`
	Status     string          `json:"status"`
	Violations []GateViolation `json:"violations"`
	Metrics    LivenessMetrics `json:"metrics"`
	Summary    string          `json:"summary"`
}

func EvaluateLiveness(input LivenessGateInput) (LivenessGateOutput, error) {
	if input.PID <= 0 {
		return LivenessGateOutput{}, fmt.Errorf("[%s] pid must be greater than 0", handler.ErrInvalidParam)
	}

	sampleMs := input.SampleDurationMs
	if sampleMs <= 0 && (input.MinCpuTicksDelta > 0 || input.MaxCpuPercent > 0) {
		sampleMs = 100
	}

	inspect, err := InspectProcess(ProcInspectInput{
		PID:              input.PID,
		SampleDurationMs: sampleMs,
	})
	if err != nil {
		return LivenessGateOutput{
			PID:    input.PID,
			Passed: false,
			Status: "FAILED",
			Violations: []GateViolation{
				{
					Gate:     "process_existence",
					Expected: "running",
					Actual:   "not found in /proc",
					Severity: "CRITICAL",
				},
			},
			Summary: fmt.Sprintf("Process %d not found or unreadable: %v", input.PID, err),
		}, nil
	}

	var violations []GateViolation

	if input.CheckBootID != "" {
		currentBootID, err := readBootID()
		if err != nil || currentBootID != input.CheckBootID {
			violations = append(violations, GateViolation{
				Gate:     "boot_id_match",
				Expected: input.CheckBootID,
				Actual:   currentBootID,
				Severity: "CRITICAL",
			})
		}
	}

	if input.CheckStartTicks > 0 {
		if inspect.Stat.StartTime != input.CheckStartTicks {
			violations = append(violations, GateViolation{
				Gate:     "start_ticks_match",
				Expected: fmt.Sprintf("%d", input.CheckStartTicks),
				Actual:   fmt.Sprintf("%d", inspect.Stat.StartTime),
				Severity: "CRITICAL",
			})
		}
	}

	disallowZombie := true
	if input.DisallowZombie != nil {
		disallowZombie = *input.DisallowZombie
	}
	if disallowZombie && inspect.State == "Z" {
		violations = append(violations, GateViolation{
			Gate:     "zombie_detection",
			Expected: "non-zombie",
			Actual:   "Z (zombie)",
			Severity: "CRITICAL",
		})
	}

	if input.DisallowStall != nil && *input.DisallowStall && inspect.State == "D" {
		violations = append(violations, GateViolation{
			Gate:     "disk_sleep_detection",
			Expected: "non-uninterruptible-sleep",
			Actual:   "D (disk sleep)",
			Severity: "WARNING",
		})
	}

	if input.MaxRssMB > 0 && inspect.RSSMB > input.MaxRssMB {
		violations = append(violations, GateViolation{
			Gate:     "rss_threshold",
			Expected: fmt.Sprintf("<= %.2f MB", input.MaxRssMB),
			Actual:   fmt.Sprintf("%.2f MB", inspect.RSSMB),
			Severity: "CRITICAL",
		})
	}

	if input.MaxVszMB > 0 && inspect.VIRTMB > input.MaxVszMB {
		violations = append(violations, GateViolation{
			Gate:     "vsz_threshold",
			Expected: fmt.Sprintf("<= %.2f MB", input.MaxVszMB),
			Actual:   fmt.Sprintf("%.2f MB", inspect.VIRTMB),
			Severity: "WARNING",
		})
	}

	if input.MaxFdCount > 0 && inspect.FDCount > input.MaxFdCount {
		violations = append(violations, GateViolation{
			Gate:     "fd_count_threshold",
			Expected: fmt.Sprintf("<= %d", input.MaxFdCount),
			Actual:   fmt.Sprintf("%d", inspect.FDCount),
			Severity: "CRITICAL",
		})
	}

	if input.MaxThreads > 0 && inspect.NumThreads > input.MaxThreads {
		violations = append(violations, GateViolation{
			Gate:     "thread_count_threshold",
			Expected: fmt.Sprintf("<= %d", input.MaxThreads),
			Actual:   fmt.Sprintf("%d", inspect.NumThreads),
			Severity: "WARNING",
		})
	}

	if input.MaxCpuPercent > 0 && inspect.CPUTelemetry.CPUPercent > input.MaxCpuPercent {
		violations = append(violations, GateViolation{
			Gate:     "cpu_runaway_threshold",
			Expected: fmt.Sprintf("<= %.1f%%", input.MaxCpuPercent),
			Actual:   fmt.Sprintf("%.1f%%", inspect.CPUTelemetry.CPUPercent),
			Severity: "WARNING",
		})
	}

	if input.MinCpuTicksDelta > 0 && inspect.CPUTelemetry.DeltaTotalTicks < input.MinCpuTicksDelta {
		violations = append(violations, GateViolation{
			Gate:     "cpu_stall_threshold",
			Expected: fmt.Sprintf(">= %d ticks", input.MinCpuTicksDelta),
			Actual:   fmt.Sprintf("%d ticks", inspect.CPUTelemetry.DeltaTotalTicks),
			Severity: "WARNING",
		})
	}

	metrics := LivenessMetrics{
		PID:             inspect.PID,
		Comm:            inspect.Comm,
		State:           inspect.State,
		RSSMB:           inspect.RSSMB,
		VIRTMB:          inspect.VIRTMB,
		FDCount:         inspect.FDCount,
		Threads:         inspect.NumThreads,
		CpuPercent:      inspect.CPUTelemetry.CPUPercent,
		DeltaTotalTicks: inspect.CPUTelemetry.DeltaTotalTicks,
	}

	if len(violations) == 0 {
		return LivenessGateOutput{
			PID:        inspect.PID,
			Comm:       inspect.Comm,
			Passed:     true,
			Status:     "HEALTHY",
			Violations: nil,
			Metrics:    metrics,
			Summary:    fmt.Sprintf("Process %d (%s) passed all liveness assertions", inspect.PID, inspect.Comm),
		}, nil
	}

	hasCritical := false
	var gateNames []string
	for _, v := range violations {
		if v.Severity == "CRITICAL" {
			hasCritical = true
		}
		gateNames = append(gateNames, v.Gate)
	}

	status := "DEGRADED"
	if hasCritical {
		status = "FAILED"
	}

	return LivenessGateOutput{
		PID:        inspect.PID,
		Comm:       inspect.Comm,
		Passed:     false,
		Status:     status,
		Violations: violations,
		Metrics:    metrics,
		Summary:    fmt.Sprintf("Process %d (%s) failed %d liveness assertions: %s", inspect.PID, inspect.Comm, len(violations), strings.Join(gateNames, ", ")),
	}, nil
}
