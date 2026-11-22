package omarchy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

type StatusInput struct {
	IncludeConfigCheck *bool  `json:"include_config_check,omitempty" jsonschema:"description=Include local Ghostty\\, hook\\, and Hyprland config validation summary (default true)"`
	Component          string `json:"component,omitempty" jsonschema:"description=Optional config validation scope: all\\, ghostty\\, hooks\\, or hyprland"`
}

type StatusOutput struct {
	Version             string   `json:"version"`
	Branch              string   `json:"branch,omitempty"`
	Channel             string   `json:"channel,omitempty"`
	Flavor              string   `json:"flavor"`
	SkipStatus          string   `json:"skip_status,omitempty"`
	CurrentTheme        string   `json:"current_theme,omitempty"`
	CurrentFont         string   `json:"current_font,omitempty"`
	HyprConfigErrors    []string `json:"hypr_config_errors"`
	ConfigValid         bool     `json:"config_valid"`
	ConfigIssueCount    int      `json:"config_issue_count"`
	ClipboardPolicy     string   `json:"clipboard_policy"`
	GhosttyReloadSignal string   `json:"ghostty_reload_signal"`
}

type CommandsInput struct {
	Group         string `json:"group,omitempty" jsonschema:"description=Filter Omarchy commands by group name (e.g. theme\\, font\\, plugin\\, bar\\, hook\\, restart\\, toggle\\, reminder\\, hw\\, hyprland)"`
	Query         string `json:"query,omitempty" jsonschema:"description=Case-insensitive substring filter over route\\, binary\\, and summary"`
	IncludeHidden bool   `json:"include_hidden,omitempty" jsonschema:"description=Include hidden commands via omarchy commands --all --json"`
	ExcludeSudo   bool   `json:"exclude_sudo,omitempty" jsonschema:"description=Exclude commands marked requires_sudo"`
	Limit         int    `json:"limit,omitempty" jsonschema:"description=Maximum commands returned (default 50\\, max 200)"`
}

type CommandEntry struct {
	Route        string `json:"route"`
	Binary       string `json:"binary"`
	Group        string `json:"group"`
	Name         string `json:"name"`
	Summary      string `json:"summary"`
	Args         string `json:"args,omitempty"`
	RequiresSudo bool   `json:"requires_sudo"`
	Hidden       bool   `json:"hidden"`
	PolicyNote   string `json:"policy_note,omitempty"`
}

type CommandsOutput struct {
	TotalMatched int            `json:"total_matched"`
	Returned     int            `json:"returned"`
	Commands     []CommandEntry `json:"commands"`
	Groups       []string       `json:"groups"`
}

type HwInput struct {
	Action   string `json:"action,omitempty" jsonschema:"description=Operation: list (default)\\, platform\\, or probe"`
	Detector string `json:"detector,omitempty" jsonschema:"description=Read-only hardware detector name when action is probe (e.g. x86\\, aarch64\\, laptop\\, vm\\, nvidia\\, nvidia gsp\\, vulkan\\, external monitors\\, display\\, touchpad)"`
}

type HwOutput struct {
	Action    string         `json:"action"`
	Platform  string         `json:"platform,omitempty"`
	Detector  string         `json:"detector,omitempty"`
	Matched   bool           `json:"matched"`
	Output    string         `json:"output,omitempty"`
	Detectors []CommandEntry `json:"detectors"`
}

type DebugInput struct {
	Section      string `json:"section,omitempty" jsonschema:"description=Optional section filter: SYSTEM INFORMATION\\, DMESG\\, JOURNALCTL\\, or INSTALLED PACKAGES"`
	MaxLines     int    `json:"max_lines,omitempty" jsonschema:"description=Maximum output lines to return (default 120\\, max 400)"`
	DisallowSudo *bool  `json:"disallow_sudo,omitempty" jsonschema:"description=Must remain true; omarchy debug always runs with --no-sudo --print"`
}

type DebugOutput struct {
	Command   []string `json:"command"`
	LineCount int      `json:"line_count"`
	Truncated bool     `json:"truncated"`
	Output    string   `json:"output"`
}

type ConfigValidateInput struct {
	Component string `json:"component,omitempty" jsonschema:"description=Surface to validate: all (default)\\, ghostty\\, hooks\\, omarchy\\, or hyprland"`
}

type ActionOutput struct {
	Action     string   `json:"action"`
	Status     string   `json:"status"`
	Flavor     string   `json:"flavor"`
	SkipStatus string   `json:"skip_status,omitempty"`
	DryRun     bool     `json:"dry_run"`
	Command    []string `json:"command"`
	Output     string   `json:"output,omitempty"`
	Items      []string `json:"items"`
}

type ThemeInput struct {
	Action   string `json:"action" jsonschema:"description=Theme action: current\\, list\\, dir\\, color\\, set\\, refresh\\, bg_current\\, or bg_next,required"`
	Name     string `json:"name,omitempty" jsonschema:"description=Theme name for set or dir"`
	ColorKey string `json:"color_key,omitempty" jsonschema:"description=Color key or --all for color action"`
	DryRun   bool   `json:"dry_run,omitempty" jsonschema:"description=Preview command without applying live desktop mutation"`
}

type FontInput struct {
	Action     string `json:"action" jsonschema:"description=Font action: current\\, list\\, set\\, or cjk,required"`
	Name       string `json:"name,omitempty" jsonschema:"description=Monospace font family name for set"`
	CJKVariant string `json:"cjk_variant,omitempty" jsonschema:"description=CJK locale variant for cjk action: jp\\, kr\\, sc\\, tc\\, hk\\, or default"`
	DryRun     bool   `json:"dry_run,omitempty" jsonschema:"description=Preview command without mutating font configuration"`
}

type PluginInput struct {
	Action    string `json:"action" jsonschema:"description=Plugin action: list\\, catalog\\, validate\\, enable\\, disable\\, or clone,required"`
	ID        string `json:"id,omitempty" jsonschema:"description=Plugin identifier for enable\\, disable\\, or clone"`
	Folder    string `json:"folder,omitempty" jsonschema:"description=Plugin directory path for validate"`
	Placement string `json:"placement,omitempty" jsonschema:"description=Optional bar section for enable: left\\, centerBegin\\, center\\, centerEnd\\, tray\\, or right"`
	DryRun    bool   `json:"dry_run,omitempty" jsonschema:"description=Preview command without mutating shell plugin state"`
}

