package process

import (
	"context"
	"strings"
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

func TestKillProcess_BlocksGhosttyUSR1(t *testing.T) {
	mod := &ProcessModule{}
	var killTD registry.ToolDefinition
	found := false
	for _, td := range mod.Tools() {
		if td.Tool.Name == "kill_process" {
			killTD = td
			found = true
			break
		}
	}
	if !found {
		t.Fatal("kill_process tool not found")
	}

	req, err := registry.NewCallToolRequest("kill_process", map[string]any{
		"pid":           999991,
		"signal":        "USR1",
		"expected_comm": "ghostty",
	})
	if err != nil {
		t.Fatalf("NewCallToolRequest: %v", err)
	}

	res, callErr := killTD.Handler(context.Background(), req)
	if callErr != nil {
		t.Fatalf("unexpected handler error: %v", callErr)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for ghostty USR1, got %+v", res)
	}
	text, _ := registry.ExtractTextContent(res.Content[0])
	if !strings.Contains(text, "USR2") || !strings.Contains(text, "ghostty") {
		t.Fatalf("expected Ghostty USR2 remediation message, got: %s", text)
	}
}
