package omarchy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
)

type ConfigIssue struct {
	Component string `json:"component" jsonschema:"description=Configuration surface: ghostty\\, hooks\\, or hyprland"`
	Code      string `json:"code" jsonschema:"description=Machine-readable check code"`
	Severity  string `json:"severity" jsonschema:"description=Issue severity: error or warning"`
	Path      string `json:"path,omitempty" jsonschema:"description=Sanitized relative or home-masked file path"`
	Detail    string `json:"detail" jsonschema:"description=Actionable validation finding"`
}

type GhosttyConfigSummary struct {
	ConfigPresent        bool     `json:"config_present"`
	Includes             []string `json:"includes"`
	FleetCockpitIncluded bool     `json:"fleet_cockpit_included"`
	FontSizeCurrentLast  bool     `json:"font_size_current_last"`
	ReloadSignal         string   `json:"reload_signal"`
}

type HooksSummary struct {
	HooksPresent    bool     `json:"hooks_present"`
	CheckedFiles    int      `json:"checked_files"`
	NonExecutable   []string `json:"non_executable"`
	BackupArtifacts []string `json:"backup_artifacts"`
}

type HyprConfigSummary struct {
	ConfigPresent       bool     `json:"config_present"`
	Flavor              string   `json:"flavor"`
	SkipStatus          string   `json:"skip_status,omitempty"`
	ConfigErrorsChecked bool     `json:"config_errors_checked"`
	ConfigErrors        []string `json:"config_errors"`
	StaleBackups        []string `json:"stale_backups"`
}

type ConfigValidationReport struct {
	Valid           bool                 `json:"valid"`
	Flavor          string               `json:"flavor"`
	SkipStatus      string               `json:"skip_status,omitempty"`
	ClipboardPolicy string               `json:"clipboard_policy"`
	Ghostty         GhosttyConfigSummary `json:"ghostty"`
	Hooks           HooksSummary         `json:"hooks"`
	Hyprland        HyprConfigSummary    `json:"hyprland"`
	Issues          []ConfigIssue        `json:"issues"`
}

func maskHome(path, homeDir string) string {
	if homeDir != "" && homeDir != "/" && strings.HasPrefix(path, homeDir) {
		rel := strings.TrimPrefix(path, homeDir)
		if rel == "" {
			return "~"
		}
		if strings.HasPrefix(rel, "/") {
			return "~" + rel
		}
	}
	return path
}

