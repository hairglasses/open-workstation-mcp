package omarchy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/mcptest"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

func callToolJSON[Out any](t *testing.T, srv *mcptest.Server, name string, args map[string]any) (Out, *registry.CallToolResult) {
	t.Helper()
	var zero Out
	td, ok := srv.Registry.GetTool(name)
	if !ok {
		t.Fatalf("missing tool %s", name)
	}
	req, err := registry.NewCallToolRequest(name, args)
	if err != nil {
		t.Fatalf("NewCallToolRequest(%s): %v", name, err)
	}
	res, err := td.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("%s returned transport error: %v", name, err)
	}
	if res == nil {
		t.Fatalf("%s returned nil result", name)
	}
	if res.IsError {
		return zero, res
	}
	text, ok := registry.ExtractTextContent(res.Content[0])
	if !ok {
		t.Fatalf("%s missing text content", name)
	}
	var out Out
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s unmarshal output: %v\nraw=%s", name, err, text)
	}
	return out, res
}

func newTestServer(t *testing.T, r *Runner) *mcptest.Server {
	t.Helper()
	SetRunner(r)
	t.Cleanup(ResetRunner)
	reg := registry.NewToolRegistry()
	reg.RegisterModule(&OmarchyModule{})
	srv := mcptest.NewServer(t, reg)
	RegisterResourcesAndPrompts(srv.MCP)
	return srv
}

func TestOmarchyModuleMetadata(t *testing.T) {
	mod := &OmarchyModule{}
	tools := mod.Tools()
	if len(tools) != 13 {
		t.Fatalf("expected 13 omarchy tools, got %d", len(tools))
	}
	readOnlyWant := map[string]bool{
		"omarchy_status":          true,
		"omarchy_commands":        true,
		"omarchy_hw":              true,
		"omarchy_debug":           true,
		"omarchy_config_validate": true,
	}
	for _, td := range tools {
		if readOnlyWant[td.Tool.Name] {
			if td.IsWrite || td.ReadOnlyOverride == nil || !*td.ReadOnlyOverride {
				t.Fatalf("%s expected read-only tool metadata, got IsWrite=%v ReadOnlyOverride=%v", td.Tool.Name, td.IsWrite, td.ReadOnlyOverride)
			}
		} else {
			if !td.IsWrite || td.Complexity != registry.ComplexityModerate {
				t.Fatalf("%s expected IsWrite=true ComplexityModerate, got IsWrite=%v Complexity=%q", td.Tool.Name, td.IsWrite, td.Complexity)
			}
		}
	}
}

