// Command tmux-mcp is an MCP server for tmux session, window, and pane
// management via the Model Context Protocol (stdio transport).
//
// Usage:
//
//	tmux-mcp
package tmux

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/middleware/gate"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var defaultTmuxCmdTimeout = 15 * time.Second

func runCmd(name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTmuxCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func runTmux(args ...string) (string, error) {
	args = anchorTargetArg(args)
	stdout, stderr, err := runCmd("tmux", args...)
	if err != nil {
		return "", fmt.Errorf("tmux %s: %s: %w", strings.Join(args, " "), stderr, err)
	}
	return stdout, nil
}

// anchorTargetArg rewrites the value of the first "-t" flag (before any "--")
// through exactTarget, so every tmux call made via runTmux resolves its target
// by exact name instead of tmux's prefix-then-fnmatch fallback. Centralising it
// here covers composite "session:window" targets built at the call sites too.
func anchorTargetArg(args []string) []string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--" {
			return args
		}
		if args[i] == "-t" {
			out := append([]string(nil), args...)
			out[i+1] = exactTarget(args[i+1])
			return out
		}
	}
	return args
}

// isNoServer returns true if the error indicates tmux has no running server.
func isNoServer(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no server running") ||
		strings.Contains(msg, "no current client") ||
		strings.Contains(msg, "error connecting")
}

// formatTmuxError formats tmux CLI errors with specific, helpful context for missing sessions, windows, or panes.
func formatTmuxError(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case isNoServer(err):
		return fmt.Errorf("[%s] %s: no tmux server is running", handler.ErrNotFound, op)
	case strings.Contains(msg, "can't find session") || strings.Contains(msg, "session not found"):
		return fmt.Errorf("[%s] %s: session not found: %w", handler.ErrNotFound, op, err)
	case strings.Contains(msg, "can't find window") || strings.Contains(msg, "window not found"):
		return fmt.Errorf("[%s] %s: window not found: %w", handler.ErrNotFound, op, err)
	case strings.Contains(msg, "can't find pane") || strings.Contains(msg, "pane not found"):
		return fmt.Errorf("[%s] %s: pane not found: %w", handler.ErrNotFound, op, err)
	default:
		return fmt.Errorf("[%s] %s: %w", handler.ErrNotFound, op, err)
	}
}

// tmuxTarget builds a tmux target string like "session:window.pane".
func tmuxTarget(session, window, pane string) string {
	t := session
	if window != "" {
		t += ":" + window
	}
	if pane != "" {
		t += "." + pane
	}
	return t
}

// exactTarget anchors a tmux target string for exact-match resolution
// instead of tmux's default prefix-then-fnmatch(3) session/window/pane name
// lookup. Without this, "kill-session -t agent-1" (or any bare-name target)
// can resolve against an unrelated session whose name merely starts with
// "agent-1" — the root cause behind cross-session tmux operations landing on
// the wrong session.
//
// Forms handled:
//   - "" (empty)            -> "" (caller must reject empty separately;
//     see validateKillSessionName for the one tool where an empty target is
//     itself a hazard, not just a no-op)
//   - already "=foo"        -> "=foo:"
//   - tmux ids "$1"/"@2"/"%3" -> unchanged        (already unique/exact)
//   - bare "session"        -> "=session:"
//   - "session:window"      -> "=session:window"
//   - "session:window.pane" -> "=session:window.pane"
//
// The "=" prefix on a compound target anchors the session-name component to
// an exact match per tmux's target grammar (cmd_find.c); window/pane
// components of an already-existing session are addressed by index or id,
// which tmux always treats as exact.
func exactTarget(target string) string {
	if target == "" {
		return target
	}
	switch target[0] {
	case '$', '@', '%':
		return target
	}
	if target[0] != '=' {
		target = "=" + target
	}
	// A bare "=name" is only valid for session-target commands: tmux 3.7c
	// rejects it for pane/window targets ("can't find pane: =name"). "=name:"
	// is exact for session, window and pane targets alike.
	if !strings.Contains(target, ":") {
		target += ":"
	}
	return target
}

// validateKillSessionName rejects tmux_kill_session inputs that are unsafe
// regardless of exactTarget's "=" anchoring: an empty name (tmux's CLI parses
// a missing/empty -t as "most recently used session" rather than failing,
// so an empty string here must never reach `tmux kill-session -t`) and any
// fnmatch(3) glob metacharacter, which signals a caller expecting
// prefix/pattern matching semantics this tool deliberately never offers.
func validateKillSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("[%s] session name is required", handler.ErrInvalidParam)
	}
	if strings.ContainsAny(name, "*?[]") {
		return fmt.Errorf("[%s] session name %q must not contain glob characters (* ? [ ]); tmux_kill_session requires an exact session name", handler.ErrInvalidParam, name)
	}
	return nil
}

// ---------------------------------------------------------------------------
// I/O types
// ---------------------------------------------------------------------------

// --- list_sessions ---

type ListSessionsInput struct {
	Prefix       string `json:"prefix,omitempty" jsonschema:"description=Optional session name prefix filter"`
	AttachedOnly bool   `json:"attached_only,omitempty" jsonschema:"description=When true return only attached sessions"`
}

type sessionInfo struct {
	Name     string `json:"name"`
	Windows  int    `json:"windows"`
	Created  string `json:"created"`
	Attached bool   `json:"attached"`
}

type ListSessionsOutput struct {
	Sessions []sessionInfo `json:"sessions"`
}

// --- list_windows ---

type ListWindowsInput struct {
	Session string `json:"session" jsonschema:"required,description=Tmux session name"`
}

type windowInfo struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Panes  int    `json:"panes"`
	Active bool   `json:"active"`
	Layout string `json:"layout"`
}

type ListWindowsOutput struct {
	Session string       `json:"session"`
	Windows []windowInfo `json:"windows"`
}

// --- list_panes ---

type ListPanesInput struct {
	Session string `json:"session" jsonschema:"required,description=Tmux session name"`
	Window  string `json:"window,omitempty" jsonschema:"description=Window index or name"`
}

