package workstationmcp

import (
	"context"
	"testing"
)

func TestSampleGatewayManifest(t *testing.T) {
	manifest := SampleGateway().Manifest()
	if manifest.Name != "open-workstation-mcp" {
		t.Fatalf("manifest name = %q", manifest.Name)
	}
	if len(manifest.Tools) != 5 {
		t.Fatalf("tool count = %d", len(manifest.Tools))
	}
	for _, tool := range manifest.Tools {
		if tool.Name == "" || tool.InputSchema["type"] != "object" {
			t.Fatalf("invalid tool metadata: %+v", tool)
		}
	}
}

func TestOmarchyControlPlanFlavorAndGuards(t *testing.T) {
	gw := SampleGateway()

	headlessResp, err := gw.Call(context.Background(), Request{
		Tool:   "omarchy_control_plan",
		Params: map[string]string{"action": "restart_terminal", "flavor": "headless"},
	})
	if err != nil {
		t.Fatalf("headless Call() error = %v", err)
	}
	if headlessResp.Payload["status"] != "SKIPPED_HEADLESS" {
		t.Fatalf("expected SKIPPED_HEADLESS, got %+v", headlessResp.Payload)
	}

	niriResp, err := gw.Call(context.Background(), Request{
		Tool:   "omarchy_control_plan",
		Params: map[string]string{"action": "restart_terminal", "flavor": "niri"},
	})
	if err != nil {
		t.Fatalf("niri Call() error = %v", err)
	}
	if niriResp.Payload["status"] != "SKIPPED_NIRI" {
		t.Fatalf("expected SKIPPED_NIRI, got %+v", niriResp.Payload)
	}

	hyprResp, err := gw.Call(context.Background(), Request{
		Tool:   "omarchy_control_plan",
		Params: map[string]string{"action": "debug", "flavor": "hyprland"},
	})
	if err != nil {
		t.Fatalf("hyprland Call() error = %v", err)
	}
	if hyprResp.Payload["clipboard_policy"] != "clipse" || hyprResp.Payload["ghostty_reload_signal"] != "SIGUSR2" {
		t.Fatalf("unexpected policy fields: %+v", hyprResp.Payload)
	}

	for _, badTarget := range []string{"wl-copy", "wl-paste", "SIGUSR1", "#000000", "cyberpunk"} {
		if _, berr := gw.Call(context.Background(), Request{
			Tool:   "omarchy_control_plan",
			Params: map[string]string{"action": "theme_set", "flavor": "hyprland", "target": badTarget},
		}); berr == nil {
			t.Fatalf("expected error for forbidden target %q", badTarget)
		}
	}
}

func TestDryRunPolicyBlocksExecution(t *testing.T) {
	resp, err := SampleGateway().Call(context.Background(), Request{Tool: "safe_input_plan", Params: map[string]string{"target": "search", "text": "hello", "execute": "true"}})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if !resp.DryRun {
		t.Fatal("expected dry-run response")
	}
	if len(resp.Warnings) == 0 || len(resp.NextSteps) == 0 {
		t.Fatalf("expected dry-run warning and next step: %+v", resp)
	}
}

func TestUnknownToolReturnsError(t *testing.T) {
	_, err := SampleGateway().Call(context.Background(), Request{Tool: "missing", Params: map[string]string{}})
	if err == nil {
		t.Fatal("expected unknown tool error")
	}
}
