package process

import (
	"testing"
)

func TestInvestigatePort_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "investigate_port" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			return
		}
	}
	t.Fatalf("investigate_port not found in module tools")
}

func TestInvestigateService_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "investigate_service" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			return
		}
	}
	t.Fatalf("investigate_service not found in module tools")
}

func TestInvestigateMemory_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "investigate_memory" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			if td.Category != "composed" {
				t.Errorf("expected Category composed, got %s", td.Category)
			}
			return
		}
	}
	t.Fatalf("investigate_memory not found in module tools")
}

func TestInvestigateCpu_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "investigate_cpu" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			if td.Category != "composed" {
				t.Errorf("expected Category composed, got %s", td.Category)
			}
			return
		}
	}
	t.Fatalf("investigate_cpu not found in module tools")
}

func TestProcInspect_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "proc_inspect" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			if td.Category != "inspection" {
				t.Errorf("expected Category inspection, got %s", td.Category)
			}
			return
		}
	}
	t.Fatalf("proc_inspect not found in module tools")
}

func TestLivenessGate_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "liveness_gate" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			if td.Category != "safety" {
				t.Errorf("expected Category safety, got %s", td.Category)
			}
			return
		}
	}
	t.Fatalf("liveness_gate not found in module tools")
}

func TestDbusProbe_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "dbus_probe" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			if td.Category != "inspection" {
				t.Errorf("expected Category inspection, got %s", td.Category)
			}
			return
		}
	}
	t.Fatalf("dbus_probe not found in module tools")
}
