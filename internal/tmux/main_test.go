package tmux

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/mcptest"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func TestModuleRegistration(t *testing.T) {
	m := &TmuxModule{}
	tools := m.Tools()
	if len(tools) != 14 {
		t.Fatalf("expected 14 tools, got %d", len(tools))
	}

	reg := registry.NewToolRegistry()
	reg.RegisterModule(m)
	srv := mcptest.NewServer(t, reg)

	names := srv.ToolNames()
	if len(names) != 14 {
		t.Fatalf("expected 14 registered tools, got %d", len(names))
	}

	for _, want := range []string{
		"tmux_list_sessions", "tmux_new_session", "tmux_kill_session",
		"tmux_list_windows", "tmux_new_window",
		"tmux_list_panes", "tmux_capture_pane", "tmux_send_keys",
		"tmux_workspace", "tmux_wait_for_text", "tmux_search_panes",
		"tmux_resize_pane", "tmux_snapshot", "tmux_template",
	} {
		if !srv.HasTool(want) {
			t.Errorf("missing tool: %s", want)
		}
	}
}

func TestContextRegistries(t *testing.T) {
	if got := buildTmuxResourceRegistry().ResourceCount(); got != 1 {
		t.Fatalf("expected 1 tmux resource, got %d", got)
	}
	if got := buildTmuxPromptRegistry().PromptCount(); got != 1 {
		t.Fatalf("expected 1 tmux prompt, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Helper unit tests
// ---------------------------------------------------------------------------

func TestIsNoServer(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("no server running on /tmp/tmux"), true},
		{fmt.Errorf("no current client"), true},
		{fmt.Errorf("error connecting to /tmp/tmux"), true},
		{fmt.Errorf("session not found"), false},
		{fmt.Errorf("some other error"), false},
	}

	for _, tc := range tests {
		got := isNoServer(tc.err)
		if got != tc.want {
			t.Errorf("isNoServer(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestTmuxTarget(t *testing.T) {
	tests := []struct {
		session, window, pane string
		want                  string
	}{
		{"sess", "", "", "sess"},
		{"sess", "1", "", "sess:1"},
		{"sess", "1", "0", "sess:1.0"},
		{"sess", "", "0", "sess.0"},
		{"my-session", "main", "2", "my-session:main.2"},
	}

	for _, tc := range tests {
		got := tmuxTarget(tc.session, tc.window, tc.pane)
		if got != tc.want {
			t.Errorf("tmuxTarget(%q, %q, %q) = %q, want %q",
				tc.session, tc.window, tc.pane, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// List sessions (graceful when no server)
// ---------------------------------------------------------------------------

func TestListSessions_GracefulNoServer(t *testing.T) {
	requireTmux(t)

	// This test works whether or not tmux is running — it should not error
	td := findTool(t, "tmux_list_sessions")
	req := makeReq(nil)
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("expected graceful handling, got error: %v", err)
	}

	var out ListSessionsOutput
	unmarshalResult(t, result, &out)
	if out.Sessions == nil {
		t.Error("sessions should be non-nil (empty slice)")
	}
}

// ---------------------------------------------------------------------------
// Session lifecycle (create → list → windows → panes → capture → send → kill)
// ---------------------------------------------------------------------------

func TestSessionLifecycle(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-mcp-%d", rand.Intn(100000))

	// Create session
	newTd := findTool(t, "tmux_new_session")
	req := makeReq(map[string]any{"name": sessName})
	result, err := newTd.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("new_session error: %v", err)
	}
	var newOut NewSessionOutput
	unmarshalResult(t, result, &newOut)
	if !newOut.Created {
		t.Error("expected created=true")
	}

	// Cleanup: kill session at end
	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		killReq := makeReq(map[string]any{"name": sessName})
		_, _ = killTd.Handler(context.Background(), killReq)
	}()

	// List sessions — should include our session
	listTd := findTool(t, "tmux_list_sessions")
	result, err = listTd.Handler(context.Background(), makeReq(nil))
	if err != nil {
		t.Fatalf("list_sessions error: %v", err)
	}
	var listOut ListSessionsOutput
	unmarshalResult(t, result, &listOut)
	found := false
	for _, s := range listOut.Sessions {
		if s.Name == sessName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("session %q not found in list", sessName)
	}

	// List windows
	winTd := findTool(t, "tmux_list_windows")
	result, err = winTd.Handler(context.Background(), makeReq(map[string]any{"session": sessName}))
	if err != nil {
		t.Fatalf("list_windows error: %v", err)
	}
	var winOut ListWindowsOutput
	unmarshalResult(t, result, &winOut)
	if len(winOut.Windows) == 0 {
		t.Error("expected at least 1 window")
	}

	// List panes
	paneTd := findTool(t, "tmux_list_panes")
	result, err = paneTd.Handler(context.Background(), makeReq(map[string]any{"session": sessName}))
	if err != nil {
		t.Fatalf("list_panes error: %v", err)
	}
	var paneOut ListPanesOutput
	unmarshalResult(t, result, &paneOut)
	if len(paneOut.Panes) == 0 {
		t.Error("expected at least 1 pane")
	}

	// Send keys
	sendTd := findTool(t, "tmux_send_keys")
	result, err = sendTd.Handler(context.Background(), makeReq(map[string]any{
		"session": sessName,
		"keys":    "echo hello-mcp-test",
	}))
	if err != nil {
		t.Fatalf("send_keys error: %v", err)
	}
	var sendOut SendKeysOutput
	unmarshalResult(t, result, &sendOut)
	if sendOut.Sent != "echo hello-mcp-test" {
		t.Errorf("expected sent=%q, got %q", "echo hello-mcp-test", sendOut.Sent)
	}

	// Capture pane
	capTd := findTool(t, "tmux_capture_pane")
	result, err = capTd.Handler(context.Background(), makeReq(map[string]any{
		"session": sessName,
		"lines":   10,
	}))
	if err != nil {
		t.Fatalf("capture_pane error: %v", err)
	}
	var capOut CaptureOutput
	unmarshalResult(t, result, &capOut)
	// Content may or may not contain our text depending on timing

	// New window
	newWinTd := findTool(t, "tmux_new_window")
	result, err = newWinTd.Handler(context.Background(), makeReq(map[string]any{
		"session": sessName,
		"name":    "test-win",
	}))
	if err != nil {
		t.Fatalf("new_window error: %v", err)
	}
	var newWinOut NewWindowOutput
	unmarshalResult(t, result, &newWinOut)
	if newWinOut.Session != sessName {
		t.Errorf("expected session=%q, got %q", sessName, newWinOut.Session)
	}

	// Kill session (explicit)
	killTd := findTool(t, "tmux_kill_session")
	result, err = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	if err != nil {
		t.Fatalf("kill_session error: %v", err)
	}
	var killOut KillSessionOutput
	unmarshalResult(t, result, &killOut)
	if !killOut.Killed {
		t.Error("expected killed=true")
	}
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestListWindows_BadSession(t *testing.T) {
	requireTmux(t)

	td := findTool(t, "tmux_list_windows")
	req := makeReq(map[string]any{"session": "nonexistent-session-xyz-99999"})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		// Error is acceptable — session doesn't exist
		return
	}
	// Typed handler wraps errors as IsError results
	if result != nil && result.IsError {
		return
	}
	// If no error, should return empty windows (graceful)
	var out ListWindowsOutput
	unmarshalResult(t, result, &out)
	if len(out.Windows) != 0 {
		t.Errorf("expected 0 windows for nonexistent session, got %d", len(out.Windows))
	}
}

func TestCapturePane_DefaultLines(t *testing.T) {
	// Verify the handler defaults lines=50 when 0 is provided
	// We can't easily test this without a running session, so just verify
	// the code path doesn't panic with lines=0
	requireTmux(t)

	sessName := fmt.Sprintf("test-cap-%d", rand.Intn(100000))
	newTd := findTool(t, "tmux_new_session")
	_, err := newTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	if err != nil {
		t.Skipf("could not create test session: %v", err)
	}
	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	td := findTool(t, "tmux_capture_pane")
	req := makeReq(map[string]any{"session": sessName, "lines": 0})
	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("capture with lines=0 error: %v", err)
	}
	var out CaptureOutput
	unmarshalResult(t, result, &out)
	// Should have captured something (default 50 lines)
}

// ---------------------------------------------------------------------------
// tmux_workspace (composed)
// ---------------------------------------------------------------------------

func TestWorkspace_EmptySession(t *testing.T) {
	td := findTool(t, "tmux_workspace")
	req := makeReq(map[string]any{"session": "", "windows": []any{}})
	result, err := td.Handler(context.Background(), req)
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("expected error for empty session")
	}
}

func TestWorkspace_NoWindows(t *testing.T) {
	td := findTool(t, "tmux_workspace")
	req := makeReq(map[string]any{"session": "test", "windows": []any{}})
	result, err := td.Handler(context.Background(), req)
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("expected error for no windows")
	}
}

func TestWorkspace_MultiPane(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-ws-%d", rand.Intn(100000))
	td := findTool(t, "tmux_workspace")

	req := makeReq(map[string]any{
		"session": sessName,
		"dir":     "/tmp",
		"windows": []any{
			map[string]any{
				"name":   "editor",
				"layout": "even-horizontal",
				"panes": []any{
					map[string]any{"command": "echo pane1"},
					map[string]any{"command": "echo pane2"},
				},
			},
			map[string]any{
				"name": "logs",
				"panes": []any{
					map[string]any{"command": "echo logs"},
				},
			},
		},
	})

	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("workspace error: %v", err)
	}

	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	var out WorkspaceOutput
	unmarshalResult(t, result, &out)
	if out.Session != sessName {
		t.Errorf("expected session=%q, got %q", sessName, out.Session)
	}
	if out.WindowCount != 2 {
		t.Errorf("expected 2 windows, got %d", out.WindowCount)
	}
	if out.PaneCount != 3 {
		t.Errorf("expected 3 panes, got %d", out.PaneCount)
	}
}

func TestBuildSplitArgs(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		pane     WorkspacePane
		baseDir  string
		expected []string
	}{
		{
			name:     "default empty pane with baseDir",
			target:   "sess:0",
			pane:     WorkspacePane{},
			baseDir:  "/workspace",
			expected: []string{"split-window", "-t", "sess:0", "-c", "/workspace"},
		},
		{
			name:     "no baseDir and no pane Dir",
			target:   "sess:0",
			pane:     WorkspacePane{},
			baseDir:  "",
			expected: []string{"split-window", "-t", "sess:0"},
		},
		{
			name:     "horizontal split short form",
			target:   "sess:0",
			pane:     WorkspacePane{Split: "h"},
			baseDir:  "/tmp",
			expected: []string{"split-window", "-t", "sess:0", "-h", "-c", "/tmp"},
		},
		{
			name:     "horizontal split long form case insensitive",
			target:   "sess:0",
			pane:     WorkspacePane{Split: "  HoRiZonTal  "},
			baseDir:  "/tmp",
			expected: []string{"split-window", "-t", "sess:0", "-h", "-c", "/tmp"},
		},
		{
			name:     "vertical split short form",
			target:   "sess:0",
			pane:     WorkspacePane{Split: "v"},
			baseDir:  "/tmp",
			expected: []string{"split-window", "-t", "sess:0", "-v", "-c", "/tmp"},
		},
		{
			name:     "vertical split long form case insensitive",
			target:   "sess:0",
			pane:     WorkspacePane{Split: "  VeRtiCal  "},
			baseDir:  "/tmp",
			expected: []string{"split-window", "-t", "sess:0", "-v", "-c", "/tmp"},
		},
		{
			name:     "size percentage with percent sign",
			target:   "sess:0",
			pane:     WorkspacePane{Split: "h", Size: "30%"},
			baseDir:  "",
			expected: []string{"split-window", "-t", "sess:0", "-h", "-p", "30"},
		},
		{
			name:     "size percentage with spaces",
			target:   "sess:0",
			pane:     WorkspacePane{Split: "v", Size: " 45% "},
			baseDir:  "",
			expected: []string{"split-window", "-t", "sess:0", "-v", "-p", "45"},
		},
		{
			name:     "size cells fixed count",
			target:   "sess:0",
			pane:     WorkspacePane{Split: "h", Size: "25"},
			baseDir:  "",
			expected: []string{"split-window", "-t", "sess:0", "-h", "-l", "25"},
		},
		{
			name:     "pane Dir overrides baseDir",
			target:   "sess:0",
			pane:     WorkspacePane{Split: "v", Size: "20%", Dir: "/custom/dir"},
			baseDir:  "/fallback/dir",
			expected: []string{"split-window", "-t", "sess:0", "-v", "-p", "20", "-c", "/custom/dir"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildSplitArgs(tc.target, tc.pane, tc.baseDir)
			if !slices.Equal(got, tc.expected) {
				t.Errorf("buildSplitArgs() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestWorkspace_SplitAndSize(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-ws-split-%d", rand.Intn(100000))
	td := findTool(t, "tmux_workspace")

	req := makeReq(map[string]any{
		"session": sessName,
		"dir":     "/tmp",
		"windows": []any{
			map[string]any{
				"name": "split-win",
				"panes": []any{
					map[string]any{"command": "echo primary"},
					map[string]any{"command": "echo side", "split": "horizontal", "size": "30%"},
					map[string]any{"command": "echo bottom", "split": "vertical", "size": "20%"},
				},
			},
		},
	})

	result, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("workspace error: %v", err)
	}

	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	var out WorkspaceOutput
	unmarshalResult(t, result, &out)
	if out.Session != sessName {
		t.Errorf("expected session=%q, got %q", sessName, out.Session)
	}
	if out.WindowCount != 1 {
		t.Errorf("expected 1 window, got %d", out.WindowCount)
	}
	if out.PaneCount != 3 {
		t.Errorf("expected 3 panes, got %d", out.PaneCount)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func requireTmux(t *testing.T) {
	// Tests own every pane they create; opt in to the send-keys guard
	// (2026-09-06 echo-storm rule) so typing into test shells is allowed.
	t.Setenv("FLEET_TMUX_SHELL_OK", "1")
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
}

func findTool(t *testing.T, name string) registry.ToolDefinition {
	t.Helper()
	m := &TmuxModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == name {
			return td
		}
	}
	t.Fatalf("tool %q not found", name)
	return registry.ToolDefinition{}
}

// makeReq/extractText go through mcpkit's registry.NewCallToolRequest /
// mcptest.ExtractText compat helpers instead of a zero-value-then-field-write
// CallToolRequest and a raw registry.TextContent type assertion: the official
// SDK's Params is a nilable pointer (zero-value + field write panics) and its
// TextContent is pointer-typed (the value-typed assertion below fails to
// compile), so this must go through the SDK-neutral accessors to build once
// for both build tags.
func makeReq(args map[string]any) registry.CallToolRequest {
	if args == nil {
		args = map[string]any{}
	}
	req, err := registry.NewCallToolRequest("", args)
	if err != nil {
		panic("makeReq: " + err.Error())
	}
	return req
}

func extractText(t *testing.T, result *registry.CallToolResult) string {
	t.Helper()
	return mcptest.ExtractText(t, result)
}

func unmarshalResult(t *testing.T, result *registry.CallToolResult, out any) {
	t.Helper()
	text := extractText(t, result)
	if err := json.Unmarshal([]byte(text), out); err != nil {
		t.Fatalf("unmarshal error: %v; text=%s", err, text)
	}
}

// ---------------------------------------------------------------------------
// Safety tier metadata
// ---------------------------------------------------------------------------

func TestSafetyTiers(t *testing.T) {
	m := &TmuxModule{}
	tools := m.Tools()

	tierMap := make(map[string]registry.ToolDefinition)
	for _, td := range tools {
		tierMap[td.Tool.Name] = td
	}

	readOnly := []string{
		"tmux_list_sessions", "tmux_list_windows", "tmux_list_panes",
		"tmux_capture_pane", "tmux_wait_for_text", "tmux_search_panes",
	}
	for _, name := range readOnly {
		td, ok := tierMap[name]
		if !ok {
			t.Errorf("tool %q not found", name)
			continue
		}
		if td.IsWrite {
			t.Errorf("%s: expected IsWrite=false, got true", name)
		}
	}

	writable := []string{
		"tmux_send_keys", "tmux_new_session", "tmux_new_window",
		"tmux_kill_session", "tmux_workspace", "tmux_resize_pane",
	}
	for _, name := range writable {
		td, ok := tierMap[name]
		if !ok {
			t.Errorf("tool %q not found", name)
			continue
		}
		if !td.IsWrite {
			t.Errorf("%s: expected IsWrite=true, got false", name)
		}
	}

	// kill_session should be complex
	if td, ok := tierMap["tmux_kill_session"]; ok {
		if td.Complexity != registry.ComplexityComplex {
			t.Errorf("tmux_kill_session: expected Complexity=%q, got %q",
				registry.ComplexityComplex, td.Complexity)
		}
	}
}

// ---------------------------------------------------------------------------
// tmux_wait_for_text
// ---------------------------------------------------------------------------

func TestWaitForText_MissingText(t *testing.T) {
	td := findTool(t, "tmux_wait_for_text")
	req := makeReq(map[string]any{"session": "test", "text": ""})
	result, err := td.Handler(context.Background(), req)
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("expected error for empty text")
	}
}

func TestWaitForText_Found(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-wait-%d", rand.Intn(100000))
	newTd := findTool(t, "tmux_new_session")
	_, err := newTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	if err != nil {
		t.Skipf("could not create session: %v", err)
	}
	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	// Send text then wait for it — use a single send_keys with Enter included
	sendTd := findTool(t, "tmux_send_keys")
	_, _ = sendTd.Handler(context.Background(), makeReq(map[string]any{
		"session": sessName,
		"keys":    "echo MARKER_FOUND_XYZ",
	}))
	_, _ = sendTd.Handler(context.Background(), makeReq(map[string]any{
		"session": sessName,
		"keys":    "Enter",
	}))

	// Give tmux time to execute the command
	time.Sleep(500 * time.Millisecond)

	td := findTool(t, "tmux_wait_for_text")
	result, err := td.Handler(context.Background(), makeReq(map[string]any{
		"session":      sessName,
		"text":         "MARKER_FOUND_XYZ",
		"timeout_secs": 10,
	}))
	if err != nil {
		t.Fatalf("wait_for_text error: %v", err)
	}
	if result.IsError {
		t.Fatalf("wait_for_text returned error: %s", extractText(t, result))
	}
	var out WaitForTextOutput
	unmarshalResult(t, result, &out)
	if !out.Found {
		t.Error("expected found=true")
	}
	if !strings.Contains(out.MatchingLine, "MARKER_FOUND_XYZ") {
		t.Errorf("matching line should contain marker, got %q", out.MatchingLine)
	}
}

// ---------------------------------------------------------------------------
// tmux_search_panes
// ---------------------------------------------------------------------------

func TestSearchPanes_MissingSession(t *testing.T) {
	td := findTool(t, "tmux_search_panes")
	req := makeReq(map[string]any{"session": "", "pattern": "test"})
	result, err := td.Handler(context.Background(), req)
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("expected error for empty session")
	}
}

func TestSearchPanes_InvalidRegex(t *testing.T) {
	td := findTool(t, "tmux_search_panes")
	req := makeReq(map[string]any{"session": "test", "pattern": "[invalid"})
	result, err := td.Handler(context.Background(), req)
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("expected error for invalid regex")
	}
}

func TestSearchPanes_Found(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-search-%d", rand.Intn(100000))
	newTd := findTool(t, "tmux_new_session")
	_, err := newTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	if err != nil {
		t.Skipf("could not create session: %v", err)
	}
	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	// Send identifiable text
	sendTd := findTool(t, "tmux_send_keys")
	_, _ = sendTd.Handler(context.Background(), makeReq(map[string]any{
		"session": sessName,
		"keys":    "echo SEARCH_MARKER_ABC123",
	}))
	_, _ = sendTd.Handler(context.Background(), makeReq(map[string]any{
		"session": sessName,
		"keys":    "Enter",
	}))

	td := findTool(t, "tmux_search_panes")
	var out SearchPanesOutput
	deadline := time.Now().Add(1 * time.Second)
	for {
		result, err := td.Handler(context.Background(), makeReq(map[string]any{
			"session": sessName,
			"pattern": "SEARCH_MARKER_ABC123",
		}))
		if err == nil {
			var cur SearchPanesOutput
			unmarshalResult(t, result, &cur)
			if len(cur.Matches) > 0 {
				out = cur
				break
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(out.Matches) == 0 {
		t.Error("expected at least one match")
	}
}

func TestFormatTmuxError(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("can't find session: foo"), "session not found"},
		{fmt.Errorf("window not found: bar"), "window not found"},
		{fmt.Errorf("can't find pane: 0"), "pane not found"},
		{fmt.Errorf("no server running on /tmp/tmux-1000/default"), "no tmux server is running"},
		{fmt.Errorf("other unexpected error"), "other unexpected error"},
	}

	for _, tc := range cases {
		got := formatTmuxError("test_op", tc.err)
		if got == nil || !strings.Contains(got.Error(), tc.want) {
			t.Errorf("formatTmuxError() = %v, want substring %q", got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// tmux_resize_pane
// ---------------------------------------------------------------------------

func TestResizePane_Validation(t *testing.T) {
	td := findTool(t, "tmux_resize_pane")

	// 1. Missing session name
	t.Run("missing_session", func(t *testing.T) {
		req := makeReq(map[string]any{"session": "", "direction": "U"})
		res, err := td.Handler(context.Background(), req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected error for empty session")
		}
	})

	// 2. Missing all resize params
	t.Run("missing_resize_params", func(t *testing.T) {
		req := makeReq(map[string]any{"session": "test-session"})
		res, err := td.Handler(context.Background(), req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected error when no resize parameters are given")
		}
	})

	// 3. Invalid direction
	t.Run("invalid_direction", func(t *testing.T) {
		req := makeReq(map[string]any{
			"session":   "test-session",
			"direction": "INVALID_DIR",
		})
		res, err := td.Handler(context.Background(), req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected error for invalid direction")
		}
	})
}

func TestResizePane_Execution(t *testing.T) {
	requireTmux(t)

	sessName := fmt.Sprintf("test-resize-%d", rand.Intn(100000))
	newTd := findTool(t, "tmux_new_session")
	_, err := newTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	if err != nil {
		t.Skipf("could not create session: %v", err)
	}
	defer func() {
		killTd := findTool(t, "tmux_kill_session")
		_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": sessName}))
	}()

	// Split pane so resizing has effect
	_, _ = runTmux("split-window", "-t", sessName)

	td := findTool(t, "tmux_resize_pane")

	// Test directional resize with adjustment
	t.Run("resize_direction", func(t *testing.T) {
		req := makeReq(map[string]any{
			"session":    sessName,
			"direction":  "D",
			"adjustment": 5,
		})
		res, err := td.Handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("resize_pane returned error: %s", extractText(t, res))
		}
		var out ResizePaneOutput
		unmarshalResult(t, res, &out)
		if !out.Resized {
			t.Error("expected resized=true")
		}
	})

	// Test zoom toggle
	t.Run("resize_zoom", func(t *testing.T) {
		req := makeReq(map[string]any{
			"session": sessName,
			"zoom":    true,
		})
		res, err := td.Handler(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("resize_pane zoom returned error: %s", extractText(t, res))
		}
		var out ResizePaneOutput
		unmarshalResult(t, res, &out)
		if !out.Resized {
			t.Error("expected resized=true")
		}
	})
}

func schemaAdvisories(node any, path string) []string {
	var issues []string
	m, ok := node.(map[string]any)
	if !ok {
		if arr, ok := node.([]any); ok {
			for i, item := range arr {
				issues = append(issues, schemaAdvisories(item, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
		return issues
	}
	if typ, _ := m["type"].(string); typ == "object" {
		props, _ := m["properties"].(map[string]any)
		if len(props) == 0 {
			issues = append(issues, fmt.Sprintf("%s: object missing properties", path))
		}
	} else if typ == "array" {
		if _, hasItems := m["items"]; !hasItems {
			issues = append(issues, fmt.Sprintf("%s: array missing items", path))
		}
	}
	for _, key := range []string{"properties", "$defs", "definitions"} {
		if sub, ok := m[key].(map[string]any); ok {
			for k, v := range sub {
				issues = append(issues, schemaAdvisories(v, path+"."+k)...)
			}
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		if sub, ok := m[key].(map[string]any); ok {
			issues = append(issues, schemaAdvisories(sub, path+"."+key)...)
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := m[key].([]any); ok {
			for i, v := range arr {
				issues = append(issues, schemaAdvisories(v, fmt.Sprintf("%s.%s[%d]", path, key, i))...)
			}
		}
	}
	return issues
}

func TestAllToolSchemasHaveNonEmptyObjectProperties(t *testing.T) {
	if got := schemaAdvisories(map[string]any{"type": "object"}, "."); len(got) == 0 {
		t.Fatal("negative control failed: expected advisory for empty object schema")
	}
	m := &TmuxModule{}
	for _, td := range m.Tools() {
		raw, err := json.Marshal(td.Tool)
		if err != nil {
			t.Fatalf("marshal %s: %v", td.Tool.Name, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("unmarshal %s: %v", td.Tool.Name, err)
		}
		inputSchema := doc["inputSchema"]
		if inputSchema == nil {
			t.Fatalf("%s missing inputSchema", td.Tool.Name)
		}
		if issues := schemaAdvisories(inputSchema, td.Tool.Name); len(issues) > 0 {
			t.Fatalf("%s schema advisories: %v", td.Tool.Name, issues)
		}
	}
}

func TestListSessions_Filters(t *testing.T) {
	requireTmux(t)
	newTd := findTool(t, "tmux_new_session")
	listTd := findTool(t, "tmux_list_sessions")
	killTd := findTool(t, "tmux_kill_session")

	prefix := fmt.Sprintf("filter-alpha-%d-", rand.Intn(100000))
	alpha := prefix + "one"
	beta := fmt.Sprintf("filter-beta-%d", rand.Intn(100000))
	for _, name := range []string{alpha, beta} {
		if res, err := newTd.Handler(context.Background(), makeReq(map[string]any{"name": name})); err != nil || (res != nil && res.IsError) {
			t.Fatalf("new_session(%s): err=%v res=%v", name, err, res)
		}
		defer func(n string) {
			_, _ = killTd.Handler(context.Background(), makeReq(map[string]any{"name": n}))
		}(name)
	}

	resAll, err := listTd.Handler(context.Background(), makeReq(map[string]any{}))
	if err != nil || resAll.IsError {
		t.Fatalf("list_sessions default: err=%v res=%v", err, resAll)
	}
	var allOut ListSessionsOutput
	unmarshalResult(t, resAll, &allOut)
	seenAlpha := false
	seenBeta := false
	for _, s := range allOut.Sessions {
		if s.Name == alpha {
			seenAlpha = true
		}
		if s.Name == beta {
			seenBeta = true
		}
	}
	if !seenAlpha || !seenBeta {
		t.Fatalf("expected both %s and %s in default list, got %+v", alpha, beta, allOut.Sessions)
	}

	resAlpha, err := listTd.Handler(context.Background(), makeReq(map[string]any{"prefix": prefix}))
	if err != nil || resAlpha.IsError {
		t.Fatalf("list_sessions prefix: err=%v res=%v", err, resAlpha)
	}
	var alphaOut ListSessionsOutput
	unmarshalResult(t, resAlpha, &alphaOut)
	if len(alphaOut.Sessions) != 1 || alphaOut.Sessions[0].Name != alpha {
		t.Fatalf("expected only [%s], got %+v", alpha, alphaOut.Sessions)
	}

	resAttached, err := listTd.Handler(context.Background(), makeReq(map[string]any{"attached_only": true}))
	if err != nil || resAttached.IsError {
		t.Fatalf("list_sessions attached_only: err=%v res=%v", err, resAttached)
	}
	var attachedOut ListSessionsOutput
	unmarshalResult(t, resAttached, &attachedOut)
	if len(attachedOut.Sessions) != 0 {
		t.Fatalf("expected 0 attached sessions, got %+v", attachedOut.Sessions)
	}
}