func TestFlavorFencing_HeadlessAndNiri(t *testing.T) {
	t.Run("headless_skips_live_actions_and_hyprctl", func(t *testing.T) {
		tmp := t.TempDir()
		called := false
		r := &Runner{
			Env:        []string{"HOME=" + tmp, "PATH=/usr/bin:/bin"},
			ConfigHome: filepath.Join(tmp, ".config"),
			HomeDir:    tmp,
			ExecFunc: func(_ context.Context, _ []string, binary string, args ...string) (ExecResult, error) {
				if binary == "hyprctl" || (binary == "omarchy" && len(args) > 0 && args[0] == "restart") {
					called = true
				}
				return ExecResult{Stdout: "4.5.0-3"}, nil
			},
		}
		srv := newTestServer(t, r)

		for _, tc := range []struct {
			tool string
			args map[string]any
		}{
			{"omarchy_restart", map[string]any{"target": "terminal"}},
			{"omarchy_theme", map[string]any{"action": "set", "name": "mineral-slate"}},
			{"omarchy_bar", map[string]any{"action": "position", "value": "top"}},
			{"omarchy_toggle", map[string]any{"target": "nightlight"}},
			{"omarchy_reminder", map[string]any{"action": "show"}},
		} {
			out, res := callToolJSON[ActionOutput](t, srv, tc.tool, tc.args)
			if res.IsError {
				t.Fatalf("%s unexpected error in headless mode: %+v", tc.tool, res)
			}
			if out.Status != StatusSkippedHeadless || out.SkipStatus != StatusSkippedHeadless {
				t.Fatalf("%s expected %s, got %+v", tc.tool, StatusSkippedHeadless, out)
			}
		}

		rep, res := callToolJSON[ConfigValidationReport](t, srv, "omarchy_config_validate", map[string]any{"component": "hyprland"})
		if res.IsError {
			t.Fatalf("omarchy_config_validate unexpected error: %+v", res)
		}
		if rep.SkipStatus != StatusSkippedHeadless || rep.Hyprland.ConfigErrorsChecked {
			t.Fatalf("expected headless skip without hyprctl call, got %+v", rep)
		}
		if called {
			t.Fatal("expected zero live compositor invocations when headless")
		}
	})

	t.Run("niri_skips_with_SKIPPED_NIRI", func(t *testing.T) {
		tmp := t.TempDir()
		r := &Runner{
			Env: []string{
				"HOME=" + tmp,
				"PATH=/usr/bin:/bin",
				"WAYLAND_DISPLAY=wayland-1",
				"NIRI_SOCKET=/run/user/1000/niri.sock",
				"XDG_CURRENT_DESKTOP=niri",
			},
			ConfigHome: filepath.Join(tmp, ".config"),
			HomeDir:    tmp,
			ExecFunc: func(_ context.Context, _ []string, _ string, _ ...string) (ExecResult, error) {
				t.Fatal("live omarchy/hyprctl should not execute on Niri")
				return ExecResult{}, nil
			},
		}
		srv := newTestServer(t, r)

		out, res := callToolJSON[ActionOutput](t, srv, "omarchy_restart", map[string]any{"target": "shell"})
		if res.IsError {
			t.Fatalf("unexpected error on Niri: %+v", res)
		}
		if out.Status != StatusSkippedNiri || out.SkipStatus != StatusSkippedNiri || out.Flavor != FlavorNiri {
			t.Fatalf("expected SKIPPED_NIRI on Niri, got %+v", out)
		}

		rep, _ := callToolJSON[ConfigValidationReport](t, srv, "omarchy_config_validate", map[string]any{"component": "hyprland"})
		if rep.SkipStatus != StatusSkippedNiri || rep.Hyprland.SkipStatus != StatusSkippedNiri {
			t.Fatalf("expected SKIPPED_NIRI in config validation report, got %+v", rep)
		}
	})
}

