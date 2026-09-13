package systemd

import (
	"encoding/json"
	"testing"
)

func TestStructuredOutput_AsUnits(t *testing.T) {
	// 1. Valid array of units
	raw := `[
		{"unit": "nginx.service", "description": "A high performance web server", "load": "loaded", "active": "active", "sub": "running"},
		{"unit": "dbus.service", "description": "D-Bus System Message Bus", "load": "loaded", "active": "active", "sub": "running"}
	]`

	out := ListUnitsOutput{Units: json.RawMessage(raw)}
	units, err := out.AsUnits()
	if err != nil {
		t.Fatalf("unexpected error parsing units: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}
	if units[0].Unit != "nginx.service" || units[0].ActiveState != "active" {
		t.Errorf("unexpected unit[0] values: %+v", units[0])
	}
	if units[1].Unit != "dbus.service" || units[1].Description != "D-Bus System Message Bus" {
		t.Errorf("unexpected unit[1] values: %+v", units[1])
	}

	// 2. Empty raw message
	emptyOut := ListUnitsOutput{}
	emptyUnits, err := emptyOut.AsUnits()
	if err != nil {
		t.Fatalf("unexpected error on empty output: %v", err)
	}
	if len(emptyUnits) != 0 {
		t.Errorf("expected 0 units for empty output, got %d", len(emptyUnits))
	}

	// 3. FailedOutput.AsUnits
	failedOut := FailedOutput{Units: json.RawMessage(raw)}
	failedUnits, err := failedOut.AsUnits()
	if err != nil {
		t.Fatalf("unexpected error on failed output: %v", err)
	}
	if len(failedUnits) != 2 {
		t.Errorf("expected 2 units, got %d", len(failedUnits))
	}
}

func TestStructuredOutput_AsTimers(t *testing.T) {
	raw := `[
		{"unit": "systemd-tmpfiles-clean.timer", "next": "Tue 2026-09-08 00:00:00 UTC", "last": "Mon 2026-09-07 00:00:00 UTC", "activates": "systemd-tmpfiles-clean.service"}
	]`

	out := ListTimersOutput{Timers: json.RawMessage(raw)}
	timers, err := out.AsTimers()
	if err != nil {
		t.Fatalf("unexpected error parsing timers: %v", err)
	}
	if len(timers) != 1 {
		t.Fatalf("expected 1 timer, got %d", len(timers))
	}
	if timers[0].Unit != "systemd-tmpfiles-clean.timer" {
		t.Errorf("unexpected timer unit: %s", timers[0].Unit)
	}

	// Empty raw message
	emptyOut := ListTimersOutput{}
	emptyTimers, err := emptyOut.AsTimers()
	if err != nil {
		t.Fatalf("unexpected error on empty output: %v", err)
	}
	if len(emptyTimers) != 0 {
		t.Errorf("expected 0 timers for empty output, got %d", len(emptyTimers))
	}
}

func TestEnsureJSONArray(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "[]"},
		{"   ", "[]"},
		{"invalid-json", "[]"},
		{"[]", "[]"},
		{"[{\"unit\":\"foo.service\"}]", "[{\"unit\":\"foo.service\"}]"},
	}

	for _, tc := range cases {
		got := string(ensureJSONArray(tc.input))
		if got != tc.want {
			t.Errorf("ensureJSONArray(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