func readBoundedRegularFile(path string, maxBytes int64) (string, error) {
	if IsProtectedOrCredentialPath(path) {
		return "", fmt.Errorf("blocked protected path")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file")
	}
	if fi.Size() > maxBytes {
		return "", fmt.Errorf("file exceeds max size %d", maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func normalizeIncludeTarget(raw string) string {
	v := strings.TrimSpace(raw)
	v = strings.Trim(v, `"'`)
	v = strings.TrimPrefix(v, "?")
	v = strings.TrimSpace(v)
	return strings.Trim(v, `"'`)
}

func validateGhosttyDir(configHome, homeDir string) (GhosttyConfigSummary, []ConfigIssue) {
	summary := GhosttyConfigSummary{
		Includes:     []string{},
		ReloadSignal: GhosttyReloadSignal,
	}
	var issues []ConfigIssue
	ghosttyDir := filepath.Join(configHome, "ghostty")
	mainCfg := filepath.Join(ghosttyDir, "config")

	content, err := readBoundedRegularFile(mainCfg, 256*1024)
	if err == nil {
		summary.ConfigPresent = true
		fleetIdx := -1
		fontSizeIdx := -1
		for line := range strings.SplitSeq(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			k, v, ok := strings.Cut(trimmed, "=")
			if !ok {
				continue
			}
			key := strings.TrimSpace(k)
			val := strings.TrimSpace(v)
			if key == "config-file" {
				inc := maskHome(normalizeIncludeTarget(val), homeDir)
				idx := len(summary.Includes)
				summary.Includes = append(summary.Includes, inc)
				if strings.HasSuffix(inc, "/fleet-cockpit.conf") || inc == "fleet-cockpit.conf" {
					summary.FleetCockpitIncluded = true
					fleetIdx = idx
				}
				if strings.HasSuffix(inc, "/font-size-current.conf") || inc == "font-size-current.conf" {
					fontSizeIdx = idx
				}
			}
		}
		if len(summary.Includes) > 0 {
			last := summary.Includes[len(summary.Includes)-1]
			if strings.HasSuffix(last, "/font-size-current.conf") || last == "font-size-current.conf" {
				if !summary.FleetCockpitIncluded || (fontSizeIdx > fleetIdx) {
					summary.FontSizeCurrentLast = true
				}
			}
		}
		fontSizeFile := filepath.Join(ghosttyDir, "font-size-current.conf")
		_, fontFileErr := os.Stat(fontSizeFile)
		if (summary.FleetCockpitIncluded || fontFileErr == nil) && !summary.FontSizeCurrentLast {
			issues = append(issues, ConfigIssue{
				Component: "ghostty",
				Code:      "GHOSTTY_INCLUDE_ORDER",
				Severity:  "error",
				Path:      maskHome(mainCfg, homeDir),
				Detail:    "font-size-current.conf must be the final config-file include after fleet-cockpit.conf",
			})
		}
	}

	entries, dirErr := os.ReadDir(ghosttyDir)
	if dirErr == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				continue
			}
			if name != "config" && !strings.HasSuffix(name, ".conf") && !strings.HasSuffix(name, ".css") {
				continue
			}
			p := filepath.Join(ghosttyDir, name)
			text, rerr := readBoundedRegularFile(p, 256*1024)
			if rerr != nil {
				continue
			}
			issues = append(issues, scanGhosttyContent(text, maskHome(p, homeDir))...)
		}
	}

	shaderDir := filepath.Join(ghosttyDir, "shaders")
	if shaderEntries, serr := os.ReadDir(shaderDir); serr == nil {
		for _, se := range shaderEntries {
			lowerName := strings.ToLower(se.Name())
			for _, badShader := range forbiddenShaders {
				if lowerName == badShader {
					issues = append(issues, ConfigIssue{
						Component: "ghostty",
						Code:      "FORBIDDEN_CYBERPUNK_SHADER",
						Severity:  "error",
						Path:      maskHome(filepath.Join(shaderDir, se.Name()), homeDir),
						Detail:    fmt.Sprintf("forbidden cyberpunk shader %s present in ghostty shaders directory", se.Name()),
					})
				}
			}
		}
	}

	return summary, issues
}

func scanGhosttyContent(content, maskedPath string) []ConfigIssue {
	var issues []ConfigIssue
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "#000000") {
			issues = append(issues, ConfigIssue{
				Component: "ghostty",
				Code:      "FORBIDDEN_PURE_BLACK",
				Severity:  "error",
				Path:      maskedPath,
				Detail:    "pure black #000000 is disallowed by dark palette contrast policy",
			})
		}
		for _, badHex := range forbiddenHexes[1:] {
			if strings.Contains(lower, badHex) {
				issues = append(issues, ConfigIssue{
					Component: "ghostty",
					Code:      "FORBIDDEN_NEON_HEX",
					Severity:  "error",
					Path:      maskedPath,
					Detail:    fmt.Sprintf("forbidden cyberpunk/snazzy hex %s referenced in Ghostty configuration", badHex),
				})
			}
		}
		for _, badShader := range forbiddenShaders {
			if strings.Contains(lower, badShader) {
				issues = append(issues, ConfigIssue{
					Component: "ghostty",
					Code:      "FORBIDDEN_CYBERPUNK_SHADER",
					Severity:  "error",
					Path:      maskedPath,
					Detail:    fmt.Sprintf("forbidden shader %s referenced in Ghostty configuration", badShader),
				})
			}
		}
		if ContainsForbiddenClipboard(trimmed) {
			issues = append(issues, ConfigIssue{
				Component: "ghostty",
				Code:      "FORBIDDEN_WL_CLIPBOARD",
				Severity:  "error",
				Path:      maskedPath,
				Detail:    "wl-copy and wl-paste are disallowed; use clipse",
			})
		}
		if ContainsForbiddenGhosttySignal(trimmed) {
			issues = append(issues, ConfigIssue{
				Component: "ghostty",
				Code:      "FORBIDDEN_GHOSTTY_SIGUSR1",
				Severity:  "error",
				Path:      maskedPath,
				Detail:    "SIGUSR1 terminates Ghostty; use SIGUSR2 or omarchy restart terminal",
			})
		}
	}
	return issues
}

