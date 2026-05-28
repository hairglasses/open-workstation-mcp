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
	if len(manifest.Tools) != 4 {
		t.Fatalf("tool count = %d", len(manifest.Tools))
	}
	for _, tool := range manifest.Tools {
		if tool.Name == "" || tool.InputSchema["type"] != "object" {
			t.Fatalf("invalid tool metadata: %+v", tool)
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
