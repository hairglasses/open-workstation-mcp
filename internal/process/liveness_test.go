package process

import (
	"os"
	"testing"
)

func TestEvaluateLiveness_SelfHealthy(t *testing.T) {
	pid := os.Getpid()
	out, err := EvaluateLiveness(LivenessGateInput{
		PID:              pid,
		MaxRssMB:         4096.0,
		MaxVszMB:         16384.0,
		MaxFdCount:       10000,
		MaxThreads:       500,
		SampleDurationMs: 10,
	})
	if err != nil {
		t.Fatalf("EvaluateLiveness failed: %v", err)
	}

	if !out.Passed {
		t.Errorf("expected Passed=true, got false with violations: %+v", out.Violations)
	}
	if out.Status != "HEALTHY" {
		t.Errorf("expected Status=HEALTHY, got %q", out.Status)
	}
	if len(out.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(out.Violations))
	}
	if out.Metrics.RSSMB <= 0 {
		t.Error("expected positive RSSMB")
	}
	if out.Metrics.FDCount <= 0 {
		t.Error("expected positive FDCount")
	}
}

func TestEvaluateLiveness_ProcessNotFound(t *testing.T) {
	out, err := EvaluateLiveness(LivenessGateInput{
		PID: 999999999,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.Passed {
		t.Error("expected Passed=false for non-existent process")
	}
	if out.Status != "FAILED" {
		t.Errorf("expected Status=FAILED, got %q", out.Status)
	}
	if len(out.Violations) == 0 || out.Violations[0].Gate != "process_existence" {
		t.Errorf("expected process_existence violation, got %+v", out.Violations)
	}
}

func TestEvaluateLiveness_RssThresholdViolation(t *testing.T) {
	pid := os.Getpid()
	out, err := EvaluateLiveness(LivenessGateInput{
		PID:      pid,
		MaxRssMB: 0.0001,
	})
	if err != nil {
		t.Fatalf("EvaluateLiveness failed: %v", err)
	}

	if out.Passed {
		t.Error("expected Passed=false on RSS limit breach")
	}
	if out.Status != "FAILED" {
		t.Errorf("expected Status=FAILED, got %q", out.Status)
	}

	found := false
	for _, v := range out.Violations {
		if v.Gate == "rss_threshold" {
			found = true
			if v.Severity != "CRITICAL" {
				t.Errorf("expected Severity=CRITICAL, got %q", v.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("missing rss_threshold violation in %+v", out.Violations)
	}
}

func TestEvaluateLiveness_FdThresholdViolation(t *testing.T) {
	pid := os.Getpid()
	out, err := EvaluateLiveness(LivenessGateInput{
		PID:        pid,
		MaxFdCount: 1,
	})
	if err != nil {
		t.Fatalf("EvaluateLiveness failed: %v", err)
	}

	if out.Passed {
		t.Error("expected Passed=false on FD limit breach")
	}

	found := false
	for _, v := range out.Violations {
		if v.Gate == "fd_count_threshold" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("missing fd_count_threshold violation in %+v", out.Violations)
	}
}

func TestEvaluateLiveness_ThreadsThresholdViolation(t *testing.T) {
	pid := os.Getpid()
	out, err := EvaluateLiveness(LivenessGateInput{
		PID:        pid,
		MaxThreads: 1,
	})
	if err != nil {
		t.Fatalf("EvaluateLiveness failed: %v", err)
	}

	if out.Passed {
		t.Error("expected Passed=false on threads limit breach")
	}

	found := false
	for _, v := range out.Violations {
		if v.Gate == "thread_count_threshold" {
			found = true
			if v.Severity != "WARNING" {
				t.Errorf("expected Severity=WARNING, got %q", v.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("missing thread_count_threshold violation in %+v", out.Violations)
	}
}

func TestEvaluateLiveness_BootIDMismatch(t *testing.T) {
	pid := os.Getpid()
	out, err := EvaluateLiveness(LivenessGateInput{
		PID:         pid,
		CheckBootID: "fake-boot-id-0000-1111-2222",
	})
	if err != nil {
		t.Fatalf("EvaluateLiveness failed: %v", err)
	}

	if out.Passed {
		t.Error("expected Passed=false on BootID mismatch")
	}

	found := false
	for _, v := range out.Violations {
		if v.Gate == "boot_id_match" {
			found = true
			if v.Severity != "CRITICAL" {
				t.Errorf("expected Severity=CRITICAL, got %q", v.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("missing boot_id_match violation in %+v", out.Violations)
	}
}

func TestEvaluateLiveness_StartTicksMismatch(t *testing.T) {
	pid := os.Getpid()
	out, err := EvaluateLiveness(LivenessGateInput{
		PID:             pid,
		CheckStartTicks: 999999999999,
	})
	if err != nil {
		t.Fatalf("EvaluateLiveness failed: %v", err)
	}

	if out.Passed {
		t.Error("expected Passed=false on StartTicks mismatch")
	}

	found := false
	for _, v := range out.Violations {
		if v.Gate == "start_ticks_match" {
			found = true
			if v.Severity != "CRITICAL" {
				t.Errorf("expected Severity=CRITICAL, got %q", v.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("missing start_ticks_match violation in %+v", out.Violations)
	}
}

func TestEvaluateLiveness_Validation(t *testing.T) {
	_, err := EvaluateLiveness(LivenessGateInput{PID: 0})
	if err == nil {
		t.Error("expected error for PID=0")
	}

	_, err = EvaluateLiveness(LivenessGateInput{PID: -10})
	if err == nil {
		t.Error("expected error for negative PID")
	}
}