type paneInfo struct {
	Index   int    `json:"index"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Command string `json:"command"`
	Active  bool   `json:"active"`
}

type ListPanesOutput struct {
	Target string     `json:"target"`
	Panes  []paneInfo `json:"panes"`
}

// --- capture_pane ---

type CaptureInput struct {
	Session   string `json:"session" jsonschema:"required,description=Tmux session name"`
	Window    string `json:"window,omitempty" jsonschema:"description=Window index or name"`
	Pane      string `json:"pane,omitempty" jsonschema:"description=Pane index"`
	Lines     int    `json:"lines,omitempty" jsonschema:"description=Number of lines to capture. Default 50."`
	StartLine string `json:"start_line,omitempty" jsonschema:"description=Starting line in scrollback buffer (e.g. '-100' or '-' for start of history)"`
	EndLine   string `json:"end_line,omitempty" jsonschema:"description=Ending line in scrollback buffer (e.g. '0' or 'e' for end of pane)"`
}

type CaptureOutput struct {
	Target  string `json:"target"`
	Content string `json:"content"`
}

// --- resize_pane ---

type ResizePaneInput struct {
	Session    string `json:"session" jsonschema:"required,description=Tmux session name"`
	Window     string `json:"window,omitempty" jsonschema:"description=Window index or name"`
	Pane       string `json:"pane,omitempty" jsonschema:"description=Pane index"`
	Direction  string `json:"direction,omitempty" jsonschema:"description=Resize direction: U (up)\\, D (down)\\, L (left)\\, R (right)"`
	Adjustment int    `json:"adjustment,omitempty" jsonschema:"description=Adjustment amount in cells when resizing in a direction"`
	Width      int    `json:"width,omitempty" jsonschema:"description=Absolute width in cells (-x)"`
	Height     int    `json:"height,omitempty" jsonschema:"description=Absolute height in cells (-y)"`
	Zoom       bool   `json:"zoom,omitempty" jsonschema:"description=Toggle pane zoom state (-Z)"`
}

type ResizePaneOutput struct {
	Target  string `json:"target"`
	Resized bool   `json:"resized"`
}

// --- send_keys ---

type SendKeysInput struct {
	Session string `json:"session" jsonschema:"required,description=Tmux session name"`
	Window  string `json:"window,omitempty" jsonschema:"description=Window index or name"`
	Pane    string `json:"pane,omitempty" jsonschema:"description=Pane index"`
	Keys    string `json:"keys" jsonschema:"required,description=Keys to send. Literal text or tmux key names like Enter or C-c."`
}

type SendKeysOutput struct {
	Target string `json:"target"`
	Sent   string `json:"sent"`
}

// --- new_session ---

type NewSessionInput struct {
	Name      string `json:"name" jsonschema:"required,description=Session name"`
	Command   string `json:"command,omitempty" jsonschema:"description=Initial command to run"`
	Directory string `json:"directory,omitempty" jsonschema:"description=Working directory for the session"`
}

type NewSessionOutput struct {
	Session string `json:"session"`
	Created bool   `json:"created"`
}

// --- new_window ---

type NewWindowInput struct {
	Session string `json:"session" jsonschema:"required,description=Tmux session name"`
	Name    string `json:"name,omitempty" jsonschema:"description=Window name"`
	Command string `json:"command,omitempty" jsonschema:"description=Command to run in the new window"`
}

type NewWindowOutput struct {
	Session string `json:"session"`
	Window  string `json:"window"`
}

// --- kill_session ---

type KillSessionInput struct {
	Name string `json:"name" jsonschema:"required,description=Session name to kill"`
}

type KillSessionOutput struct {
	Session string `json:"session"`
	Killed  bool   `json:"killed"`
}

// --- wait_for_text ---

type WaitForTextInput struct {
	Session     string `json:"session" jsonschema:"required,description=tmux session name"`
	WindowIndex int    `json:"window_index,omitempty" jsonschema:"description=window index (default 0)"`
	PaneIndex   int    `json:"pane_index,omitempty" jsonschema:"description=pane index (default 0)"`
	Text        string `json:"text" jsonschema:"required,description=substring to wait for"`
	TimeoutSecs int    `json:"timeout_secs,omitempty" jsonschema:"description=timeout in seconds (default 30)"`
}

type WaitForTextOutput struct {
	Found        bool   `json:"found"`
	MatchingLine string `json:"matching_line"`
	ElapsedMs    int64  `json:"elapsed_ms"`
}

// --- search_panes ---

type SearchPanesInput struct {
	Session string `json:"session" jsonschema:"required,description=tmux session name"`
	Pattern string `json:"pattern" jsonschema:"required,description=regex pattern to search for"`
}

type PaneMatch struct {
	WindowIndex int      `json:"window_index"`
	WindowName  string   `json:"window_name"`
	PaneIndex   int      `json:"pane_index"`
	Lines       []string `json:"lines"`
}

type SearchPanesOutput struct {
	Session string      `json:"session"`
	Pattern string      `json:"pattern"`
	Matches []PaneMatch `json:"matches"`
}

// ---------------------------------------------------------------------------
// tmux_workspace (composed)
// ---------------------------------------------------------------------------

type WorkspacePane struct {
	Command string `json:"command,omitempty" jsonschema:"description=Command to run in this pane"`
	Dir     string `json:"dir,omitempty" jsonschema:"description=Working directory for this pane"`
	Split   string `json:"split,omitempty" jsonschema:"description=Split direction for this pane: 'horizontal' (or 'h') for side-by-side\\, 'vertical' (or 'v') for top-bottom"`
	Size    string `json:"size,omitempty" jsonschema:"description=Size percentage or cell count (e.g. '30%' or '25')"`
}

type WorkspaceWindow struct {
	Name   string          `json:"name,omitempty" jsonschema:"description=Window name"`
	Layout string          `json:"layout,omitempty" jsonschema:"description=Tmux layout: tiled\\, even-horizontal\\, even-vertical\\, main-horizontal\\, main-vertical"`
	Panes  []WorkspacePane `json:"panes,omitempty" jsonschema:"description=Panes in this window"`
}

type WorkspaceInput struct {
	Session string            `json:"session" jsonschema:"required,description=Session name"`
	Dir     string            `json:"dir,omitempty" jsonschema:"description=Base working directory for all panes"`
	Windows []WorkspaceWindow `json:"windows" jsonschema:"required,description=Windows to create"`
}

type WorkspaceOutput struct {
	Session     string `json:"session"`
	WindowCount int    `json:"windows_created"`
	PaneCount   int    `json:"panes_created"`
}

// ---------------------------------------------------------------------------
// tmux_snapshot
// ---------------------------------------------------------------------------

type SnapshotInput struct {
	Session string `json:"session" jsonschema:"required,description=Tmux session name to snapshot"`
}

type SnapshotOutput struct {
	Session     string            `json:"session"`
	WindowCount int               `json:"window_count"`
	PaneCount   int               `json:"pane_count"`
	Windows     []WorkspaceWindow `json:"windows"`
}

// ---------------------------------------------------------------------------
// tmux_template
// ---------------------------------------------------------------------------

type WorkspaceTemplate struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Dir         string            `json:"dir,omitempty"`
	Windows     []WorkspaceWindow `json:"windows"`
	CreatedAt   string            `json:"created_at,omitempty"`
}

type TemplateInput struct {
	Action      string            `json:"action" jsonschema:"required,description=Action to perform: save\\, load\\, list\\, apply\\, or delete"`
	Name        string            `json:"name,omitempty" jsonschema:"description=Template name (required for save\\, load\\, apply\\, delete)"`
	Description string            `json:"description,omitempty" jsonschema:"description=Optional description when saving a template"`
	Dir         string            `json:"dir,omitempty" jsonschema:"description=Base directory for template when saving an explicit spec"`
	Windows     []WorkspaceWindow `json:"windows,omitempty" jsonschema:"description=Window specifications when saving an explicit template"`
	Session     string            `json:"session,omitempty" jsonschema:"description=Source running session to snapshot when saving\\, or target session name when applying template"`
}

type TemplateInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	WindowCount int    `json:"window_count"`
	PaneCount   int    `json:"pane_count"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type TemplateOutput struct {
	Action    string             `json:"action"`
	Success   bool               `json:"success"`
	Message   string             `json:"message,omitempty"`
	Template  *WorkspaceTemplate `json:"template,omitempty"`
	Templates []TemplateInfo     `json:"templates,omitempty"`
	Workspace *WorkspaceOutput   `json:"workspace,omitempty"`
}

func isValidTemplateName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func getTemplatesDir() (string, error) {
	if custom := os.Getenv("TMUX_MCP_TEMPLATE_DIR"); custom != "" {
		if err := os.MkdirAll(custom, 0755); err != nil {
			return "", err
		}
		return custom, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(home, ".config")
	}
	dir := filepath.Join(configDir, "tmux-mcp", "templates")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func buildSplitArgs(target string, pane WorkspacePane, baseDir string) []string {
	splitArgs := []string{"split-window", "-t", target}
	splitDir := strings.ToLower(strings.TrimSpace(pane.Split))
	if splitDir == "h" || splitDir == "horizontal" {
		splitArgs = append(splitArgs, "-h")
	} else if splitDir == "v" || splitDir == "vertical" {
		splitArgs = append(splitArgs, "-v")
	}

	if pane.Size != "" {
		sizeStr := strings.TrimSpace(pane.Size)
		if strings.HasSuffix(sizeStr, "%") {
			splitArgs = append(splitArgs, "-p", strings.TrimSuffix(sizeStr, "%"))
		} else {
			splitArgs = append(splitArgs, "-l", sizeStr)
		}
	}

	paneDir := pane.Dir
	if paneDir == "" {
		paneDir = baseDir
	}
	if paneDir != "" {
		splitArgs = append(splitArgs, "-c", paneDir)
	}
	return splitArgs
}

func executeWorkspace(input WorkspaceInput) (WorkspaceOutput, error) {
	if input.Session == "" {
		return WorkspaceOutput{}, fmt.Errorf("[%s] session name is required", handler.ErrInvalidParam)
	}
	if len(input.Windows) == 0 {
		return WorkspaceOutput{}, fmt.Errorf("[%s] at least one window is required", handler.ErrInvalidParam)
	}

	slog.Info("creating workspace", "session", input.Session, "windows", len(input.Windows))

	baseDir := input.Dir
	totalPanes := 0

	// Create session with first window's first pane
	firstWin := input.Windows[0]
	sessionArgs := []string{"new-session", "-d", "-s", input.Session}
	if firstWin.Name != "" {
		sessionArgs = append(sessionArgs, "-n", firstWin.Name)
	}

	// Determine directory for first pane
	firstDir := baseDir
	if len(firstWin.Panes) > 0 && firstWin.Panes[0].Dir != "" {
		firstDir = firstWin.Panes[0].Dir
	}
	if firstDir != "" {
		sessionArgs = append(sessionArgs, "-c", firstDir)
	}

	_, err := runTmux(sessionArgs...)
	if err != nil {
		return WorkspaceOutput{}, fmt.Errorf("[%s] create session: %w", handler.ErrAPIError, err)
	}
	totalPanes++

	// Send command to first pane if specified
	if len(firstWin.Panes) > 0 && firstWin.Panes[0].Command != "" {
		_, _ = runTmux("send-keys", "-t", input.Session, firstWin.Panes[0].Command, "Enter")
	}

	// Add remaining panes to first window
	for i := 1; i < len(firstWin.Panes); i++ {
		pane := firstWin.Panes[i]
		splitArgs := buildSplitArgs(input.Session, pane, baseDir)
		_, _ = runTmux(splitArgs...)
		totalPanes++

		if pane.Command != "" {
			_, _ = runTmux("send-keys", "-t", input.Session, pane.Command, "Enter")
		}
	}

	// Apply layout to first window
	if firstWin.Layout != "" {
		_, _ = runTmux("select-layout", "-t", input.Session, firstWin.Layout)
	}

	// Create additional windows
	for w := 1; w < len(input.Windows); w++ {
		win := input.Windows[w]
		winArgs := []string{"new-window", "-t", input.Session}
		if win.Name != "" {
			winArgs = append(winArgs, "-n", win.Name)
		}

		// First pane directory
		winDir := baseDir
		if len(win.Panes) > 0 && win.Panes[0].Dir != "" {
			winDir = win.Panes[0].Dir
		}
		if winDir != "" {
			winArgs = append(winArgs, "-c", winDir)
		}

		_, _ = runTmux(winArgs...)
		totalPanes++

		// Send command to first pane
		if len(win.Panes) > 0 && win.Panes[0].Command != "" {
			target := input.Session + ":" + strconv.Itoa(w)
			_, _ = runTmux("send-keys", "-t", target, win.Panes[0].Command, "Enter")
		}

		// Additional panes
		for i := 1; i < len(win.Panes); i++ {
			pane := win.Panes[i]
			target := input.Session + ":" + strconv.Itoa(w)
			splitArgs := buildSplitArgs(target, pane, baseDir)
			_, _ = runTmux(splitArgs...)
			totalPanes++

			if pane.Command != "" {
				target := input.Session + ":" + strconv.Itoa(w)
				_, _ = runTmux("send-keys", "-t", target, pane.Command, "Enter")
			}
		}

		// Apply layout
		if win.Layout != "" {
			target := input.Session + ":" + strconv.Itoa(w)
			_, _ = runTmux("select-layout", "-t", target, win.Layout)
		}
	}

	return WorkspaceOutput{
		Session:     input.Session,
		WindowCount: len(input.Windows),
		PaneCount:   totalPanes,
	}, nil
}

func executeSnapshot(session string) ([]WorkspaceWindow, int, error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return nil, 0, fmt.Errorf("[%s] session name is required", handler.ErrInvalidParam)
	}

	// Verify session exists
	_, err := runTmux("has-session", "-t", session)
	if err != nil {
		return nil, 0, formatTmuxError("tmux_snapshot", err)
	}

	// Query windows
	winOut, err := runTmux("list-windows", "-t", session, "-F", "#{window_index}\t#{window_name}\t#{window_layout}")
	if err != nil {
		return nil, 0, formatTmuxError("tmux_snapshot: list-windows", err)
	}

	var windows []WorkspaceWindow
	totalPanes := 0

	for line := range strings.SplitSeq(winOut, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		wIdx := parts[0]
		wName := parts[1]
		wLayout := parts[2]

		// Query panes in this window
		paneOut, err := runTmux("list-panes", "-t", session+":"+wIdx, "-F", "#{pane_current_path}\t#{pane_current_command}")
		var panes []WorkspacePane
		if err == nil {
			for pLine := range strings.SplitSeq(paneOut, "\n") {
				pLine = strings.TrimSpace(pLine)
				if pLine == "" {
					continue
				}
				pParts := strings.Split(pLine, "\t")
				pDir := ""
				pCmd := ""
				if len(pParts) > 0 {
					pDir = pParts[0]
				}
				if len(pParts) > 1 {
					pCmd = pParts[1]
				}
				panes = append(panes, WorkspacePane{
					Dir:     pDir,
					Command: pCmd,
				})
				totalPanes++
			}
		}

		windows = append(windows, WorkspaceWindow{
			Name:   wName,
			Layout: wLayout,
			Panes:  panes,
		})
	}

	return windows, totalPanes, nil
}

// ---------------------------------------------------------------------------
// TmuxModule
// ---------------------------------------------------------------------------

type TmuxModule struct{}

func (m *TmuxModule) Name() string        { return "tmux" }
func (m *TmuxModule) Description() string { return "Tmux session, window, and pane management" }

func (m *TmuxModule) Tools() []registry.ToolDefinition {
	// ---------------------------------------------------------------
	// tmux_list_sessions — read-only
	// ---------------------------------------------------------------
	listSessions := handler.TypedHandler[ListSessionsInput, ListSessionsOutput](
		"tmux_list_sessions",
		"List all tmux sessions with window count, creation time, and attached status.",
		func(_ context.Context, input ListSessionsInput) (ListSessionsOutput, error) {
			out, err := runTmux("list-sessions", "-F",
				"#{session_name}\t#{session_windows}\t#{session_created}\t#{session_attached}")
			if err != nil {
				if isNoServer(err) {
					return ListSessionsOutput{Sessions: []sessionInfo{}}, nil
				}
				return ListSessionsOutput{}, fmt.Errorf("[%s] list sessions: %w", handler.ErrAPIError, err)
			}

			var sessions []sessionInfo
			for line := range strings.SplitSeq(out, "\n") {
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "\t", 4)
				if len(parts) < 4 {
					continue
				}
				windows, _ := strconv.Atoi(parts[1])
				attached := parts[3] == "1"
				if input.Prefix != "" && !strings.HasPrefix(parts[0], input.Prefix) {
					continue
				}
				if input.AttachedOnly && !attached {
					continue
				}
				sessions = append(sessions, sessionInfo{
					Name:     parts[0],
					Windows:  windows,
					Created:  parts[2],
					Attached: attached,
				})
			}
			if sessions == nil {
				sessions = []sessionInfo{}
			}
			return ListSessionsOutput{Sessions: sessions}, nil
		},
	)
	listSessions.Category = "tmux"
	listSessions.IsWrite = false

	// ---------------------------------------------------------------
	// tmux_list_windows — read-only
	// ---------------------------------------------------------------
	listWindows := handler.TypedHandler[ListWindowsInput, ListWindowsOutput](
		"tmux_list_windows",
		"List windows in a tmux session with index, name, pane count, active status, and layout.",
		func(_ context.Context, input ListWindowsInput) (ListWindowsOutput, error) {
			out, err := runTmux("list-windows", "-t", input.Session, "-F",
				"#{window_index}\t#{window_name}\t#{window_panes}\t#{window_active}\t#{window_layout}")
			if err != nil {
				if isNoServer(err) {
					return ListWindowsOutput{Session: input.Session, Windows: []windowInfo{}}, nil
				}
				return ListWindowsOutput{}, formatTmuxError("list windows", err)
			}

			var windows []windowInfo
			for line := range strings.SplitSeq(out, "\n") {
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "\t", 5)
				if len(parts) < 5 {
					continue
				}
				idx, _ := strconv.Atoi(parts[0])
				panes, _ := strconv.Atoi(parts[2])
				active := parts[3] == "1"
				windows = append(windows, windowInfo{
					Index:  idx,
					Name:   parts[1],
					Panes:  panes,
					Active: active,
					Layout: parts[4],
				})
			}
			if windows == nil {
				windows = []windowInfo{}
			}
			return ListWindowsOutput{
				Session: input.Session,
				Windows: windows,
			}, nil
		},
	)
	listWindows.Category = "tmux"
	listWindows.IsWrite = false

	// ---------------------------------------------------------------
	// tmux_list_panes — read-only
	// ---------------------------------------------------------------
	listPanes := handler.TypedHandler[ListPanesInput, ListPanesOutput](
		"tmux_list_panes",
		"List panes in a session or window with index, dimensions, current command, and active status.",
		func(_ context.Context, input ListPanesInput) (ListPanesOutput, error) {
			target := tmuxTarget(input.Session, input.Window, "")
			out, err := runTmux("list-panes", "-t", target, "-F",
				"#{pane_index}\t#{pane_width}\t#{pane_height}\t#{pane_current_command}\t#{pane_active}")
			if err != nil {
				if isNoServer(err) {
					return ListPanesOutput{Target: target, Panes: []paneInfo{}}, nil
				}
				return ListPanesOutput{}, formatTmuxError("list panes", err)
			}

			var panes []paneInfo
			for line := range strings.SplitSeq(out, "\n") {
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "\t", 5)
				if len(parts) < 5 {
					continue
				}
				idx, _ := strconv.Atoi(parts[0])
				width, _ := strconv.Atoi(parts[1])
				height, _ := strconv.Atoi(parts[2])
				active := parts[4] == "1"
				panes = append(panes, paneInfo{
					Index:   idx,
					Width:   width,
					Height:  height,
					Command: parts[3],
					Active:  active,
				})
			}
			if panes == nil {
				panes = []paneInfo{}
			}
			return ListPanesOutput{
				Target: target,
				Panes:  panes,
			}, nil
		},
	)
	listPanes.Category = "tmux"
	listPanes.IsWrite = false

	// ---------------------------------------------------------------
	// tmux_capture_pane — read-only
	// ---------------------------------------------------------------
	capturePane := handler.TypedHandler[CaptureInput, CaptureOutput](
		"tmux_capture_pane",
		"Capture visible text content from a tmux pane. Default 50 lines of scrollback.",
		func(_ context.Context, input CaptureInput) (CaptureOutput, error) {
			target := tmuxTarget(input.Session, input.Window, input.Pane)
			args := []string{"capture-pane", "-p", "-t", target}
			if input.StartLine != "" {
				args = append(args, "-S", input.StartLine)
			} else {
				lines := input.Lines
				if lines <= 0 {
					lines = 50
				}
				args = append(args, "-S", fmt.Sprintf("-%d", lines))
			}
			if input.EndLine != "" {
				args = append(args, "-E", input.EndLine)
			}
			out, err := runTmux(args...)
			if err != nil {
				return CaptureOutput{}, formatTmuxError("capture pane", err)
			}
			return CaptureOutput{
				Target:  target,
				Content: out,
			}, nil
		},
	)
	capturePane.Category = "tmux"
	capturePane.IsWrite = false
	capturePane.SearchTerms = []string{"scrollback", "pane output", "terminal output", "copy pane"}
	capturePane.MaxResultChars = 8000

	// ---------------------------------------------------------------
	// tmux_send_keys — write
	// ---------------------------------------------------------------
	sendKeys := handler.TypedHandler[SendKeysInput, SendKeysOutput](
		"tmux_send_keys",
		"Send keystrokes to a tmux pane whose foreground command is an agent runtime (node/cline/claude/codex/gemini/agy). Refused for shell/interpreter panes unless FLEET_TMUX_SHELL_OK=1 or the pane option @accept_shell_injection=1 (2026-09-06 echo-storm guard); use tmux display-message for notices. Keys can be literal text or tmux key names like Enter, C-c, etc.",
		func(_ context.Context, input SendKeysInput) (SendKeysOutput, error) {
			target := tmuxTarget(input.Session, input.Window, input.Pane)
			if ok, why := sendKeysAllowed(target, runTmux); !ok {
				slog.Warn("send keys refused", "target", target, "reason", why)
				return SendKeysOutput{}, fmt.Errorf("[%s] send keys: %s", handler.ErrInvalidParam, why)
			}
			slog.Info("sending keys", "target", target, "keys", input.Keys)
			_, err := runTmux("send-keys", "-t", target, input.Keys)
			if err != nil {
				slog.Error("send keys failed", "target", target, "error", err)
				return SendKeysOutput{}, formatTmuxError("send keys", err)
			}
			return SendKeysOutput{
				Target: target,
				Sent:   input.Keys,
			}, nil
		},
	)
	sendKeys.Category = "tmux"
	sendKeys.IsWrite = true

	// ---------------------------------------------------------------
	// tmux_new_session — write
	// ---------------------------------------------------------------
	newSession := handler.TypedHandler[NewSessionInput, NewSessionOutput](
		"tmux_new_session",
		"Create a new detached tmux session with optional working directory and initial command.",
		func(_ context.Context, input NewSessionInput) (NewSessionOutput, error) {
			if strings.TrimSpace(input.Name) == "" {
				return NewSessionOutput{}, fmt.Errorf("[%s] session name is required", handler.ErrInvalidParam)
			}
			slog.Info("creating session", "session", input.Name, "directory", input.Directory)
			args := []string{"new-session", "-d", "-s", input.Name}
			if input.Directory != "" {
				args = append(args, "-c", input.Directory)
			}
			if input.Command != "" {
				args = append(args, input.Command)
			}
			_, err := runTmux(args...)
			if err != nil {
				slog.Error("session creation failed", "session", input.Name, "error", err)
				return NewSessionOutput{}, fmt.Errorf("[%s] new session: %w", handler.ErrAPIError, err)
			}
			slog.Info("session created", "session", input.Name)
			return NewSessionOutput{
				Session: input.Name,
				Created: true,
			}, nil
		},
	)
	newSession.Category = "tmux"
	newSession.IsWrite = true

	// ---------------------------------------------------------------
	// tmux_new_window — write
	// ---------------------------------------------------------------
	newWindow := handler.TypedHandler[NewWindowInput, NewWindowOutput](
		"tmux_new_window",
		"Create a new window in an existing tmux session with optional name and command.",
		func(_ context.Context, input NewWindowInput) (NewWindowOutput, error) {
			if strings.TrimSpace(input.Session) == "" {
				return NewWindowOutput{}, fmt.Errorf("[%s] session name is required", handler.ErrInvalidParam)
			}
			args := []string{"new-window", "-t", input.Session, "-P", "-F", "#{window_index}"}
			if input.Name != "" {
				args = append(args, "-n", input.Name)
			}
			if input.Command != "" {
				args = append(args, input.Command)
			}
			out, err := runTmux(args...)
			if err != nil {
				return NewWindowOutput{}, formatTmuxError("new window", err)
			}
			return NewWindowOutput{
				Session: input.Session,
				Window:  out,
			}, nil
		},
	)
	newWindow.Category = "tmux"
	newWindow.IsWrite = true

	// ---------------------------------------------------------------
	// tmux_kill_session — write, complex
	// ---------------------------------------------------------------
	killSession := handler.TypedHandler[KillSessionInput, KillSessionOutput](
		"tmux_kill_session",
		"Kill a tmux session by name.",
		func(_ context.Context, input KillSessionInput) (KillSessionOutput, error) {
			if err := validateKillSessionName(strings.TrimSpace(input.Name)); err != nil {
				return KillSessionOutput{}, err
			}
			slog.Info("killing session", "session", input.Name)
			_, err := runTmux("kill-session", "-t", input.Name)
			if err != nil {
				slog.Error("session kill failed", "session", input.Name, "error", err)
				return KillSessionOutput{}, formatTmuxError("kill session", err)
			}
			slog.Info("session killed", "session", input.Name)
			return KillSessionOutput{
				Session: input.Name,
				Killed:  true,
			}, nil
		},
	)
	killSession.Category = "tmux"
	killSession.IsWrite = true
	killSession.Complexity = registry.ComplexityComplex

	// ---------------------------------------------------------------
	// tmux_wait_for_text — read-only polling
	// ---------------------------------------------------------------
	waitForText := handler.TypedHandler[WaitForTextInput, WaitForTextOutput](
		"tmux_wait_for_text",
		"Poll a tmux pane until specific text appears. Returns the matching line or times out.",
		func(ctx context.Context, input WaitForTextInput) (WaitForTextOutput, error) {
			if input.Text == "" {
				return WaitForTextOutput{}, fmt.Errorf("[%s] text is required", handler.ErrInvalidParam)
			}

			timeout := input.TimeoutSecs
			if timeout <= 0 {
				timeout = 30
			}

			// Build target: only include window/pane if explicitly set (non-zero)
			winStr := ""
			if input.WindowIndex != 0 {
				winStr = strconv.Itoa(input.WindowIndex)
			}
			paneStr := ""
			if input.PaneIndex != 0 {
				paneStr = strconv.Itoa(input.PaneIndex)
			}
			target := tmuxTarget(input.Session, winStr, paneStr)

			deadline := time.Now().Add(time.Duration(timeout) * time.Second)
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()

			start := time.Now()

			for {
				out, err := runTmux("capture-pane", "-p", "-t", target)
				if err == nil {
					for line := range strings.SplitSeq(out, "\n") {
						if strings.Contains(line, input.Text) {
							return WaitForTextOutput{
								Found:        true,
								MatchingLine: line,
								ElapsedMs:    time.Since(start).Milliseconds(),
							}, nil
						}
					}
				}

				if time.Now().After(deadline) {
					return WaitForTextOutput{}, fmt.Errorf(
						"[%s] text %q not found in %s after %ds",
						handler.ErrAPIError, input.Text, target, timeout,
					)
				}

				select {
				case <-ctx.Done():
					return WaitForTextOutput{}, fmt.Errorf("[%s] context cancelled: %w", handler.ErrAPIError, ctx.Err())
				case <-ticker.C:
				}
			}
		},
	)
	waitForText.Category = "tmux"
	waitForText.IsWrite = false

	// ---------------------------------------------------------------
	// tmux_search_panes — read-only search
	// ---------------------------------------------------------------
	searchPanes := handler.TypedHandler[SearchPanesInput, SearchPanesOutput](
		"tmux_search_panes",
		"Search across all panes in a session for a regex pattern. Returns matching lines with window/pane indices.",
		func(_ context.Context, input SearchPanesInput) (SearchPanesOutput, error) {
			if input.Session == "" {
				return SearchPanesOutput{}, fmt.Errorf("[%s] session is required", handler.ErrInvalidParam)
			}

			re, err := regexp.Compile(input.Pattern)
			if err != nil {
				return SearchPanesOutput{}, fmt.Errorf("[%s] invalid regex pattern: %w", handler.ErrInvalidParam, err)
			}

			// List all windows in the session
			winOut, err := runTmux("list-windows", "-t", input.Session, "-F",
				"#{window_index}\t#{window_name}")
			if err != nil {
				if isNoServer(err) {
					return SearchPanesOutput{Session: input.Session, Pattern: input.Pattern, Matches: []PaneMatch{}}, nil
				}
				return SearchPanesOutput{}, fmt.Errorf("[%s] list windows: %w", handler.ErrNotFound, err)
			}

			var matches []PaneMatch

			for winLine := range strings.SplitSeq(winOut, "\n") {
				if winLine == "" {
					continue
				}
				winParts := strings.SplitN(winLine, "\t", 2)
				if len(winParts) < 2 {
					continue
				}
				winIdx, _ := strconv.Atoi(winParts[0])
				winName := winParts[1]

				// List panes in this window
				paneOut, err := runTmux("list-panes", "-t",
					fmt.Sprintf("%s:%d", input.Session, winIdx), "-F", "#{pane_index}")
				if err != nil {
					continue
				}

				for paneLine := range strings.SplitSeq(paneOut, "\n") {
					if paneLine == "" {
						continue
					}
					paneIdx, _ := strconv.Atoi(strings.TrimSpace(paneLine))

					// Capture pane content
					target := fmt.Sprintf("%s:%d.%d", input.Session, winIdx, paneIdx)
					content, err := runTmux("capture-pane", "-p", "-t", target)
					if err != nil {
						continue
					}

					var matchedLines []string
					for line := range strings.SplitSeq(content, "\n") {
						if re.MatchString(line) {
							matchedLines = append(matchedLines, line)
						}
					}

					if len(matchedLines) > 0 {
						matches = append(matches, PaneMatch{
							WindowIndex: winIdx,
							WindowName:  winName,
							PaneIndex:   paneIdx,
							Lines:       matchedLines,
						})
					}
				}
			}

			if matches == nil {
				matches = []PaneMatch{}
			}

			return SearchPanesOutput{
				Session: input.Session,
				Pattern: input.Pattern,
				Matches: matches,
			}, nil
		},
	)
	searchPanes.Category = "tmux"
	searchPanes.IsWrite = false
	searchPanes.SearchTerms = []string{"grep panes", "find in panes", "search session output"}

	// ---------------------------------------------------------------
	// tmux_workspace (composed) — write
	// ---------------------------------------------------------------
	workspace := handler.TypedHandler[WorkspaceInput, WorkspaceOutput](
		"tmux_workspace",
		"Create a multi-window, multi-pane tmux workspace from a declarative spec. Single tool replaces new_session + new_window + send_keys sequences.",
		func(_ context.Context, input WorkspaceInput) (WorkspaceOutput, error) {
			if input.Session == "" {
				return WorkspaceOutput{}, fmt.Errorf("[%s] session name is required", handler.ErrInvalidParam)
			}
			if len(input.Windows) == 0 {
				return WorkspaceOutput{}, fmt.Errorf("[%s] at least one window is required", handler.ErrInvalidParam)
			}

			slog.Info("creating workspace", "session", input.Session, "windows", len(input.Windows))

			baseDir := input.Dir
			totalPanes := 0

			// Create session with first window's first pane
			firstWin := input.Windows[0]
			sessionArgs := []string{"new-session", "-d", "-s", input.Session}
			if firstWin.Name != "" {
				sessionArgs = append(sessionArgs, "-n", firstWin.Name)
			}

			// Determine directory for first pane
			firstDir := baseDir
			if len(firstWin.Panes) > 0 && firstWin.Panes[0].Dir != "" {
				firstDir = firstWin.Panes[0].Dir
			}
			if firstDir != "" {
				sessionArgs = append(sessionArgs, "-c", firstDir)
			}

			_, err := runTmux(sessionArgs...)
			if err != nil {
				return WorkspaceOutput{}, fmt.Errorf("[%s] create session: %w", handler.ErrAPIError, err)
			}
			totalPanes++

			// Send command to first pane if specified
			if len(firstWin.Panes) > 0 && firstWin.Panes[0].Command != "" {
				_, _ = runTmux("send-keys", "-t", input.Session, firstWin.Panes[0].Command, "Enter")
			}

			// Add remaining panes to first window
			for i := 1; i < len(firstWin.Panes); i++ {
				pane := firstWin.Panes[i]
				splitArgs := buildSplitArgs(input.Session, pane, baseDir)
				_, _ = runTmux(splitArgs...)
				totalPanes++

				if pane.Command != "" {
					_, _ = runTmux("send-keys", "-t", input.Session, pane.Command, "Enter")
				}
			}

			// Apply layout to first window
			if firstWin.Layout != "" {
				_, _ = runTmux("select-layout", "-t", input.Session, firstWin.Layout)
			}

			// Create additional windows
			for w := 1; w < len(input.Windows); w++ {
				win := input.Windows[w]
				winArgs := []string{"new-window", "-t", input.Session}
				if win.Name != "" {
					winArgs = append(winArgs, "-n", win.Name)
				}

				// First pane directory
				winDir := baseDir
				if len(win.Panes) > 0 && win.Panes[0].Dir != "" {
					winDir = win.Panes[0].Dir
				}
				if winDir != "" {
					winArgs = append(winArgs, "-c", winDir)
				}

				_, _ = runTmux(winArgs...)
				totalPanes++

				// Send command to first pane
				if len(win.Panes) > 0 && win.Panes[0].Command != "" {
					target := input.Session + ":" + strconv.Itoa(w)
					_, _ = runTmux("send-keys", "-t", target, win.Panes[0].Command, "Enter")
				}

				// Additional panes
				for i := 1; i < len(win.Panes); i++ {
					pane := win.Panes[i]
					splitArgs := []string{"split-window", "-t", input.Session + ":" + strconv.Itoa(w)}
					paneDir := pane.Dir
					if paneDir == "" {
						paneDir = baseDir
					}
					if paneDir != "" {
						splitArgs = append(splitArgs, "-c", paneDir)
					}
					_, _ = runTmux(splitArgs...)
					totalPanes++

					if pane.Command != "" {
						target := input.Session + ":" + strconv.Itoa(w)
						_, _ = runTmux("send-keys", "-t", target, pane.Command, "Enter")
					}
				}

				// Apply layout
				if win.Layout != "" {
					_, _ = runTmux("select-layout", "-t", input.Session+":"+strconv.Itoa(w), win.Layout)
				}
			}

			// Select first window
			_, _ = runTmux("select-window", "-t", input.Session+":0")

			slog.Info("workspace created", "session", input.Session, "windows", len(input.Windows), "panes", totalPanes)
			return WorkspaceOutput{
				Session:     input.Session,
				WindowCount: len(input.Windows),
				PaneCount:   totalPanes,
			}, nil
		},
	)
	workspace.Category = "tmux"
	workspace.IsWrite = true

	// ---------------------------------------------------------------
	// tmux_resize_pane — write
	// ---------------------------------------------------------------
	resizePane := handler.TypedHandler[ResizePaneInput, ResizePaneOutput](
		"tmux_resize_pane",
		"Resize a tmux pane programmatically: adjust by direction (U/D/L/R) with cell count, set absolute width (-x) and height (-y), or toggle zoom state (-Z).",
		func(_ context.Context, input ResizePaneInput) (ResizePaneOutput, error) {
			if strings.TrimSpace(input.Session) == "" {
				return ResizePaneOutput{}, fmt.Errorf("[%s] session name is required", handler.ErrInvalidParam)
			}

			dir := strings.ToUpper(strings.TrimSpace(input.Direction))
			if dir == "" && input.Width <= 0 && input.Height <= 0 && !input.Zoom {
				return ResizePaneOutput{}, fmt.Errorf("[%s] at least one resize parameter must be specified (direction, width, height, or zoom)", handler.ErrInvalidParam)
			}

			if dir != "" {
				switch dir {
				case "U", "UP":
					dir = "U"
				case "D", "DOWN":
					dir = "D"
				case "L", "LEFT":
					dir = "L"
				case "R", "RIGHT":
					dir = "R"
				default:
					return ResizePaneOutput{}, fmt.Errorf("[%s] invalid direction %q: must be U, D, L, or R", handler.ErrInvalidParam, input.Direction)
				}
			}

			target := tmuxTarget(input.Session, input.Window, input.Pane)
			args := []string{"resize-pane", "-t", target}

			if input.Zoom {
				args = append(args, "-Z")
			}
			if input.Width > 0 {
				args = append(args, "-x", strconv.Itoa(input.Width))
			}
			if input.Height > 0 {
				args = append(args, "-y", strconv.Itoa(input.Height))
			}
			if dir != "" {
				args = append(args, "-"+dir)
				if input.Adjustment > 0 {
					args = append(args, strconv.Itoa(input.Adjustment))
				}
			}

			slog.Info("resizing pane", "target", target, "args", args)
			_, err := runTmux(args...)
			if err != nil {
				return ResizePaneOutput{}, formatTmuxError("resize pane", err)
			}

			return ResizePaneOutput{
				Target:  target,
				Resized: true,
			}, nil
		},
	)
	resizePane.Category = "tmux"
	resizePane.IsWrite = true

	// ---------------------------------------------------------------
	// tmux_snapshot — read-only
	// ---------------------------------------------------------------
	snapshot := handler.TypedHandler[SnapshotInput, SnapshotOutput](
		"tmux_snapshot",
		"Capture the layout, windows, and panes of a running tmux session into a reusable workspace specification matching tmux_workspace schema.",
		func(_ context.Context, input SnapshotInput) (SnapshotOutput, error) {
			session := strings.TrimSpace(input.Session)
			if session == "" {
				return SnapshotOutput{}, fmt.Errorf("[%s] session name is required", handler.ErrInvalidParam)
			}

			// Verify session exists
			_, err := runTmux("has-session", "-t", session)
			if err != nil {
				return SnapshotOutput{}, formatTmuxError("tmux_snapshot", err)
			}

			// Query windows
			winOut, err := runTmux("list-windows", "-t", session, "-F", "#{window_index}\t#{window_name}\t#{window_layout}")
			if err != nil {
				return SnapshotOutput{}, formatTmuxError("tmux_snapshot: list-windows", err)
			}

			var windows []WorkspaceWindow
			totalPanes := 0

			for line := range strings.SplitSeq(winOut, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := strings.Split(line, "\t")
				if len(parts) < 3 {
					continue
				}
				wIdx := parts[0]
				wName := parts[1]
				wLayout := parts[2]

				// Query panes in this window
				paneOut, err := runTmux("list-panes", "-t", session+":"+wIdx, "-F", "#{pane_current_path}\t#{pane_current_command}")
				var panes []WorkspacePane
				if err == nil {
					for pLine := range strings.SplitSeq(paneOut, "\n") {
						pLine = strings.TrimSpace(pLine)
						if pLine == "" {
							continue
						}
						pParts := strings.Split(pLine, "\t")
						pDir := ""
						pCmd := ""
						if len(pParts) > 0 {
							pDir = pParts[0]
						}
						if len(pParts) > 1 {
							pCmd = pParts[1]
						}
						panes = append(panes, WorkspacePane{
							Dir:     pDir,
							Command: pCmd,
						})
						totalPanes++
					}
				}

				windows = append(windows, WorkspaceWindow{
					Name:   wName,
					Layout: wLayout,
					Panes:  panes,
				})
			}

			return SnapshotOutput{
				Session:     session,
				WindowCount: len(windows),
				PaneCount:   totalPanes,
				Windows:     windows,
			}, nil
		},
	)
	snapshot.Category = "tmux"
	snapshot.SearchTerms = []string{"snapshot", "save workspace", "export layout", "tmux layout"}

	// ---------------------------------------------------------------
	// tmux_template — write
	// ---------------------------------------------------------------
	templateTool := handler.TypedHandler[TemplateInput, TemplateOutput](
		"tmux_template",
		"Save, list, recall, apply, or delete named workspace templates. Enables saving running sessions or declarative layouts as reusable named templates, and instantiating them on demand.",
		func(_ context.Context, input TemplateInput) (TemplateOutput, error) {
			action := strings.ToLower(strings.TrimSpace(input.Action))
			if action == "" {
				return TemplateOutput{}, fmt.Errorf("[%s] action is required (save, load, list, apply, delete)", handler.ErrInvalidParam)
			}

			dir, err := getTemplatesDir()
			if err != nil {
				return TemplateOutput{}, fmt.Errorf("[%s] failed to access template directory: %w", handler.ErrAPIError, err)
			}

			name := strings.TrimSpace(input.Name)

			switch action {
			case "list":
				entries, err := os.ReadDir(dir)
				if err != nil {
					return TemplateOutput{}, fmt.Errorf("[%s] read templates directory: %w", handler.ErrAPIError, err)
				}
				var templates []TemplateInfo
				for _, entry := range entries {
					if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
						continue
					}
					tPath := filepath.Join(dir, entry.Name())
					raw, err := os.ReadFile(tPath)
					if err != nil {
						continue
					}
					var tpl WorkspaceTemplate
					if err := json.Unmarshal(raw, &tpl); err != nil {
						continue
					}
					paneCount := 0
					for _, w := range tpl.Windows {
						paneCount += len(w.Panes)
					}
					templates = append(templates, TemplateInfo{
						Name:        tpl.Name,
						Description: tpl.Description,
						WindowCount: len(tpl.Windows),
						PaneCount:   paneCount,
						CreatedAt:   tpl.CreatedAt,
					})
				}
				if templates == nil {
					templates = []TemplateInfo{}
				}
				sort.Slice(templates, func(i, j int) bool {
					return templates[i].Name < templates[j].Name
				})
				return TemplateOutput{
					Action:    "list",
					Success:   true,
					Templates: templates,
				}, nil

			case "save":
				if !isValidTemplateName(name) {
					return TemplateOutput{}, fmt.Errorf("[%s] valid template name required (alphanumeric, dash, underscore, max 64 chars)", handler.ErrInvalidParam)
				}

				var windows []WorkspaceWindow
				baseDir := input.Dir

				if input.Session != "" {
					snapWindows, _, err := executeSnapshot(input.Session)
					if err != nil {
						return TemplateOutput{}, err
					}
					windows = snapWindows
				} else if len(input.Windows) > 0 {
					windows = input.Windows
				} else {
					return TemplateOutput{}, fmt.Errorf("[%s] must provide windows or session to save template", handler.ErrInvalidParam)
				}

				tpl := WorkspaceTemplate{
					Name:        name,
					Description: input.Description,
					Dir:         baseDir,
					Windows:     windows,
					CreatedAt:   time.Now().UTC().Format(time.RFC3339),
				}

				data, err := json.MarshalIndent(tpl, "", "  ")
				if err != nil {
					return TemplateOutput{}, fmt.Errorf("[%s] marshal template: %w", handler.ErrAPIError, err)
				}

				tPath := filepath.Join(dir, name+".json")
				if err := os.WriteFile(tPath, data, 0644); err != nil {
					return TemplateOutput{}, fmt.Errorf("[%s] save template file: %w", handler.ErrAPIError, err)
				}

				return TemplateOutput{
					Action:   "save",
					Success:  true,
					Message:  fmt.Sprintf("Template %q saved successfully with %d windows", name, len(windows)),
					Template: &tpl,
				}, nil

			case "load":
				if !isValidTemplateName(name) {
					return TemplateOutput{}, fmt.Errorf("[%s] valid template name required", handler.ErrInvalidParam)
				}
				tPath := filepath.Join(dir, name+".json")
				raw, err := os.ReadFile(tPath)
				if err != nil {
					if os.IsNotExist(err) {
						return TemplateOutput{}, fmt.Errorf("[%s] template %q not found", handler.ErrNotFound, name)
					}
					return TemplateOutput{}, fmt.Errorf("[%s] read template: %w", handler.ErrAPIError, err)
				}
				var tpl WorkspaceTemplate
				if err := json.Unmarshal(raw, &tpl); err != nil {
					return TemplateOutput{}, fmt.Errorf("[%s] unmarshal template: %w", handler.ErrAPIError, err)
				}
				return TemplateOutput{
					Action:   "load",
					Success:  true,
					Template: &tpl,
				}, nil

			case "apply":
				if !isValidTemplateName(name) {
					return TemplateOutput{}, fmt.Errorf("[%s] valid template name required", handler.ErrInvalidParam)
				}
				tPath := filepath.Join(dir, name+".json")
				raw, err := os.ReadFile(tPath)
				if err != nil {
					if os.IsNotExist(err) {
						return TemplateOutput{}, fmt.Errorf("[%s] template %q not found", handler.ErrNotFound, name)
					}
					return TemplateOutput{}, fmt.Errorf("[%s] read template: %w", handler.ErrAPIError, err)
				}
				var tpl WorkspaceTemplate
				if err := json.Unmarshal(raw, &tpl); err != nil {
					return TemplateOutput{}, fmt.Errorf("[%s] unmarshal template: %w", handler.ErrAPIError, err)
				}

				targetSession := strings.TrimSpace(input.Session)
				if targetSession == "" {
					targetSession = tpl.Name
				}

				wsOut, err := executeWorkspace(WorkspaceInput{
					Session: targetSession,
					Dir:     tpl.Dir,
					Windows: tpl.Windows,
				})
				if err != nil {
					return TemplateOutput{}, err
				}

				return TemplateOutput{
					Action:    "apply",
					Success:   true,
					Message:   fmt.Sprintf("Template %q applied to session %q", name, targetSession),
					Template:  &tpl,
					Workspace: &wsOut,
				}, nil

			case "delete":
				if !isValidTemplateName(name) {
					return TemplateOutput{}, fmt.Errorf("[%s] valid template name required", handler.ErrInvalidParam)
				}
				tPath := filepath.Join(dir, name+".json")
				if err := os.Remove(tPath); err != nil {
					if os.IsNotExist(err) {
						return TemplateOutput{}, fmt.Errorf("[%s] template %q not found", handler.ErrNotFound, name)
					}
					return TemplateOutput{}, fmt.Errorf("[%s] delete template: %w", handler.ErrAPIError, err)
				}
				return TemplateOutput{
					Action:  "delete",
					Success: true,
					Message: fmt.Sprintf("Template %q deleted successfully", name),
				}, nil

			default:
				return TemplateOutput{}, fmt.Errorf("[%s] unknown action %q (supported: save, load, list, apply, delete)", handler.ErrInvalidParam, action)
			}
		},
	)
	templateTool.Category = "tmux"
	templateTool.IsWrite = true
	templateTool.SearchTerms = []string{"workspace template", "save template", "load template", "recall layout", "tmux template"}

	return []registry.ToolDefinition{
		listSessions,
		listWindows,
		listPanes,
		capturePane,
		sendKeys,
		newSession,
		newWindow,
		killSession,
		waitForText,
		searchPanes,
		workspace,
		resizePane,
		snapshot,
		templateTool,
	}
}

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

func Setup() (*registry.ToolRegistry, *registry.MCPServer) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("service", "tmux-mcp"))

	slog.Info("server starting", "name", "tmux-mcp", "version", "1.2.0")

	reg := registry.NewToolRegistry(registry.Config{
		Middleware: []registry.Middleware{
			gate.Middleware(gate.Config{Gate: gate.PauseWrites}),
			registry.AuditMiddleware(""),
			registry.SafetyTierMiddleware(),
		},
	})
	mod := &TmuxModule{}
	reg.RegisterModule(mod)
	slog.Info("tools registered", "module", mod.Name(), "count", len(mod.Tools()))

	s := registry.NewMCPServer("tmux-mcp", "1.2.0")
	reg.RegisterWithServer(s)
	buildTmuxResourceRegistry().RegisterWithServer(s)
	buildTmuxPromptRegistry().RegisterWithServer(s)

	return reg, s
}