type BarInput struct {
	Action    string `json:"action" jsonschema:"description=Bar action: position\\, background\\, get\\, use\\, reset\\, defaults\\, put\\, move\\, or set,required"`
	WidgetID  string `json:"widget_id,omitempty" jsonschema:"description=Widget or bar plugin ID"`
	Section   string `json:"section,omitempty" jsonschema:"description=Target bar section: left\\, centerBegin\\, center\\, centerEnd\\, tray\\, or right"`
	Key       string `json:"key,omitempty" jsonschema:"description=Widget option key for get or set"`
	Value     string `json:"value,omitempty" jsonschema:"description=Position\\, background mode\\, or widget option value"`
	JSONValue bool   `json:"json_value,omitempty" jsonschema:"description=Pass --json when setting a structured widget option"`
	DryRun    bool   `json:"dry_run,omitempty" jsonschema:"description=Preview command without mutating shell.json"`
}

type HookInput struct {
	Action     string   `json:"action" jsonschema:"description=Hook action: list\\, validate\\, run\\, or install,required"`
	HookName   string   `json:"hook_name,omitempty" jsonschema:"description=Hook type name (e.g. theme-set\\, font-set\\, post-update)"`
	SourceFile string   `json:"source_file,omitempty" jsonschema:"description=Source script path for install"`
	Args       []string `json:"args,omitempty" jsonschema:"description=Optional safe positional arguments passed to run"`
	DryRun     bool     `json:"dry_run,omitempty" jsonschema:"description=Preview command without installing or executing hooks"`
}

type RestartInput struct {
	Target string `json:"target" jsonschema:"description=Controlled restart target: terminal\\, shell\\, hyprsunset\\, xcompose\\, btop\\, tmux\\, audio\\, or waybar,required"`
	Signal string `json:"signal,omitempty" jsonschema:"description=Optional terminal reload signal check (only SIGUSR2 allowed; SIGUSR1 is blocked)"`
	DryRun bool   `json:"dry_run,omitempty" jsonschema:"description=Preview restart command without signaling live processes"`
}

type ToggleInput struct {
	Target string `json:"target" jsonschema:"description=Controlled toggle target: nightlight\\, idle\\, bar\\, animations\\, screensaver\\, notification_silencing\\, fullscreen_desktop\\, touchpad\\, touchscreen\\, theme_sync\\, crash_capture\\, suspend\\, enabled\\, or flag,required"`
	State  string `json:"state,omitempty" jsonschema:"description=Desired state: toggle\\, on\\, off\\, status\\, stay-awake\\, allow-idle\\, or cycle"`
	Flag   string `json:"flag,omitempty" jsonschema:"description=Flag identifier when target is enabled or flag"`
	DryRun bool   `json:"dry_run,omitempty" jsonschema:"description=Preview toggle command without changing desktop state"`
}

type ReminderInput struct {
	Action  string `json:"action" jsonschema:"description=Reminder action: show\\, set\\, or clear,required"`
	Minutes int    `json:"minutes,omitempty" jsonschema:"description=Reminder delay in minutes (1..10080) when action is set"`
	Message string `json:"message,omitempty" jsonschema:"description=Optional short reminder label when action is set"`
	DryRun  bool   `json:"dry_run,omitempty" jsonschema:"description=Preview reminder command without modifying user timers"`
}

var (
	allowedReadOnlyHwDetectors = map[string][]string{
		"aarch64":                  {"hw", "aarch64"},
		"aarch64 apple":            {"hw", "aarch64", "apple"},
		"aarch64 gb10":             {"hw", "aarch64", "gb10"},
		"aarch64 n1x":              {"hw", "aarch64", "n1x"},
		"aarch64 qualcomm":         {"hw", "aarch64", "qualcomm"},
		"apple silicon":            {"hw", "apple", "silicon"},
		"asus expertbook b9406":    {"hw", "asus", "expertbook", "b9406"},
		"asus rog":                 {"hw", "asus", "rog"},
		"asus zenbook ux5406aa":    {"hw", "asus", "zenbook", "ux5406aa"},
		"clamshell":                {"hw", "clamshell"},
		"dell xps haptic touchpad": {"hw", "dell", "xps", "haptic", "touchpad"},
		"dell xps oled":            {"hw", "dell", "xps", "oled"},
		"dell xps13 dx13260 ptl":   {"hw", "dell", "xps13", "dx13260", "ptl"},
		"dell xps13 sidecar amps":  {"hw", "dell", "xps13", "sidecar", "amps"},
		"display":                  {"hw", "display"},
		"elgato camlink 4k":        {"hw", "elgato", "camlink", "4k"},
		"external monitors":        {"hw", "external", "monitors"},
		"fingerprint":              {"hw", "fingerprint"},
		"framework16":              {"hw", "framework16"},
		"hybrid gpu":               {"hw", "hybrid", "gpu"},
		"intel":                    {"hw", "intel"},
		"intel ptl":                {"hw", "intel", "ptl"},
		"intel sof":                {"hw", "intel", "sof"},
		"laptop":                   {"hw", "laptop"},
		"laptop closed":            {"hw", "laptop", "closed"},
		"nvidia":                   {"hw", "nvidia"},
		"nvidia display":           {"hw", "nvidia", "display"},
		"nvidia gsp":               {"hw", "nvidia", "gsp"},
		"nvidia without gsp":       {"hw", "nvidia", "without", "gsp"},
		"platform":                 {"hw", "platform"},
		"surface":                  {"hw", "surface"},
		"touchpad":                 {"hw", "touchpad"},
		"touchscreen":              {"hw", "touchscreen"},
		"vm":                       {"hw", "vm"},
		"vulkan":                   {"hw", "vulkan"},
		"webcam":                   {"hw", "webcam"},
		"x86":                      {"hw", "x86"},
	}
	allowedBarSections = map[string]bool{
		"left": true, "centerBegin": true, "center": true,
		"centerEnd": true, "tray": true, "right": true,
	}
	allowedBarPositions = map[string]bool{
		"top": true, "bottom": true, "left": true, "right": true,
	}
	allowedBarBackgrounds = map[string]bool{
		"opaque": true, "glassy": true, "none": true, "cycle": true,
	}
	allowedCJKVariants = map[string]bool{
		"jp": true, "kr": true, "sc": true, "tc": true, "hk": true, "default": true,
	}
)

