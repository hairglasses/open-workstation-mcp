package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
	"github.com/hairglasses/open-workstation-mcp/internal/workstationmcp"
)

func auditSchemaNode(node any, tool, path string) []string {
	sch, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	loc := path
	if loc == "" {
		loc = "."
	}
	var out []string
	kind, _ := sch["type"].(string)
	switch kind {
	case "array":
		_, hasItems := sch["items"]
		_, hasPrefix := sch["prefixItems"]
		if !hasItems && !hasPrefix {
			out = append(out, fmt.Sprintf("ARRAY_MISSING_ITEMS:%s:%s", tool, loc))
		}
	case "object":
		props, _ := sch["properties"].(map[string]any)
		if len(props) == 0 {
			out = append(out, fmt.Sprintf("OBJECT_MISSING_PROPERTIES:%s:%s", tool, loc))
		}
	case "":
		hasComb := false
		for _, k := range []string{"anyOf", "oneOf", "allOf", "$ref", "not", "$defs", "definitions", "const", "enum"} {
			if _, exists := sch[k]; exists {
				hasComb = true
				break
			}
		}
		if !hasComb {
			out = append(out, fmt.Sprintf("NODE_MISSING_TYPE:%s:%s", tool, loc))
		}
	}
	if props, ok := sch["properties"].(map[string]any); ok {
		if reqRaw, ok := sch["required"].([]any); ok {
			for _, r := range reqRaw {
				name, _ := r.(string)
				if _, exists := props[name]; !exists {
					out = append(out, fmt.Sprintf("REQUIRED_NOT_IN_PROPERTIES:%s:%s.%s", tool, path, name))
				}
			}
		}
		for name, sub := range props {
			out = append(out, auditSchemaNode(sub, tool, path+"."+name)...)
		}
	}
	if items, ok := sch["items"].(map[string]any); ok {
		out = append(out, auditSchemaNode(items, tool, path+"[]")...)
	} else if itemList, ok := sch["items"].([]any); ok {
		for i, sub := range itemList {
			out = append(out, auditSchemaNode(sub, tool, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	if ap, ok := sch["additionalProperties"].(map[string]any); ok {
		out = append(out, auditSchemaNode(ap, tool, path+".*")...)
	}
	for _, kw := range []string{"anyOf", "oneOf", "allOf"} {
		if subs, ok := sch[kw].([]any); ok {
			for i, sub := range subs {
				out = append(out, auditSchemaNode(sub, tool, fmt.Sprintf("%s(%s[%d])", path, kw, i))...)
			}
		}
	}
	for _, kw := range []string{"$defs", "definitions"} {
		if defs, ok := sch[kw].(map[string]any); ok {
			for name, sub := range defs {
				out = append(out, auditSchemaNode(sub, tool, fmt.Sprintf("%s#%s", path, name))...)
			}
		}
	}
	return out
}

func TestSchemaDetectorRedControl(t *testing.T) {
	type emptyRegressionInput struct{}
	type emptyRegressionOutput struct {
		OK bool `json:"ok"`
	}
	td := handler.TypedHandler[emptyRegressionInput, emptyRegressionOutput](
		"empty_regression_tool",
		"Red-control empty struct tool.",
		func(_ context.Context, _ emptyRegressionInput) (emptyRegressionOutput, error) {
			return emptyRegressionOutput{OK: true}, nil
		},
	)
	raw, err := json.Marshal(td.Tool)
	if err != nil {
		t.Fatalf("marshal red-control tool: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal red-control tool: %v", err)
	}
	viols := auditSchemaNode(doc["inputSchema"], td.Tool.Name, "")
	want := "OBJECT_MISSING_PROPERTIES:empty_regression_tool:."
	if !slices.Contains(viols, want) {
		t.Fatalf("red-control empty struct failed: got %v, want %q", viols, want)
	}

	nestedEmpty := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"args": map[string]any{"type": "object"},
		},
	}
	if got := auditSchemaNode(nestedEmpty, "neg_nested", ""); len(got) == 0 {
		t.Fatal("red-control failed to flag nested empty object")
	}

	missingItems := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ids": map[string]any{"type": "array"},
		},
	}
	if got := auditSchemaNode(missingItems, "neg_array", ""); len(got) == 0 {
		t.Fatal("red-control failed to flag array without items")
	}

	missingReq := map[string]any{
		"type":       "object",
		"properties": map[string]any{"a": map[string]any{"type": "string"}},
		"required":   []any{"b"},
	}
	if got := auditSchemaNode(missingReq, "neg_req", ""); len(got) == 0 {
		t.Fatal("red-control failed to flag required field absent from properties")
	}
}

func TestAllRegisteredToolsZeroSchemaAdvisories(t *testing.T) {
	reg := BuildRegistry()
	if got := reg.ToolCount(); got != 57 {
		t.Fatalf("expected 57 tools, got %d", got)
	}

	var violations []string
	for _, name := range reg.ListTools() {
		td, ok := reg.GetTool(name)
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		raw, err := json.Marshal(td.Tool)
		if err != nil {
			t.Fatalf("%s marshal tool: %v", name, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s unmarshal tool: %v", name, err)
		}
		inputSchema := doc["inputSchema"]
		if inputSchema == nil {
			t.Fatalf("%s missing inputSchema", name)
		}
		violations = append(violations, auditSchemaNode(inputSchema, name, "")...)
	}
	if len(violations) > 0 {
		t.Fatalf("registry schema violations:\n%s", strings.Join(violations, "\n"))
	}

	manifest := workstationmcp.SampleGateway().Manifest()
	if len(manifest.Tools) != 5 {
		t.Fatalf("expected 5 sample gateway tools, got %d", len(manifest.Tools))
	}
	for _, tool := range manifest.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("sample %s marshal InputSchema: %v", tool.Name, err)
		}
		var sch any
		if err := json.Unmarshal(raw, &sch); err != nil {
			t.Fatalf("sample %s unmarshal InputSchema: %v", tool.Name, err)
		}
		if viols := auditSchemaNode(sch, tool.Name, ""); len(viols) > 0 {
			t.Fatalf("sample %s schema violations: %v", tool.Name, viols)
		}
	}
}

func TestWireToolsListZeroSchemaAdvisories(t *testing.T) {
	reg := BuildRegistry()
	s := registry.NewMCPServer("open-workstation-mcp", "1.0.0")
	reg.RegisterWithServer(s)

	ctx := context.Background()
	initReq := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"schema-audit","version":"1"}}}`)
	if resp := s.HandleMessage(ctx, initReq); resp == nil {
		t.Fatal("nil initialize response")
	}
	initNotify := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	_ = s.HandleMessage(ctx, initNotify)

	listReq := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	listResp := s.HandleMessage(ctx, listReq)
	if listResp == nil {
		t.Fatal("nil tools/list response")
	}
	raw, err := json.Marshal(listResp)
	if err != nil {
		t.Fatalf("marshal tools/list response: %v", err)
	}
	var payload struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal tools/list response: %v", err)
	}
	if len(payload.Result.Tools) != 57 {
		t.Fatalf("wire tools/list count = %d, want 57", len(payload.Result.Tools))
	}
	var violations []string
	for _, tool := range payload.Result.Tools {
		name, _ := tool["name"].(string)
		if name == "" {
			t.Fatalf("tool missing name: %v", tool)
		}
		violations = append(violations, auditSchemaNode(tool["inputSchema"], name, "")...)
	}
	if len(violations) > 0 {
		t.Fatalf("wire tools/list schema violations:\n%s", strings.Join(violations, "\n"))
	}
}
