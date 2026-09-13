package tmux

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func TestTemplate_Validation(t *testing.T) {
	td := findTool(t, "tmux_template")

	// Empty action
	res, err := td.Handler(context.Background(), makeReq(map[string]any{"action": ""}))
	if err == nil && (res == nil || !res.IsError) {
		t.Error("expected error for empty action")
	}

	// Unknown action
	res, err = td.Handler(context.Background(), makeReq(map[string]any{"action": "unknown_action"}))
	if err == nil && (res == nil || !res.IsError) {
		t.Error("expected error for unknown action")
	}

	// Save without name
	res, err = td.Handler(context.Background(), makeReq(map[string]any{"action": "save", "name": ""}))
	if err == nil && (res == nil || !res.IsError) {
		t.Error("expected error for save without name")
	}

	// Save without windows or session
	res, err = td.Handler(context.Background(), makeReq(map[string]any{"action": "save", "name": "my-tpl"}))
	if err == nil && (res == nil || !res.IsError) {
		t.Error("expected error for save without windows or session")
	}

	// Invalid name (path traversal / bad chars)
	for _, badName := range []string{"../bad", "bad/name", "name with spaces", "bad$name", strings.Repeat("a", 100)} {
		res, err = td.Handler(context.Background(), makeReq(map[string]any{"action": "save", "name": badName, "windows": []any{}}))
		if err == nil && (res == nil || !res.IsError) {
			t.Errorf("expected error for invalid name %q", badName)
		}
	}
}

func TestTemplate_SaveLoadListDelete(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMUX_MCP_TEMPLATE_DIR", tmpDir)

	td := findTool(t, "tmux_template")

	// 1. Initially empty list
	listRes, err := td.Handler(context.Background(), makeReq(map[string]any{"action": "list"}))
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	var listOut TemplateOutput
	unmarshalResult(t, listRes, &listOut)
	if len(listOut.Templates) != 0 {
		t.Errorf("expected 0 templates, got %d", len(listOut.Templates))
	}

	// 2. Save an explicit template
	saveReq := makeReq(map[string]any{
		"action":      "save",
		"name":        "dev-web",
		"description": "Full-stack development workspace",
		"dir":         "/tmp",
		"windows": []map[string]any{
			{
				"name":   "editor",
				"layout": "tiled",
				"panes": []map[string]any{
					{"command": "echo editor"},
				},
			},
			{
				"name":   "server",
				"layout": "even-horizontal",
				"panes": []map[string]any{
					{"command": "echo api"},
					{"command": "echo web"},
				},
			},
		},
	})
	saveRes, err := td.Handler(context.Background(), saveReq)
	if err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}
	var saveOut TemplateOutput
	unmarshalResult(t, saveRes, &saveOut)
	if !saveOut.Success {
		t.Error("expected save success")
	}
	if saveOut.Template == nil || saveOut.Template.Name != "dev-web" {
		t.Errorf("unexpected template in save output: %+v", saveOut.Template)
	}

	// 3. List should have 1 template
	listRes2, err := td.Handler(context.Background(), makeReq(map[string]any{"action": "list"}))
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	var listOut2 TemplateOutput
	unmarshalResult(t, listRes2, &listOut2)
	if len(listOut2.Templates) != 1 {
		t.Fatalf("expected 1 template in list, got %d", len(listOut2.Templates))
	}
	if listOut2.Templates[0].Name != "dev-web" {
		t.Errorf("expected template name dev-web, got %s", listOut2.Templates[0].Name)
	}
	if listOut2.Templates[0].WindowCount != 2 {
		t.Errorf("expected 2 windows, got %d", listOut2.Templates[0].WindowCount)
	}
	if listOut2.Templates[0].PaneCount != 3 {
		t.Errorf("expected 3 panes, got %d", listOut2.Templates[0].PaneCount)
	}

	// 4. Load the template
	loadRes, err := td.Handler(context.Background(), makeReq(map[string]any{
		"action": "load",
		"name":   "dev-web",
	}))
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}
	var loadOut TemplateOutput
	unmarshalResult(t, loadRes, &loadOut)
	if !loadOut.Success || loadOut.Template == nil {
		t.Fatal("expected successful load")
	}
	if len(loadOut.Template.Windows) != 2 {
		t.Errorf("expected 2 windows in loaded template, got %d", len(loadOut.Template.Windows))
	}

	// 5. Load nonexistent template
	loadBadRes, err := td.Handler(context.Background(), makeReq(map[string]any{
		"action": "load",
		"name":   "nonexistent-tpl",
	}))
	if err == nil && (loadBadRes == nil || !loadBadRes.IsError) {
		t.Error("expected error for nonexistent template")
	}

	// 6. Delete the template
	delRes, err := td.Handler(context.Background(), makeReq(map[string]any{
		"action": "delete",
		"name":   "dev-web",
	}))
	if err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
	var delOut TemplateOutput
	unmarshalResult(t, delRes, &delOut)
	if !delOut.Success {
		t.Error("expected delete success")
	}

	// 7. Verify deletion
	listRes3, _ := td.Handler(context.Background(), makeReq(map[string]any{"action": "list"}))
	var listOut3 TemplateOutput
	unmarshalResult(t, listRes3, &listOut3)
	if len(listOut3.Templates) != 0 {
		t.Errorf("expected 0 templates after deletion, got %d", len(listOut3.Templates))
	}
}

func TestTemplate_LiveApply(t *testing.T) {
	requireTmux(t)
	tmpDir := t.TempDir()
	t.Setenv("TMUX_MCP_TEMPLATE_DIR", tmpDir)

	td := findTool(t, "tmux_template")

	tplName := fmt.Sprintf("tpl-test-%d", rand.Intn(10000))
	sessName := fmt.Sprintf("sess-applied-%d", rand.Intn(10000))

	// Save template
	_, err := td.Handler(context.Background(), makeReq(map[string]any{
		"action": "save",
		"name":   tplName,
		"windows": []map[string]any{
			{
				"name":   "win1",
				"layout": "tiled",
				"panes": []map[string]any{
					{"command": "echo win1"},
				},
			},
		},
	}))
	if err != nil {
		t.Fatalf("save error: %v", err)
	}

	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	// Apply template
	applyRes, err := td.Handler(context.Background(), makeReq(map[string]any{
		"action":  "apply",
		"name":    tplName,
		"session": sessName,
	}))
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	var applyOut TemplateOutput
	unmarshalResult(t, applyRes, &applyOut)
	if !applyOut.Success || applyOut.Workspace == nil {
		t.Fatalf("expected apply success, got: %+v", applyOut)
	}
	if applyOut.Workspace.Session != sessName {
		t.Errorf("session = %s, want %s", applyOut.Workspace.Session, sessName)
	}

	// Verify session exists
	hasOut, err := runTmux("has-session", "-t", sessName)
	if err != nil {
		t.Errorf("has-session failed: %v, output: %s", err, hasOut)
	}
}