func TestIntrospectionAndDebugNoSudoEnforcement(t *testing.T) {
	tmp := t.TempDir()
	var seenCalls [][]string
	catalogJSON := `{"ok":true,"commands":[` +
		`{"route":"omarchy version","binary":"omarchy-version","group":"version","name":"version","summary":"Show Omarchy version","requires_sudo":false,"hidden":false},` +
		`{"route":"omarchy hw platform","binary":"omarchy-hw-platform","group":"hw","name":"platform","summary":"Print hardware platform","requires_sudo":false,"hidden":false},` +
		`{"route":"omarchy restart trackpad","binary":"omarchy-restart-trackpad","group":"restart","name":"trackpad","summary":"Reload trackpad","requires_sudo":true,"hidden":false},` +
		`{"route":"omarchy clipboard copy","binary":"omarchy-clipboard-copy","group":"clipboard","name":"copy","summary":"Clipboard helper","requires_sudo":false,"hidden":true},` +
		`{"route":"omarchy debug","binary":"omarchy-debug","group":"debug","name":"debug","summary":"Diagnostics","requires_sudo":true,"hidden":false}` +
		`]}`

	r := &Runner{
		Env: []string{
			"HOME=" + tmp,
			"PATH=/usr/bin:/bin",
			"WAYLAND_DISPLAY=wayland-1",
			"HYPRLAND_INSTANCE_SIGNATURE=hypr_sig_1",
			"XDG_CURRENT_DESKTOP=Hyprland",
			"SECRET_API_CRED=must_not_leak",
		},
		ConfigHome: filepath.Join(tmp, ".config"),
		HomeDir:    tmp,
		ExecFunc: func(_ context.Context, env []string, binary string, args ...string) (ExecResult, error) {
			for _, kv := range env {
				if strings.HasPrefix(kv, "SECRET_") {
					t.Fatalf("unsafe env var leaked into child process: %s", kv)
				}
			}
			seenCalls = append(seenCalls, append([]string{binary}, args...))
			key := binary + " " + strings.Join(args, " ")
			switch key {
			case "omarchy version":
				return ExecResult{Stdout: "4.5.0-3"}, nil
			case "omarchy version branch":
				return ExecResult{Stdout: "main"}, nil
			case "omarchy version channel":
				return ExecResult{Stdout: "stable"}, nil
			case "omarchy theme current":
				return ExecResult{Stdout: "Mineral Slate"}, nil
			case "omarchy font current":
				return ExecResult{Stdout: "Maple Mono"}, nil
			case "hyprctl configerrors":
				return ExecResult{Stdout: ""}, nil
			case "omarchy commands --all --json", "omarchy commands --json":
				return ExecResult{Stdout: catalogJSON}, nil
			case "omarchy hw platform":
				return ExecResult{Stdout: "x86"}, nil
			case "omarchy hw nvidia gsp":
				return ExecResult{Stdout: "true", ExitCode: 0}, nil
			case "omarchy debug --no-sudo --print":
				return ExecResult{Stdout: "Date: today\n=========================================\nSYSTEM INFORMATION\n=========================================\nHost: synthetic\nUserPath: " + tmp + "/work\n=========================================\nDMESG\n=========================================\n(skipped - --no-sudo flag used)"}, nil
			default:
				return ExecResult{Stdout: "ok"}, nil
			}
		},
	}
	srv := newTestServer(t, r)

	st, res := callToolJSON[StatusOutput](t, srv, "omarchy_status", map[string]any{})
	if res.IsError {
		t.Fatalf("omarchy_status failed: %+v", res)
	}
	if st.Version != "4.5.0-3" || st.Flavor != FlavorHyprland || st.ClipboardPolicy != "clipse" || st.GhosttyReloadSignal != "SIGUSR2" {
		t.Fatalf("unexpected status output: %+v", st)
	}

	cmds, _ := callToolJSON[CommandsOutput](t, srv, "omarchy_commands", map[string]any{
		"include_hidden": true,
		"exclude_sudo":   true,
	})
	if cmds.TotalMatched != 3 {
		t.Fatalf("expected 3 non-sudo commands, got %d (%+v)", cmds.TotalMatched, cmds.Commands)
	}

	hwList, _ := callToolJSON[HwOutput](t, srv, "omarchy_hw", map[string]any{"action": "list"})
	if hwList.Platform != "x86" || len(hwList.Detectors) != 1 {
		t.Fatalf("unexpected hw list output: %+v", hwList)
	}

	hwProbe, _ := callToolJSON[HwOutput](t, srv, "omarchy_hw", map[string]any{"action": "probe", "detector": "nvidia gsp"})
	if !hwProbe.Matched {
		t.Fatalf("expected nvidia gsp probe matched=true, got %+v", hwProbe)
	}

	dbg, _ := callToolJSON[DebugOutput](t, srv, "omarchy_debug", map[string]any{"section": "SYSTEM INFORMATION"})
	if !slices.Equal(dbg.Command, []string{"omarchy", "debug", "--no-sudo", "--print"}) {
		t.Fatalf("unexpected debug command: %v", dbg.Command)
	}
	if strings.Contains(dbg.Output, tmp) || !strings.Contains(dbg.Output, "~/work") {
		t.Fatalf("expected home directory masked in debug output, got: %s", dbg.Output)
	}

	_, denyRes := callToolJSON[DebugOutput](t, srv, "omarchy_debug", map[string]any{"disallow_sudo": false})
	if !denyRes.IsError {
		t.Fatal("expected disallow_sudo=false to be rejected")
	}

	if _, err := r.Run(context.Background(), "omarchy", "debug", "--print"); err == nil {
		t.Fatal("expected Runner.Run to reject omarchy debug without --no-sudo")
	}
	if _, err := r.Run(context.Background(), "omarchy", "debug", "--no-sudo"); err == nil {
		t.Fatal("expected Runner.Run to reject omarchy debug without --print")
	}
	_ = seenCalls
}