type rawCatalogDoc struct {
	OK       bool              `json:"ok"`
	Commands []rawCatalogEntry `json:"commands"`
}

type rawCatalogEntry struct {
	Route        string `json:"route"`
	Binary       string `json:"binary"`
	Group        string `json:"group"`
	Name         string `json:"name"`
	Summary      string `json:"summary"`
	Args         string `json:"args"`
	RequiresSudo bool   `json:"requires_sudo"`
	Hidden       bool   `json:"hidden"`
}

func boolPtr(v bool) *bool { return &v }

func splitLinesNonEmpty(s string) []string {
	out := []string{}
	for line := range strings.SplitSeq(s, "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func skipOrDryRun(action, flavor, skipStatus string, dryRun bool, cmd []string, requireDesktop bool) (ActionOutput, bool) {
	fullCmd := append([]string{"omarchy"}, cmd...)
	if dryRun {
		return ActionOutput{
			Action:     action,
			Status:     "DRY_RUN",
			Flavor:     flavor,
			SkipStatus: skipStatus,
			DryRun:     true,
			Command:    fullCmd,
			Items:      []string{},
		}, true
	}
	if requireDesktop && skipStatus != "" {
		return ActionOutput{
			Action:     action,
			Status:     skipStatus,
			Flavor:     flavor,
			SkipStatus: skipStatus,
			DryRun:     false,
			Command:    fullCmd,
			Items:      []string{},
		}, true
	}
	return ActionOutput{}, false
}

func execControlledAction(ctx context.Context, r *Runner, action string, dryRun, requireDesktop bool, args ...string) (ActionOutput, error) {
	flavor, skipStatus := r.DetectFlavor()
	if out, handled := skipOrDryRun(action, flavor, skipStatus, dryRun, args, requireDesktop); handled {
		return out, nil
	}
	res, err := r.Run(ctx, "omarchy", args...)
	fullCmd := append([]string{"omarchy"}, args...)
	if err != nil {
		msg := res.Stderr
		if msg == "" {
			msg = res.Stdout
		}
		if msg == "" {
			msg = err.Error()
		}
		return ActionOutput{}, fmt.Errorf("[%s] %s failed: %s", handler.ErrUpstreamError, strings.Join(fullCmd, " "), msg)
	}
	return ActionOutput{
		Action:  action,
		Status:  "OK",
		Flavor:  flavor,
		DryRun:  false,
		Command: fullCmd,
		Output:  res.Stdout,
		Items:   splitLinesNonEmpty(res.Stdout),
	}, nil
}

func loadCommandCatalog(ctx context.Context, r *Runner, includeHidden bool) ([]CommandEntry, error) {
	args := []string{"commands", "--json"}
	if includeHidden {
		args = []string{"commands", "--all", "--json"}
	}
	res, err := r.Run(ctx, "omarchy", args...)
	if err != nil {
		return nil, fmt.Errorf("[%s] omarchy commands failed: %w", handler.ErrUpstreamError, err)
	}
	var doc rawCatalogDoc
	if uerr := json.Unmarshal([]byte(res.Stdout), &doc); uerr != nil {
		return nil, fmt.Errorf("[%s] parse omarchy commands json: %w", handler.ErrValidation, uerr)
	}
	entries := make([]CommandEntry, 0, len(doc.Commands))
	for _, c := range doc.Commands {
		note := ""
		if c.Group == "clipboard" || ContainsForbiddenClipboard(c.Route) || ContainsForbiddenClipboard(c.Binary) {
			note = "clipboard_policy:clipse_only"
		} else if c.Route == "omarchy debug" || c.Binary == "omarchy-debug" {
			note = "requires_flags:--no-sudo --print"
		} else if c.RequiresSudo {
			note = "requires_sudo"
		}
		entries = append(entries, CommandEntry{
			Route:        c.Route,
			Binary:       c.Binary,
			Group:        c.Group,
			Name:         c.Name,
			Summary:      c.Summary,
			Args:         c.Args,
			RequiresSudo: c.RequiresSudo,
			Hidden:       c.Hidden,
			PolicyNote:   note,
		})
	}
	return entries, nil
}

func validatePluginFolderLocal(folder string) (string, error) {
	cleanDir, err := ValidateSafeUserFile(folder, false)
	if err != nil {
		return "", err
	}
	fi, serr := os.Lstat(cleanDir)
	if serr != nil || !fi.IsDir() {
		return "", fmt.Errorf("[%s] plugin folder must be an existing directory", handler.ErrInvalidParam)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("[%s] plugin folder must not be a symlink", ErrPolicyDenied)
	}
	walkErr := filepath.WalkDir(cleanDir, func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("[%s] symlink %s disallowed inside Omarchy plugin folder", ErrPolicyDenied, filepath.Base(p))
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	manifestPath := filepath.Join(cleanDir, "manifest.json")
	raw, rerr := readBoundedRegularFile(manifestPath, 64*1024)
	if rerr != nil {
		return "", fmt.Errorf("[%s] missing or unreadable manifest.json", handler.ErrInvalidParam)
	}
	var m struct {
		SchemaVersion int    `json:"schemaVersion"`
		ID            string `json:"id"`
	}
	if jerr := json.Unmarshal([]byte(raw), &m); jerr != nil {
		return "", fmt.Errorf("[%s] manifest.json is invalid JSON", handler.ErrInvalidParam)
	}
	if m.SchemaVersion != 1 {
		return "", fmt.Errorf("[%s] manifest schemaVersion must be 1", handler.ErrInvalidParam)
	}
	if strings.HasPrefix(m.ID, "omarchy.") {
		return "", fmt.Errorf("[%s] plugin id %q uses reserved omarchy.* namespace", ErrPolicyDenied, m.ID)
	}
	if err := ValidateIdentifier("id", m.ID); err != nil {
		return "", err
	}
	return cleanDir, nil
}

type OmarchyModule struct{}

func (m *OmarchyModule) Name() string { return "omarchy" }
func (m *OmarchyModule) Description() string {
	return "Native Omarchy 4.5, Hyprland, and Ghostty workstation introspection and controlled actions"
}

func (m *OmarchyModule) Tools() []registry.ToolDefinition {
	statusTool := handler.TypedHandler[StatusInput, StatusOutput](
		"omarchy_status",
		"Inspect Omarchy version, release channel, desktop flavor fencing (hyprland, SKIPPED_NIRI, SKIPPED_HEADLESS), active theme and font, Hyprland configerrors, and Ghostty/hook hygiene.",
		func(ctx context.Context, input StatusInput) (StatusOutput, error) {
			r := GetRunner()
			flavor, skipStatus := r.DetectFlavor()
			out := StatusOutput{
				Flavor:              flavor,
				SkipStatus:          skipStatus,
				HyprConfigErrors:    []string{},
				ConfigValid:         true,
				ClipboardPolicy:     ClipboardPolicy,
				GhosttyReloadSignal: GhosttyReloadSignal,
			}
			if vRes, err := r.Run(ctx, "omarchy", "version"); err == nil {
				out.Version = vRes.Stdout
			}
			if bRes, err := r.Run(ctx, "omarchy", "version", "branch"); err == nil {
				out.Branch = bRes.Stdout
			}
			if cRes, err := r.Run(ctx, "omarchy", "version", "channel"); err == nil {
				out.Channel = cRes.Stdout
			}
			if tRes, err := r.Run(ctx, "omarchy", "theme", "current"); err == nil {
				out.CurrentTheme = tRes.Stdout
			}
			if fRes, err := r.Run(ctx, "omarchy", "font", "current"); err == nil {
				out.CurrentFont = fRes.Stdout
			}
			checkCfg := input.IncludeConfigCheck == nil || *input.IncludeConfigCheck
			if checkCfg {
				rep, err := ValidateConfigs(ctx, r, input.Component)
				if err != nil {
					return StatusOutput{}, err
				}
				out.ConfigValid = rep.Valid
				out.ConfigIssueCount = len(rep.Issues)
				out.HyprConfigErrors = rep.Hyprland.ConfigErrors
			}
			return out, nil
		},
	)
	statusTool.Category = "inspection"
	statusTool.ReadOnlyOverride = boolPtr(true)

	commandsTool := handler.TypedHandler[CommandsInput, CommandsOutput](
		"omarchy_commands",
		"Query Omarchy 4.5 native CLI command catalog via omarchy commands --json with group, substring, hidden, and sudo filters.",
		func(ctx context.Context, input CommandsInput) (CommandsOutput, error) {
			if input.Group != "" {
				if err := ValidateIdentifier("group", input.Group); err != nil {
					return CommandsOutput{}, err
				}
			}
			if input.Query != "" {
				if err := ValidateArg(input.Query); err != nil {
					return CommandsOutput{}, err
				}
			}
			limit := input.Limit
			if limit <= 0 {
				limit = 50
			}
			if limit > 200 {
				limit = 200
			}
			r := GetRunner()
			all, err := loadCommandCatalog(ctx, r, input.IncludeHidden)
			if err != nil {
				return CommandsOutput{}, err
			}
			groupSet := map[string]bool{}
			matched := make([]CommandEntry, 0, len(all))
			qLower := strings.ToLower(strings.TrimSpace(input.Query))
			gFilter := strings.ToLower(strings.TrimSpace(input.Group))
			for _, c := range all {
				if c.Group != "" {
					groupSet[c.Group] = true
				}
				if gFilter != "" && strings.ToLower(c.Group) != gFilter {
					continue
				}
				if input.ExcludeSudo && c.RequiresSudo {
					continue
				}
				if qLower != "" {
					hay := strings.ToLower(c.Route + " " + c.Binary + " " + c.Summary)
					if !strings.Contains(hay, qLower) {
						continue
					}
				}
				matched = append(matched, c)
			}
			groups := make([]string, 0, len(groupSet))
			for g := range groupSet {
				groups = append(groups, g)
			}
			sort.Strings(groups)
			total := len(matched)
			if len(matched) > limit {
				matched = matched[:limit]
			}
			return CommandsOutput{
				TotalMatched: total,
				Returned:     len(matched),
				Commands:     matched,
				Groups:       groups,
			}, nil
		},
	)
	commandsTool.Category = "inspection"
	commandsTool.ReadOnlyOverride = boolPtr(true)

	hwTool := handler.TypedHandler[HwInput, HwOutput](
		"omarchy_hw",
		"Inspect Omarchy hardware detection capabilities (list 42 detectors, read platform, or run a non-mutating hardware probe).",
		func(ctx context.Context, input HwInput) (HwOutput, error) {
			r := GetRunner()
			act := strings.ToLower(strings.TrimSpace(input.Action))
			if act == "" {
				act = "list"
			}
			switch act {
			case "list":
				all, err := loadCommandCatalog(ctx, r, true)
				if err != nil {
					return HwOutput{}, err
				}
				detectors := []CommandEntry{}
				for _, c := range all {
					if c.Group == "hw" {
						detectors = append(detectors, c)
					}
				}
				plat := ""
				if pRes, perr := r.Run(ctx, "omarchy", "hw", "platform"); perr == nil {
					plat = pRes.Stdout
				}
				return HwOutput{
					Action:    "list",
					Platform:  plat,
					Matched:   len(detectors) > 0,
					Detectors: detectors,
				}, nil
			case "platform":
				res, err := r.Run(ctx, "omarchy", "hw", "platform")
				if err != nil {
					return HwOutput{}, fmt.Errorf("[%s] omarchy hw platform failed: %w", handler.ErrUpstreamError, err)
				}
				return HwOutput{
					Action:    "platform",
					Platform:  res.Stdout,
					Matched:   res.Stdout != "",
					Output:    res.Stdout,
					Detectors: []CommandEntry{},
				}, nil
			case "probe":
				det := strings.ToLower(strings.Join(strings.Fields(input.Detector), " "))
				if det == "" {
					return HwOutput{}, fmt.Errorf("[%s] detector is required when action is probe", handler.ErrInvalidParam)
				}
				if det == "recover internal monitor" {
					return HwOutput{}, fmt.Errorf("[%s] detector %q mutates monitor state and is disallowed in read-only omarchy_hw", ErrPolicyDenied, det)
				}
				args, ok := allowedReadOnlyHwDetectors[det]
				if !ok {
					return HwOutput{}, fmt.Errorf("[%s] unsupported hardware detector %q", handler.ErrInvalidParam, input.Detector)
				}
				res, err := r.Run(ctx, "omarchy", args...)
				matched := err == nil && res.ExitCode == 0
				return HwOutput{
					Action:    "probe",
					Detector:  det,
					Matched:   matched,
					Output:    res.Stdout,
					Detectors: []CommandEntry{},
				}, nil
			default:
				return HwOutput{}, fmt.Errorf("[%s] action must be list, platform, or probe", handler.ErrInvalidParam)
			}
		},
	)
	hwTool.Category = "inspection"
	hwTool.ReadOnlyOverride = boolPtr(true)

	debugTool := handler.TypedHandler[DebugInput, DebugOutput](
		"omarchy_debug",
		"Collect non-interactive Omarchy diagnostic snapshot strictly enforcing --no-sudo --print.",
		func(ctx context.Context, input DebugInput) (DebugOutput, error) {
			if input.DisallowSudo != nil && !*input.DisallowSudo {
				return DebugOutput{}, fmt.Errorf("[%s] omarchy_debug always enforces --no-sudo --print", ErrPolicyDenied)
			}
			if input.Section != "" {
				if err := ValidateArg(input.Section); err != nil {
					return DebugOutput{}, err
				}
			}
			maxLines := input.MaxLines
			if maxLines <= 0 {
				maxLines = 120
			}
			if maxLines > 400 {
				maxLines = 400
			}
			r := GetRunner()
			res, err := r.Run(ctx, "omarchy", "debug", "--no-sudo", "--print")
			if err != nil {
				return DebugOutput{}, fmt.Errorf("[%s] omarchy debug --no-sudo --print failed: %w", handler.ErrUpstreamError, err)
			}
			lines := strings.Split(res.Stdout, "\n")
			sec := strings.ToUpper(strings.TrimSpace(input.Section))
			if sec != "" {
				var filtered []string
				inSec := false
				headerCount := 0
				for _, l := range lines {
					trimmed := strings.TrimSpace(l)
					if trimmed == sec {
						inSec = true
						headerCount = 0
						filtered = append(filtered, l)
						continue
					}
					if inSec {
						if strings.HasPrefix(trimmed, "=========") {
							headerCount++
							if headerCount >= 2 {
								break
							}
							continue
						}
						filtered = append(filtered, l)
					}
				}
				lines = filtered
			}
			truncated := false
			if len(lines) > maxLines {
				lines = lines[:maxLines]
				truncated = true
			}
			return DebugOutput{
				Command:   []string{"omarchy", "debug", "--no-sudo", "--print"},
				LineCount: len(lines),
				Truncated: truncated,
				Output:    strings.TrimSpace(strings.Join(lines, "\n")),
			}, nil
		},
	)
	debugTool.Category = "inspection"
	debugTool.ReadOnlyOverride = boolPtr(true)

	validateTool := handler.TypedHandler[ConfigValidateInput, ConfigValidationReport](
		"omarchy_config_validate",
		"Validate local Ghostty include ordering, dark non-cyberpunk shader/color rules, Omarchy hook permissions, clipse clipboard hygiene, and Hyprland configerrors.",
		func(ctx context.Context, input ConfigValidateInput) (ConfigValidationReport, error) {
			return ValidateConfigs(ctx, GetRunner(), input.Component)
		},
	)
	validateTool.Category = "inspection"
	validateTool.ReadOnlyOverride = boolPtr(true)

	themeTool := handler.TypedHandler[ThemeInput, ActionOutput](
		"omarchy_theme",
		"Inspect or apply Omarchy themes and backgrounds while rejecting pure black #000000 and cyberpunk/Snazzy Black palettes.",
		func(ctx context.Context, input ThemeInput) (ActionOutput, error) {
			r := GetRunner()
			act := strings.ToLower(strings.TrimSpace(input.Action))
			switch act {
			case "current":
				return execControlledAction(ctx, r, act, input.DryRun, false, "theme", "current")
			case "list":
				return execControlledAction(ctx, r, act, input.DryRun, false, "theme", "list")
			case "dir":
				if err := ValidateThemeName(input.Name); err != nil {
					return ActionOutput{}, err
				}
				return execControlledAction(ctx, r, act, input.DryRun, false, "theme", "dir", strings.TrimSpace(input.Name))
			case "color":
				ck := strings.TrimSpace(input.ColorKey)
				if ck == "" || ck == "--all" || ck == "all" {
					return execControlledAction(ctx, r, act, input.DryRun, false, "theme", "color", "--all")
				}
				if err := ValidateIdentifier("color_key", ck); err != nil {
					return ActionOutput{}, err
				}
				return execControlledAction(ctx, r, act, input.DryRun, false, "theme", "color", ck)
			case "set":
				if err := ValidateThemeName(input.Name); err != nil {
					return ActionOutput{}, err
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, "theme", "set", strings.TrimSpace(input.Name))
			case "refresh":
				return execControlledAction(ctx, r, act, input.DryRun, true, "theme", "refresh")
			case "bg_current":
				return execControlledAction(ctx, r, act, input.DryRun, false, "theme", "bg", "current")
			case "bg_next":
				return execControlledAction(ctx, r, act, input.DryRun, true, "theme", "bg", "next")
			default:
				return ActionOutput{}, fmt.Errorf("[%s] unsupported theme action %q", handler.ErrInvalidParam, input.Action)
			}
		},
	)
	themeTool.Category = "desktop"
	themeTool.IsWrite = true
	themeTool.Complexity = registry.ComplexityModerate

	fontTool := handler.TypedHandler[FontInput, ActionOutput](
		"omarchy_font",
		"Inspect or configure Omarchy system monospace and CJK fonts with flavor fencing and dry-run support.",
		func(ctx context.Context, input FontInput) (ActionOutput, error) {
			r := GetRunner()
			act := strings.ToLower(strings.TrimSpace(input.Action))
			switch act {
			case "current":
				return execControlledAction(ctx, r, act, input.DryRun, false, "font", "current")
			case "list":
				return execControlledAction(ctx, r, act, input.DryRun, false, "font", "list")
			case "set":
				if err := ValidatePositionalArg("name", input.Name); err != nil {
					return ActionOutput{}, err
				}
				if strings.Contains(input.Name, "/") {
					return ActionOutput{}, fmt.Errorf("[%s] font name must not contain '/'", handler.ErrInvalidParam)
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, "font", "set", strings.TrimSpace(input.Name))
			case "cjk":
				v := strings.ToLower(strings.TrimSpace(input.CJKVariant))
				if v == "" {
					v = "default"
				}
				if !allowedCJKVariants[v] {
					return ActionOutput{}, fmt.Errorf("[%s] cjk_variant must be jp, kr, sc, tc, hk, or default", handler.ErrInvalidParam)
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, "font", "cjk", v)
			default:
				return ActionOutput{}, fmt.Errorf("[%s] unsupported font action %q", handler.ErrInvalidParam, input.Action)
			}
		},
	)
	fontTool.Category = "desktop"
	fontTool.IsWrite = true
	fontTool.Complexity = registry.ComplexityModerate

	pluginTool := handler.TypedHandler[PluginInput, ActionOutput](
		"omarchy_plugin",
		"Manage and validate Omarchy Quickshell plugins with zero-symlink, non-omarchy.* manifest checks, and flavor fencing.",
		func(ctx context.Context, input PluginInput) (ActionOutput, error) {
			r := GetRunner()
			act := strings.ToLower(strings.TrimSpace(input.Action))
			switch act {
			case "list":
				return execControlledAction(ctx, r, act, input.DryRun, true, "plugin", "list", "--json")
			case "catalog":
				return execControlledAction(ctx, r, act, input.DryRun, false, "plugin", "catalog")
			case "validate":
				cleanFolder, err := validatePluginFolderLocal(input.Folder)
				if err != nil {
					return ActionOutput{}, err
				}
				if input.DryRun {
					flavor, skipStatus := r.DetectFlavor()
					return ActionOutput{
						Action:     act,
						Status:     "DRY_RUN",
						Flavor:     flavor,
						SkipStatus: skipStatus,
						DryRun:     true,
						Command:    []string{"omarchy", "plugin", "validate", maskHome(cleanFolder, r.effectiveHome())},
						Items:      []string{},
					}, nil
				}
				if _, lookErr := r.Run(ctx, "omarchy", "plugin", "validate", cleanFolder); lookErr != nil {
					return ActionOutput{}, fmt.Errorf("[%s] omarchy plugin validate failed: %w", handler.ErrValidation, lookErr)
				}
				flavor, _ := r.DetectFlavor()
				return ActionOutput{
					Action:  act,
					Status:  "VALID",
					Flavor:  flavor,
					Command: []string{"omarchy", "plugin", "validate", maskHome(cleanFolder, r.effectiveHome())},
					Items:   []string{maskHome(cleanFolder, r.effectiveHome())},
				}, nil
			case "enable":
				if err := ValidateIdentifier("id", input.ID); err != nil {
					return ActionOutput{}, err
				}
				args := []string{"plugin", "enable", strings.TrimSpace(input.ID)}
				if p := strings.TrimSpace(input.Placement); p != "" {
					if !allowedBarSections[p] {
						return ActionOutput{}, fmt.Errorf("[%s] placement must be left, centerBegin, center, centerEnd, tray, or right", handler.ErrInvalidParam)
					}
					args = append(args, p)
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, args...)
			case "disable":
				if err := ValidateIdentifier("id", input.ID); err != nil {
					return ActionOutput{}, err
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, "plugin", "disable", strings.TrimSpace(input.ID))
			case "clone":
				if err := ValidateIdentifier("id", input.ID); err != nil {
					return ActionOutput{}, err
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, "plugin", "clone", strings.TrimSpace(input.ID))
			default:
				return ActionOutput{}, fmt.Errorf("[%s] unsupported plugin action %q", handler.ErrInvalidParam, input.Action)
			}
		},
	)
	pluginTool.Category = "desktop"
	pluginTool.IsWrite = true
	pluginTool.Complexity = registry.ComplexityModerate

	barTool := handler.TypedHandler[BarInput, ActionOutput](
		"omarchy_bar",
		"Inspect or update Omarchy shell bar layout, position, background, and widget options safely.",
		func(ctx context.Context, input BarInput) (ActionOutput, error) {
			r := GetRunner()
			act := strings.ToLower(strings.TrimSpace(input.Action))
			switch act {
			case "position":
				args := []string{"bar", "position"}
				if v := strings.ToLower(strings.TrimSpace(input.Value)); v != "" {
					if !allowedBarPositions[v] {
						return ActionOutput{}, fmt.Errorf("[%s] position value must be top, bottom, left, or right", handler.ErrInvalidParam)
					}
					args = append(args, v)
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, args...)
			case "background":
				args := []string{"bar", "background"}
				if v := strings.ToLower(strings.TrimSpace(input.Value)); v != "" {
					if !allowedBarBackgrounds[v] {
						return ActionOutput{}, fmt.Errorf("[%s] background value must be opaque, glassy, none, or cycle", handler.ErrInvalidParam)
					}
					args = append(args, v)
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, args...)
			case "get":
				if err := ValidateIdentifier("widget_id", input.WidgetID); err != nil {
					return ActionOutput{}, err
				}
				if err := ValidateIdentifier("key", input.Key); err != nil {
					return ActionOutput{}, err
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, "bar", "get", strings.TrimSpace(input.WidgetID), strings.TrimSpace(input.Key))
			case "use":
				if err := ValidateIdentifier("widget_id", input.WidgetID); err != nil {
					return ActionOutput{}, err
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, "bar", "use", strings.TrimSpace(input.WidgetID))
			case "reset", "defaults":
				return execControlledAction(ctx, r, act, input.DryRun, true, "bar", act)
			case "put", "move":
				if err := ValidateIdentifier("widget_id", input.WidgetID); err != nil {
					return ActionOutput{}, err
				}
				args := []string{"bar", act, strings.TrimSpace(input.WidgetID)}
				if sec := strings.TrimSpace(input.Section); sec != "" {
					if !allowedBarSections[sec] {
						return ActionOutput{}, fmt.Errorf("[%s] section must be left, centerBegin, center, centerEnd, tray, or right", handler.ErrInvalidParam)
					}
					args = append(args, "--section", sec)
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, args...)
			case "set":
				if err := ValidateIdentifier("widget_id", input.WidgetID); err != nil {
					return ActionOutput{}, err
				}
				if err := ValidateIdentifier("key", input.Key); err != nil {
					return ActionOutput{}, err
				}
				if err := ValidatePositionalArg("value", input.Value); err != nil {
					return ActionOutput{}, err
				}
				args := []string{"bar", "set", strings.TrimSpace(input.WidgetID), strings.TrimSpace(input.Key), strings.TrimSpace(input.Value)}
				if input.JSONValue {
					args = append(args, "--json")
				}
				if sec := strings.TrimSpace(input.Section); sec != "" {
					if !allowedBarSections[sec] {
						return ActionOutput{}, fmt.Errorf("[%s] section must be left, centerBegin, center, centerEnd, tray, or right", handler.ErrInvalidParam)
					}
					args = append(args, "--section", sec)
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, args...)
			default:
				return ActionOutput{}, fmt.Errorf("[%s] unsupported bar action %q", handler.ErrInvalidParam, input.Action)
			}
		},
	)
	barTool.Category = "desktop"
	barTool.IsWrite = true
	barTool.Complexity = registry.ComplexityModerate

	hookTool := handler.TypedHandler[HookInput, ActionOutput](
		"omarchy_hook",
		"List, validate, install, or run user Omarchy hooks under ~/.config/omarchy/hooks with path-traversal, clipse, and SIGUSR2 safety checks.",
		func(ctx context.Context, input HookInput) (ActionOutput, error) {
			r := GetRunner()
			flavor, skipStatus := r.DetectFlavor()
			act := strings.ToLower(strings.TrimSpace(input.Action))
			switch act {
			case "list":
				hooksRoot := filepath.Join(r.effectiveConfigHome(), "omarchy", "hooks")
				entries, err := os.ReadDir(hooksRoot)
				items := []string{}
				if err == nil {
					for _, e := range entries {
						items = append(items, e.Name())
					}
				}
				sort.Strings(items)
				return ActionOutput{
					Action:  act,
					Status:  "OK",
					Flavor:  flavor,
					Command: []string{"omarchy", "hook"},
					Items:   items,
				}, nil
			case "validate":
				rep, err := ValidateConfigs(ctx, r, "hooks")
				if err != nil {
					return ActionOutput{}, err
				}
				status := "VALID"
				if !rep.Valid {
					status = "INVALID"
				}
				items := append([]string{}, rep.Hooks.NonExecutable...)
				items = append(items, rep.Hooks.BackupArtifacts...)
				return ActionOutput{
					Action:  act,
					Status:  status,
					Flavor:  flavor,
					Command: []string{"omarchy", "hook"},
					Items:   items,
				}, nil
			case "install":
				if err := ValidateHookName("hook_name", input.HookName); err != nil {
					return ActionOutput{}, err
				}
				cleanSrc, err := ValidateSafeUserFile(input.SourceFile, false)
				if err != nil {
					return ActionOutput{}, err
				}
				body, rerr := readBoundedRegularFile(cleanSrc, 64*1024)
				if rerr != nil {
					return ActionOutput{}, fmt.Errorf("[%s] source_file must be a readable regular file: %w", handler.ErrInvalidParam, rerr)
				}
				if ContainsForbiddenClipboard(body) {
					return ActionOutput{}, fmt.Errorf("[%s] hook script invokes wl-copy or wl-paste; use clipse", ErrPolicyDenied)
				}
				if ContainsForbiddenGhosttySignal(body) {
					return ActionOutput{}, fmt.Errorf("[%s] hook script contains SIGUSR1; use SIGUSR2 or omarchy restart terminal", ErrPolicyDenied)
				}
				if input.DryRun {
					return ActionOutput{
						Action:     act,
						Status:     "DRY_RUN",
						Flavor:     flavor,
						SkipStatus: skipStatus,
						DryRun:     true,
						Command:    []string{"omarchy", "hook", "install", strings.TrimSpace(input.HookName), maskHome(cleanSrc, r.effectiveHome())},
						Items:      []string{},
					}, nil
				}
				return execControlledAction(ctx, r, act, false, false, "hook", "install", strings.TrimSpace(input.HookName), cleanSrc)
			case "run":
				if err := ValidateHookName("hook_name", input.HookName); err != nil {
					return ActionOutput{}, err
				}
				args := []string{"hook", strings.TrimSpace(input.HookName)}
				for _, a := range input.Args {
					if err := ValidatePositionalArg("args", a); err != nil {
						return ActionOutput{}, err
					}
					args = append(args, strings.TrimSpace(a))
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, args...)
			default:
				return ActionOutput{}, fmt.Errorf("[%s] unsupported hook action %q", handler.ErrInvalidParam, input.Action)
			}
		},
	)
	hookTool.Category = "desktop"
	hookTool.IsWrite = true
	hookTool.Complexity = registry.ComplexityModerate

	restartTool := handler.TypedHandler[RestartInput, ActionOutput](
		"omarchy_restart",
		"Reload or restart controlled non-sudo Omarchy components (terminal via SIGUSR2, shell, hyprsunset, xcompose, btop, tmux, audio) while blocking SIGUSR1 and sudo targets.",
		func(ctx context.Context, input RestartInput) (ActionOutput, error) {
			if sig := strings.TrimSpace(input.Signal); sig != "" {
				if ContainsForbiddenGhosttySignal(sig) {
					return ActionOutput{}, fmt.Errorf("[%s] Ghostty reload uses %s or omarchy restart terminal; SIGUSR1 is blocked", ErrPolicyDenied, GhosttyReloadSignal)
				}
				upper := strings.ToUpper(sig)
				if upper != "USR2" && upper != "SIGUSR2" {
					return ActionOutput{}, fmt.Errorf("[%s] unsupported signal %q; only SIGUSR2 is permitted", handler.ErrInvalidParam, input.Signal)
				}
			}
			target := strings.ToLower(strings.TrimSpace(input.Target))
			if target == "waybar" {
				target = "shell"
			}
			if target == "trackpad" {
				return ActionOutput{}, fmt.Errorf("[%s] omarchy restart trackpad requires sudo and is blocked", ErrPolicyDenied)
			}
			switch target {
			case "terminal", "shell", "hyprsunset", "xcompose", "btop", "tmux", "audio":
				return execControlledAction(ctx, GetRunner(), target, input.DryRun, true, "restart", target)
			default:
				return ActionOutput{}, fmt.Errorf("[%s] unsupported restart target %q", handler.ErrInvalidParam, input.Target)
			}
		},
	)
	restartTool.Category = "desktop"
	restartTool.IsWrite = true
	restartTool.Complexity = registry.ComplexityModerate

	toggleTool := handler.TypedHandler[ToggleInput, ActionOutput](
		"omarchy_toggle",
		"Toggle controlled non-sudo Omarchy desktop features (nightlight, idle, bar, animations, screensaver, notification_silencing, touchpad, touchscreen, or named flag).",
		func(ctx context.Context, input ToggleInput) (ActionOutput, error) {
			r := GetRunner()
			target := strings.ToLower(strings.TrimSpace(input.Target))
			state := strings.ToLower(strings.TrimSpace(input.State))
			if target == "hybrid_gpu" || target == "hybrid gpu" || target == "hybrid-gpu" {
				return ActionOutput{}, fmt.Errorf("[%s] omarchy toggle hybrid gpu requires sudo and is blocked", ErrPolicyDenied)
			}
			var args []string
			switch target {
			case "nightlight":
				args = []string{"toggle", "nightlight"}
				if state == "status" || state == "--status" {
					args = append(args, "--status")
				} else if state != "" && state != "toggle" {
					return ActionOutput{}, fmt.Errorf("[%s] nightlight state must be empty, toggle, or status", handler.ErrInvalidParam)
				}
			case "idle":
				args = []string{"toggle", "idle"}
				if state != "" {
					switch state {
					case "toggle", "stay-awake", "allow-idle", "status":
						args = append(args, state)
					default:
						return ActionOutput{}, fmt.Errorf("[%s] idle state must be toggle, stay-awake, allow-idle, or status", handler.ErrInvalidParam)
					}
				}
			case "bar", "animations", "touchpad", "touchscreen":
				args = []string{"toggle", target}
				if state != "" {
					switch state {
					case "toggle", "on", "off":
						args = append(args, state)
					default:
						return ActionOutput{}, fmt.Errorf("[%s] %s state must be toggle, on, or off", handler.ErrInvalidParam, target)
					}
				}
			case "fullscreen_desktop":
				args = []string{"toggle", "fullscreen", "desktop"}
				if state != "" {
					switch state {
					case "toggle", "on", "off":
						args = append(args, state)
					default:
						return ActionOutput{}, fmt.Errorf("[%s] fullscreen_desktop state must be toggle, on, or off", handler.ErrInvalidParam)
					}
				}
			case "notification_silencing":
				args = []string{"toggle", "notification", "silencing"}
			case "crash_capture":
				args = []string{"toggle", "crash", "capture"}
			case "theme_sync":
				args = []string{"toggle", "theme", "sync"}
			case "screensaver", "suspend":
				args = []string{"toggle", target}
			case "enabled":
				if err := ValidateIdentifier("flag", input.Flag); err != nil {
					return ActionOutput{}, err
				}
				return execControlledAction(ctx, r, target, input.DryRun, false, "toggle", "enabled", strings.TrimSpace(input.Flag))
			case "flag":
				if err := ValidateIdentifier("flag", input.Flag); err != nil {
					return ActionOutput{}, err
				}
				args = []string{"toggle", strings.TrimSpace(input.Flag)}
				if state != "" {
					switch state {
					case "toggle", "on", "off":
						args = append(args, state)
					default:
						return ActionOutput{}, fmt.Errorf("[%s] flag state must be toggle, on, or off", handler.ErrInvalidParam)
					}
				}
			default:
				return ActionOutput{}, fmt.Errorf("[%s] unsupported toggle target %q", handler.ErrInvalidParam, input.Target)
			}
			return execControlledAction(ctx, r, target, input.DryRun, true, args...)
		},
	)
	toggleTool.Category = "desktop"
	toggleTool.IsWrite = true
	toggleTool.Complexity = registry.ComplexityModerate

	reminderTool := handler.TypedHandler[ReminderInput, ActionOutput](
		"omarchy_reminder",
		"Show, schedule, or clear non-interactive Omarchy desktop reminders.",
		func(ctx context.Context, input ReminderInput) (ActionOutput, error) {
			r := GetRunner()
			act := strings.ToLower(strings.TrimSpace(input.Action))
			if act == "interactive" || act == "-i" || act == "--interactive" {
				return ActionOutput{}, fmt.Errorf("[%s] interactive reminder mode is blocked in non-interactive MCP execution", ErrPolicyDenied)
			}
			switch act {
			case "show":
				return execControlledAction(ctx, r, act, input.DryRun, true, "reminder", "show", "--json")
			case "clear":
				return execControlledAction(ctx, r, act, input.DryRun, true, "reminder", "clear")
			case "set":
				if input.Minutes < 1 || input.Minutes > 10080 {
					return ActionOutput{}, fmt.Errorf("[%s] minutes must be between 1 and 10080", handler.ErrInvalidParam)
				}
				args := []string{"reminder", strconv.Itoa(input.Minutes)}
				if msg := strings.TrimSpace(input.Message); msg != "" {
					if err := ValidatePositionalArg("message", msg); err != nil {
						return ActionOutput{}, err
					}
					args = append(args, msg)
				}
				return execControlledAction(ctx, r, act, input.DryRun, true, args...)
			default:
				return ActionOutput{}, fmt.Errorf("[%s] reminder action must be show, set, or clear", handler.ErrInvalidParam)
			}
		},
	)
	reminderTool.Category = "desktop"
	reminderTool.IsWrite = true
	reminderTool.Complexity = registry.ComplexityModerate

	return []registry.ToolDefinition{
		statusTool,
		commandsTool,
		hwTool,
		debugTool,
		validateTool,
		themeTool,
		fontTool,
		pluginTool,
		barTool,
		hookTool,
		restartTool,
		toggleTool,
		reminderTool,
	}
}