func validateHooksDir(configHome, homeDir string) (HooksSummary, []ConfigIssue) {
	summary := HooksSummary{
		NonExecutable:   []string{},
		BackupArtifacts: []string{},
	}
	var issues []ConfigIssue
	hooksRoot := filepath.Join(configHome, "omarchy", "hooks")
	rootEntries, err := os.ReadDir(hooksRoot)
	if err != nil {
		return summary, issues
	}
	summary.HooksPresent = true
	for _, d := range rootEntries {
		if !d.IsDir() || !strings.HasSuffix(d.Name(), ".d") {
			continue
		}
		subDir := filepath.Join(hooksRoot, d.Name())
		subEntries, serr := os.ReadDir(subDir)
		if serr != nil {
			continue
		}
		for _, f := range subEntries {
			if f.IsDir() {
				continue
			}
			fname := f.Name()
			if strings.HasSuffix(fname, ".sample") {
				continue
			}
			fullPath := filepath.Join(subDir, fname)
			masked := maskHome(fullPath, homeDir)
			summary.CheckedFiles++

			if strings.Contains(fname, ".bak") || strings.HasSuffix(fname, "~") {
				summary.BackupArtifacts = append(summary.BackupArtifacts, masked)
				issues = append(issues, ConfigIssue{
					Component: "hooks",
					Code:      "HOOK_BACKUP_ARTIFACT",
					Severity:  "error",
					Path:      masked,
					Detail:    "backup file inside hooks .d directory will be executed by omarchy hook runner",
				})
				continue
			}

			info, ierr := os.Stat(fullPath)
			if ierr != nil {
				continue
			}
			if info.Mode().IsRegular() && info.Mode().Perm()&0o111 == 0 {
				summary.NonExecutable = append(summary.NonExecutable, masked)
				issues = append(issues, ConfigIssue{
					Component: "hooks",
					Code:      "HOOK_NOT_EXECUTABLE",
					Severity:  "error",
					Path:      masked,
					Detail:    "hook script in .d directory lacks executable permission (expected 0755)",
				})
			}

			if text, rerr := readBoundedRegularFile(fullPath, 64*1024); rerr == nil {
				if ContainsForbiddenClipboard(text) {
					issues = append(issues, ConfigIssue{
						Component: "hooks",
						Code:      "FORBIDDEN_WL_CLIPBOARD",
						Severity:  "error",
						Path:      masked,
						Detail:    "hook script invokes wl-copy or wl-paste instead of clipse",
					})
				}
				if strings.Contains(strings.ToLower(text), "ghostty") && ContainsForbiddenGhosttySignal(text) {
					issues = append(issues, ConfigIssue{
						Component: "hooks",
						Code:      "FORBIDDEN_GHOSTTY_SIGUSR1",
						Severity:  "error",
						Path:      masked,
						Detail:    "hook script sends SIGUSR1 to Ghostty; use SIGUSR2 or omarchy restart terminal",
					})
				}
			}
		}
	}
	sort.Strings(summary.NonExecutable)
	sort.Strings(summary.BackupArtifacts)
	return summary, issues
}

