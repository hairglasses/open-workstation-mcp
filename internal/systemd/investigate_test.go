package systemd

import (
	"context"
	"strings"
	"testing"

	"github.com/hairglasses-studio/mcpkit/mcptest"
	"github.com/hairglasses-studio/mcpkit/registry"
)

func TestParseDependenciesOutput(t *testing.T) {
	sampleShow := `Requires=pipewire.socket dbus-broker.service session.slice
Wants=wireplumber.service
RequiredBy=
WantedBy=pipewire-pulse.service default.target
Before=wireplumber.service shutdown.target
After=pipewire.socket session.slice
`
	deps := parseDependenciesOutput(sampleShow)

	if len(deps.Requires) != 3 {
		t.Errorf("expected 3 requires, got %v", deps.Requires)
	}
	if len(deps.Wants) != 1 || deps.Wants[0] != "wireplumber.service" {
		t.Errorf("expected Wants [wireplumber.service], got %v", deps.Wants)
	}
	if len(deps.RequiredBy) != 0 {
		t.Errorf("expected empty RequiredBy, got %v", deps.RequiredBy)
	}
	if len(deps.WantedBy) != 2 {
		t.Errorf("expected 2 WantedBy, got %v", deps.WantedBy)
	}
	if len(deps.Before) != 2 {
		t.Errorf("expected 2 Before, got %v", deps.Before)
	}
	if len(deps.After) != 2 {
		t.Errorf("expected 2 After, got %v", deps.After)
	}
}

func TestInvestigateUnit_InvalidName(t *testing.T) {
	_, err := investigateUnit(false, "../evil.service", 20)
	if err == nil {
		t.Fatal("expected error for path traversal unit name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid unit name") {
		t.Errorf("expected invalid unit name error, got %v", err)
	}
}

func TestInvestigateUnit_Nonexistent(t *testing.T) {
	_, err := investigateUnit(false, "nonexistent-unit-for-testing-12345.service", 20)
	if err == nil {
		t.Fatal("expected error for nonexistent unit, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got %v", err)
	}
}

func TestInvestigateUnit_ServerEndToEnd(t *testing.T) {
	reg := registry.NewToolRegistry()
	reg.RegisterModule(&SystemdModule{})
	srv := mcptest.NewServer(t, reg)
	ctx := context.Background()

	tool, ok := srv.Registry.GetTool("systemd_investigate_unit")
	if !ok {
		t.Fatal("tool systemd_investigate_unit not found in registry")
	}

	// Invoke with invalid unit name
	req1 := makeReq(map[string]any{"unit": "; rm -rf /"})
	res1, err := tool.Handler(ctx, req1)
	if err != nil {
		t.Fatalf("unexpected call error: %v", err)
	}
	if !res1.IsError {
		t.Fatal("expected tool error for dangerous unit name")
	}

	// Invoke with nonexistent unit
	req2 := makeReq(map[string]any{"unit": "nonexistent-test-probe.service"})
	res2, err := tool.Handler(ctx, req2)
	if err != nil {
		t.Fatalf("unexpected call error: %v", err)
	}
	if !res2.IsError {
		t.Fatal("expected tool error for nonexistent unit")
	}
}