func TestConfigValidate_GhosttyHooksAndNegativeControls(t *testing.T) {
	t.Run("valid_ghostty_include_order_and_executable_hooks", func(t *testing.T) {
		tmp := t.TempDir()
		cfgHome := filepath.Join(tmp, ".config")
		ghosttyDir := filepath.Join(cfgHome, "ghostty")
		hooksDir := filepath.Join(cfgHome, "omarchy", "hooks", "theme-set.d")
		hyprDir := filepath.Join(cfgHome, "hypr")
		if err := os.MkdirAll(ghosttyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(hyprDir, 0o755); err != nil {
			t.Fatal(err)
		}

		ghosttyCfg := strings.Join([]string{
			`font-family = "Maple Mono"`,
			`background = "#1a1f24"`,
			`config-file = ?~/.config/ghostty/fleet-cockpit.conf`,
			`config-file = ?~/.config/ghostty/font-size-current.conf`,
		}, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(ghosttyDir, "config"), []byte(ghosttyCfg), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ghosttyDir, "fleet-cockpit.conf"), []byte("cursor-style = bar\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ghosttyDir, "font-size-current.conf"), []byte("font-size = 9.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooksDir, "10-reload.sh"), []byte("#!/bin/sh\nomarchy restart terminal\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hyprDir, "hyprland.lua"), []byte("return {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		r := &Runner{
			Env: []string{
				"HOME=" + tmp,
				"WAYLAND_DISPLAY=wayland-1",
				"HYPRLAND_INSTANCE_SIGNATURE=sig1",
			},
			ConfigHome: cfgHome,
			HomeDir:    tmp,
			ExecFunc: func(_ context.Context, _ []string, _ string, _ ...string) (ExecResult, error) {
				return ExecResult{Stdout: ""}, nil
			},
		}
		srv := newTestServer(t, r)
		rep, res := callToolJSON[ConfigValidationReport](t, srv, "omarchy_config_validate", map[string]any{})
		if res.IsError || !rep.Valid {
			t.Fatalf("expected valid config report, got res=%+v rep=%+v", res, rep)
		}
		if !rep.Ghostty.FleetCockpitIncluded || !rep.Ghostty.FontSizeCurrentLast {
			t.Fatalf("expected fleet-cockpit + final font-size-current, got %+v", rep.Ghostty)
		}
	})

	t.Run("detects_misordered_includes_pure_black_cyberpunk_wlcopy_sigusr1_and_bad_hooks", func(t *testing.T) {
		tmp := t.TempDir()
		cfgHome := filepath.Join(tmp, ".config")
		ghosttyDir := filepath.Join(cfgHome, "ghostty")
		shaderDir := filepath.Join(ghosttyDir, "shaders")
		hooksDir := filepath.Join(cfgHome, "omarchy", "hooks", "theme-set.d")
		if err := os.MkdirAll(shaderDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}

		badGhostty := strings.Join([]string{
			"background = " + "#" + "000000",
			`config-file = ?~/.config/ghostty/font-size-current.conf`,
			`config-file = ?~/.config/ghostty/fleet-cockpit.conf`,
		}, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(ghosttyDir, "config"), []byte(badGhostty), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(shaderDir, "cyberpunk-crt.glsl"), []byte("void main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooksDir, "20-nonexec.sh"), []byte("#!/bin/sh\n"+"wl-"+"copy < /dev/null\nkillall -"+"SIGUSR"+"1 ghostty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooksDir, "30-old.sh.bak.1"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		r := &Runner{
			Env:        []string{"HOME=" + tmp},
			ConfigHome: cfgHome,
			HomeDir:    tmp,
		}
		srv := newTestServer(t, r)
		rep, res := callToolJSON[ConfigValidationReport](t, srv, "omarchy_config_validate", map[string]any{})
		if res.IsError {
			t.Fatalf("unexpected transport error: %+v", res)
		}
		if rep.Valid {
			t.Fatalf("expected invalid report for adversarial defects, got %+v", rep)
		}

		codes := map[string]bool{}
		for _, iss := range rep.Issues {
			codes[iss.Code] = true
		}
		for _, wantCode := range []string{
			"GHOSTTY_INCLUDE_ORDER",
			"FORBIDDEN_PURE_BLACK",
			"FORBIDDEN_CYBERPUNK_SHADER",
			"HOOK_NOT_EXECUTABLE",
			"HOOK_BACKUP_ARTIFACT",
			"FORBIDDEN_WL_CLIPBOARD",
			"FORBIDDEN_GHOSTTY_SIGUSR1",
		} {
			if !codes[wantCode] {
				t.Fatalf("missing expected negative-control issue code %s in %+v", wantCode, rep.Issues)
			}
		}
	})
}

func TestControlledActions_NegativeControlsAndDryRun(t *testing.T) {
	tmp := t.TempDir()
	r := &Runner{
		Env: []string{
			"HOME=" + tmp,
			"WAYLAND_DISPLAY=wayland-1",
			"HYPRLAND_INSTANCE_SIGNATURE=sig1",
		},
		ConfigHome: filepath.Join(tmp, ".config"),
		HomeDir:    tmp,
		ExecFunc: func(_ context.Context, _ []string, binary string, args ...string) (ExecResult, error) {
			return ExecResult{Stdout: binary + " " + strings.Join(args, " ")}, nil
		},
	}
	srv := newTestServer(t, r)

	dryOut, res := callToolJSON[ActionOutput](t, srv, "omarchy_restart", map[string]any{
		"target":  "terminal",
		"signal":  "SIGUSR2",
		"dry_run": true,
	})
	if res.IsError || !dryOut.DryRun || dryOut.Status != "DRY_RUN" {
		t.Fatalf("expected dry-run restart terminal success, got res=%+v out=%+v", res, dryOut)
	}

	badCases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"ghostty_sigusr1", "omarchy_restart", map[string]any{"target": "terminal", "signal": "SIG" + "USR1"}},
		{"sudo_trackpad_restart", "omarchy_restart", map[string]any{"target": "trackpad"}},
		{"sudo_hybrid_gpu_toggle", "omarchy_toggle", map[string]any{"target": "hybrid_gpu"}},
		{"interactive_reminder", "omarchy_reminder", map[string]any{"action": "interactive"}},
		{"cyberpunk_theme", "omarchy_theme", map[string]any{"action": "set", "name": "cyberpunk"}},
		{"snazzy_black_theme", "omarchy_theme", map[string]any{"action": "set", "name": "Snazzy Black"}},
		{"pure_black_theme", "omarchy_theme", map[string]any{"action": "set", "name": "#" + "000000"}},
		{"wl_copy_injection", "omarchy_reminder", map[string]any{"action": "set", "minutes": 5, "message": "run wl-" + "copy now"}},
		{"wl_paste_injection", "omarchy_reminder", map[string]any{"action": "set", "minutes": 5, "message": "run wl-" + "paste now"}},
		{"shell_metachar", "omarchy_font", map[string]any{"action": "set", "name": "Maple Mono; id"}},
		{"flag_injection", "omarchy_font", map[string]any{"action": "set", "name": "--help"}},
		{"hook_path_traversal", "omarchy_hook", map[string]any{"action": "run", "hook_name": "../escape"}},
		{"hook_usr_share_write", "omarchy_hook", map[string]any{"action": "install", "hook_name": "theme-set", "source_file": "/usr/share/omarchy/default/hook.sh"}},
		{"credential_path_block", "omarchy_hook", map[string]any{"action": "install", "hook_name": "theme-set", "source_file": tmp + "/.ssh/id_ed25519"}},
		{"mutating_hw_detector", "omarchy_hw", map[string]any{"action": "probe", "detector": "recover internal monitor"}},
	}

	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			_, r := callToolJSON[ActionOutput](t, srv, tc.tool, tc.args)
			if !r.IsError {
				t.Fatalf("expected %s (%s) to be rejected", tc.name, tc.tool)
			}
		})
	}

	if _, err := r.Run(context.Background(), "sh", "-c", "echo unsafe"); err == nil {
		t.Fatal("expected arbitrary shell binary to be rejected by Runner.Run")
	}

	pluginDir := filepath.Join(tmp, "hg.sample-widget")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reservedManifest := `{"schemaVersion":1,"id":"omarchy.reserved","name":"Bad","version":"0.1.0","kinds":["bar"],"entryPoints":{"main":"Main.qml"}}`
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte(reservedManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	_, reservedRes := callToolJSON[ActionOutput](t, srv, "omarchy_plugin", map[string]any{
		"action": "validate",
		"folder": pluginDir,
	})
	if !reservedRes.IsError {
		t.Fatal("expected reserved omarchy.* plugin id to fail validation")
	}

	validManifest := `{"schemaVersion":1,"id":"local.sample-widget","name":"Good","version":"0.1.0","kinds":["bar"],"entryPoints":{"main":"Main.qml"}}`
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(pluginDir, "Main.qml")); err != nil {
		t.Fatal(err)
	}
	_, symlinkRes := callToolJSON[ActionOutput](t, srv, "omarchy_plugin", map[string]any{
		"action": "validate",
		"folder": pluginDir,
	})
	if !symlinkRes.IsError {
		t.Fatal("expected symlink inside plugin folder to fail validation")
	}
}