func validateHyprDir(ctx context.Context, r *Runner, configHome, homeDir, flavor, skipStatus string) (HyprConfigSummary, []ConfigIssue) {
	summary := HyprConfigSummary{
		Flavor:       flavor,
		SkipStatus:   skipStatus,
		ConfigErrors: []string{},
		StaleBackups: []string{},
	}
	var issues []ConfigIssue
	hyprDir := filepath.Join(configHome, "hypr")
	entries, err := os.ReadDir(hyprDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if name == "hyprland.lua" || name == "hyprland.conf" {
				summary.ConfigPresent = true
			}
			if strings.Contains(name, ".bak") || (strings.HasPrefix(name, "monitors.lua.") && !strings.HasSuffix(name, ".lua")) {
				masked := maskHome(filepath.Join(hyprDir, name), homeDir)
				summary.StaleBackups = append(summary.StaleBackups, masked)
				issues = append(issues, ConfigIssue{
					Component: "hyprland",
					Code:      "HYPR_STALE_BACKUP",
					Severity:  "warning",
					Path:      masked,
					Detail:    "stale backup or temp artifact present in hypr config directory",
				})
			}
		}
	}
	sort.Strings(summary.StaleBackups)

	if flavor == FlavorHyprland && skipStatus == "" {
		res, runErr := r.Run(ctx, "hyprctl", "configerrors")
		if runErr == nil {
			summary.ConfigErrorsChecked = true
			for line := range strings.SplitSeq(res.Stdout, "\n") {
				t := strings.TrimSpace(line)
				if t != "" {
					summary.ConfigErrors = append(summary.ConfigErrors, t)
					issues = append(issues, ConfigIssue{
						Component: "hyprland",
						Code:      "HYPRCTL_CONFIG_ERROR",
						Severity:  "error",
						Detail:    t,
					})
				}
			}
		}
	}

	return summary, issues
}

func ValidateConfigs(ctx context.Context, r *Runner, component string) (ConfigValidationReport, error) {
	if r == nil {
		r = GetRunner()
	}
	comp := strings.ToLower(strings.TrimSpace(component))
	if comp == "" {
		comp = "all"
	}
	switch comp {
	case "all", "ghostty", "hooks", "omarchy", "hypr", "hyprland":
	default:
		return ConfigValidationReport{}, fmt.Errorf("[%s] component must be all, ghostty, hooks, omarchy, or hyprland", handler.ErrInvalidParam)
	}

	flavor, skipStatus := r.DetectFlavor()
	configHome := r.effectiveConfigHome()
	homeDir := r.effectiveHome()

	report := ConfigValidationReport{
		Valid:           true,
		Flavor:          flavor,
		SkipStatus:      skipStatus,
		ClipboardPolicy: ClipboardPolicy,
		Ghostty: GhosttyConfigSummary{
			Includes:     []string{},
			ReloadSignal: GhosttyReloadSignal,
		},
		Hooks: HooksSummary{
			NonExecutable:   []string{},
			BackupArtifacts: []string{},
		},
		Hyprland: HyprConfigSummary{
			Flavor:       flavor,
			SkipStatus:   skipStatus,
			ConfigErrors: []string{},
			StaleBackups: []string{},
		},
		Issues: []ConfigIssue{},
	}

	if comp == "all" || comp == "ghostty" {
		gSum, gIssues := validateGhosttyDir(configHome, homeDir)
		report.Ghostty = gSum
		report.Issues = append(report.Issues, gIssues...)
	}
	if comp == "all" || comp == "hooks" || comp == "omarchy" {
		hSum, hIssues := validateHooksDir(configHome, homeDir)
		report.Hooks = hSum
		report.Issues = append(report.Issues, hIssues...)
	}
	if comp == "all" || comp == "hypr" || comp == "hyprland" {
		hySum, hyIssues := validateHyprDir(ctx, r, configHome, homeDir, flavor, skipStatus)
		report.Hyprland = hySum
		report.Issues = append(report.Issues, hyIssues...)
	}

	for _, iss := range report.Issues {
		if iss.Severity == "error" {
			report.Valid = false
			break
		}
	}

	return report, nil
}
