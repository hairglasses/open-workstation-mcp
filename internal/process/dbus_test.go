package process

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestProbeDbus_Validation(t *testing.T) {
	_, err := ProbeDbus(DbusProbeInput{Probe: ""})
	if err == nil {
		t.Error("expected error for empty probe type")
	}

	_, err = ProbeDbus(DbusProbeInput{Probe: "invalid_probe_type"})
	if err == nil {
		t.Error("expected error for invalid probe type")
	}

	_, err = ProbeDbus(DbusProbeInput{Probe: "unit_state", Unit: ""})
	if err == nil {
		t.Error("expected error for unit_state without unit")
	}
}

func TestProbeDbus_DaemonLifecycle_SystemBus(t *testing.T) {
	out, err := ProbeDbus(DbusProbeInput{
		Bus:   "system",
		Probe: "daemon_lifecycle",
	})
	if err != nil {
		t.Fatalf("ProbeDbus failed: %v", err)
	}

	if !out.Success {
		t.Skipf("System bus unavailable or unprivileged in environment: %s", out.Error)
		return
	}

	if out.DaemonLifecycle == nil {
		t.Fatal("expected non-nil DaemonLifecycle")
	}
	if out.DaemonLifecycle.Architecture == "" {
		t.Error("expected non-empty Architecture")
	}
	if out.DaemonLifecycle.SystemState == "" {
		t.Error("expected non-empty SystemState")
	}
	if len(out.Properties) == 0 {
		t.Error("expected non-empty Properties map")
	}
}

func TestProbeDbus_UnitState_SystemBus(t *testing.T) {
	out, err := ProbeDbus(DbusProbeInput{
		Bus:   "system",
		Probe: "unit_state",
		Unit:  "dbus.service",
	})
	if err != nil {
		t.Fatalf("ProbeDbus failed: %v", err)
	}

	if !out.Success {
		t.Skipf("System bus unavailable or unprivileged in environment: %s", out.Error)
		return
	}

	if out.UnitState == nil {
		t.Fatal("expected non-nil UnitState")
	}
	if out.UnitState.Unit != "dbus.service" {
		t.Errorf("expected unit=dbus.service, got %q", out.UnitState.Unit)
	}
	if out.UnitState.ActiveState == "" {
		t.Error("expected non-empty ActiveState")
	}
}

func TestProbeDbus_ListUnits_SystemBus(t *testing.T) {
	out, err := ProbeDbus(DbusProbeInput{
		Bus:      "system",
		Probe:    "list_units",
		Patterns: []string{"*.service"},
	})
	if err != nil {
		t.Fatalf("ProbeDbus failed: %v", err)
	}

	if !out.Success {
		t.Skipf("System bus unavailable or unprivileged in environment: %s", out.Error)
		return
	}

	if len(out.Units) == 0 {
		t.Error("expected at least one unit in list_units output")
	}
	for _, u := range out.Units {
		if u.Unit == "" {
			t.Error("expected non-empty unit name")
		}
	}
}

func TestProbeDbus_Properties_SystemBus(t *testing.T) {
	out, err := ProbeDbus(DbusProbeInput{
		Bus:      "system",
		Probe:    "properties",
		Property: "Version",
	})
	if err != nil {
		t.Fatalf("ProbeDbus failed: %v", err)
	}

	if !out.Success {
		t.Skipf("System bus unavailable or unprivileged in environment: %s", out.Error)
		return
	}

	if out.Properties == nil || out.Properties["Version"] == nil {
		t.Errorf("expected Version property in output, got: %+v", out.Properties)
	}
}

func TestNormalizeVariantValue(t *testing.T) {
	vStr := dbus.MakeVariant("hello")
	gotStr := normalizeVariantValue(vStr)
	if gotStr != "hello" {
		t.Errorf("expected 'hello', got %v", gotStr)
	}

	vNested := dbus.MakeVariant(dbus.MakeVariant(uint32(42)))
	gotNested := normalizeVariantValue(vNested)
	if gotNested != uint32(42) {
		t.Errorf("expected uint32(42), got %v", gotNested)
	}

	vPath := dbus.ObjectPath("/org/freedesktop/systemd1")
	gotPath := normalizeVariantValue(vPath)
	if gotPath != "/org/freedesktop/systemd1" {
		t.Errorf("expected path string, got %v", gotPath)
	}

	vSig, _ := dbus.ParseSignature("a{sv}")
	gotSig := normalizeVariantValue(vSig)
	if gotSig == "" {
		t.Error("expected non-empty signature string")
	}

	vSlice := []dbus.Variant{dbus.MakeVariant(1), dbus.MakeVariant(2)}
	gotSlice := normalizeVariantValue(vSlice)
	if s, ok := gotSlice.([]any); !ok || len(s) != 2 {
		t.Errorf("expected []any with 2 elements, got %v", gotSlice)
	}

	vMap := map[string]dbus.Variant{"k": dbus.MakeVariant("val")}
	gotMap := normalizeVariantValue(vMap)
	if m, ok := gotMap.(map[string]any); !ok || m["k"] != "val" {
		t.Errorf("expected map[k:val], got %v", gotMap)
	}
}
