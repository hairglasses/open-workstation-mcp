package omarchy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/handler"
)

const (
	FlavorHyprland        = "hyprland"
	FlavorNiri            = "niri"
	FlavorHeadless        = "headless"
	StatusSkippedHeadless = "SKIPPED_HEADLESS"
	StatusSkippedNiri     = "SKIPPED_NIRI"
	ClipboardPolicy       = "clipse"
	GhosttyReloadSignal   = "SIGUSR2"
	ErrPolicyDenied       = "POLICY_DENIED"
	DefaultTimeout        = 15 * time.Second
	MaxOutputBytes        = 48 * 1024
)

type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

type ExecFunc func(ctx context.Context, env []string, binary string, args ...string) (ExecResult, error)

type Runner struct {
	Timeout    time.Duration
	Env        []string
	ConfigHome string
	HomeDir    string
	ExecFunc   ExecFunc
}

var (
	runnerMu     sync.RWMutex
	activeRunner = DefaultRunner()
	allowedBins  = map[string]bool{
		"omarchy": true,
		"hyprctl": true,
	}
	safeIdentRe       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	emailScrubRe      = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	forbiddenClipBins = []string{"wl-copy", "wl-paste"}
	forbiddenThemes   = []string{"cyberpunk", "snazzy-black", "snazzy black", "snazzy_black"}
	forbiddenShaders  = []string{"cyberpunk-crt.glsl", "snazzy-phosphor-grid.glsl"}
	forbiddenHexes    = []string{"#000000", "#ff00ff", "#00ffff", "#ff0099", "#39ff14", "#ff6ac1", "#57c7ff", "#5af78e"}
)

func DefaultRunner() *Runner {
	return &Runner{Timeout: DefaultTimeout}
}

func GetRunner() *Runner {
	runnerMu.RLock()
	defer runnerMu.RUnlock()
	if activeRunner == nil {
		return DefaultRunner()
	}
	return activeRunner
}

func SetRunner(r *Runner) {
	runnerMu.Lock()
	defer runnerMu.Unlock()
	if r == nil {
		activeRunner = DefaultRunner()
		return
	}
	activeRunner = r
}

func ResetRunner() {
	SetRunner(DefaultRunner())
}

func (r *Runner) effectiveEnv() []string {
	if r != nil && r.Env != nil {
		return r.Env
	}
	return os.Environ()
}

func (r *Runner) effectiveHome() string {
	if r != nil && r.HomeDir != "" {
		return filepath.Clean(r.HomeDir)
	}
	for _, kv := range r.effectiveEnv() {
		if after, ok := strings.CutPrefix(kv, "HOME="); ok && strings.TrimSpace(after) != "" {
			return filepath.Clean(strings.TrimSpace(after))
		}
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Clean(h)
}

func (r *Runner) effectiveConfigHome() string {
	if r != nil && r.ConfigHome != "" {
		return filepath.Clean(r.ConfigHome)
	}
	for _, kv := range r.effectiveEnv() {
		if after, ok := strings.CutPrefix(kv, "XDG_CONFIG_HOME="); ok && strings.TrimSpace(after) != "" {
			return filepath.Clean(strings.TrimSpace(after))
		}
	}
	home := r.effectiveHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config")
}

func (r *Runner) DetectFlavor() (string, string) {
	return DetectFlavorFromEnv(r.effectiveEnv())
}

func DetectFlavorFromEnv(env []string) (string, string) {
	vals := make(map[string]string, len(env))
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			vals[k] = strings.TrimSpace(v)
		}
	}
	switch strings.ToLower(vals["OMARCHY_FLAVOR_OVERRIDE"]) {
	case FlavorHeadless:
		return FlavorHeadless, StatusSkippedHeadless
	case FlavorNiri:
		return FlavorNiri, StatusSkippedNiri
	case FlavorHyprland:
		return FlavorHyprland, ""
	}

	hyprSig := vals["HYPRLAND_INSTANCE_SIGNATURE"]
	niriSock := vals["NIRI_SOCKET"]
	waylandDisp := vals["WAYLAND_DISPLAY"]
	xdgDesktop := strings.ToLower(vals["XDG_CURRENT_DESKTOP"])
	desktopSession := strings.ToLower(vals["DESKTOP_SESSION"])

	if niriSock != "" || (hyprSig == "" && (strings.Contains(xdgDesktop, "niri") || strings.Contains(desktopSession, "niri"))) {
		return FlavorNiri, StatusSkippedNiri
	}
	if hyprSig != "" || waylandDisp != "" {
		return FlavorHyprland, ""
	}
	return FlavorHeadless, StatusSkippedHeadless
}

func SafeExecEnv(raw []string) []string {
	if raw == nil {
		raw = os.Environ()
	}
	allowedPrefixes := []string{
		"PATH=",
		"HOME=",
		"USER=",
		"LANG=",
		"LC_",
		"TERM=",
		"XDG_RUNTIME_DIR=",
		"XDG_CONFIG_HOME=",
		"XDG_STATE_HOME=",
		"XDG_DATA_HOME=",
		"XDG_CURRENT_DESKTOP=",
		"DESKTOP_SESSION=",
		"WAYLAND_DISPLAY=",
		"HYPRLAND_INSTANCE_SIGNATURE=",
		"NIRI_SOCKET=",
		"OMARCHY_PATH=",
		"OMARCHY_FLAVOR_OVERRIDE=",
	}
	out := make([]string, 0, len(allowedPrefixes))
	for _, entry := range raw {
		for _, pfx := range allowedPrefixes {
			if strings.HasPrefix(entry, pfx) {
				out = append(out, entry)
				break
			}
		}
	}
	return out
}

func ContainsForbiddenClipboard(s string) bool {
	lower := strings.ToLower(s)
	for _, bin := range forbiddenClipBins {
		if strings.Contains(lower, bin) {
			return true
		}
	}
	return false
}

func ContainsForbiddenGhosttySignal(s string) bool {
	upper := strings.ToUpper(strings.TrimSpace(s))
	if upper == "USR1" || upper == "SIGUSR1" || upper == "-USR1" || upper == "-SIGUSR1" {
		return true
	}
	return strings.Contains(upper, "SIGUSR1")
}

func ContainsForbiddenThemeOrColor(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, hex := range forbiddenHexes {
		if strings.Contains(lower, hex) {
			return hex
		}
	}
	for _, shader := range forbiddenShaders {
		if strings.Contains(lower, shader) {
			return shader
		}
	}
	norm := strings.NewReplacer("_", "-", " ", "-").Replace(lower)
	for _, bad := range forbiddenThemes {
		badNorm := strings.NewReplacer("_", "-", " ", "-").Replace(bad)
		if norm == badNorm || strings.Contains(norm, "cyberpunk") || norm == "snazzy-black" {
			return bad
		}
	}
	return ""
}

func IsProtectedOrCredentialPath(p string) bool {
	if p == "" {
		return false
	}
	slashed := filepath.ToSlash(p)
	lower := strings.ToLower(slashed)
	blockedSubstrings := []string{
		"/.ssh/",
		"/.gnupg/",
		"/.aws/",
		"/.kube/",
		"/.docker/",
		"/.netrc",
		"/.claude",
		"/.gemini",
		"/.codex",
		"/.cline",
		".credentials",
		"rclone.conf",
		"/etc/credstore",
		"/run/secrets",
		"/run/hg-secrets",
		"/var/lib/hg-secrets",
		"provider_keys.db",
		"/.config/op/",
		"/.config/gh/hosts.yml",
		"/.config/gcloud/",
		"/.config/restic/",
	}
	for _, sub := range blockedSubstrings {
		if strings.Contains(lower, sub) || strings.HasSuffix(lower, strings.TrimSuffix(sub, "/")) {
			return true
		}
	}
	for _, seg := range strings.Split(lower, "/") {
		if seg == ".env" || (strings.HasPrefix(seg, ".env.") && seg != ".env.example" && seg != ".env.sample" && seg != ".env.template") {
			return true
		}
	}
	return false
}

func ValidateArg(arg string) error {
	if len(arg) > 256 {
		return fmt.Errorf("[%s] argument exceeds max length 256", handler.ErrInvalidParam)
	}
	for i := 0; i < len(arg); i++ {
		c := arg[i]
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("[%s] argument contains control characters", handler.ErrInvalidParam)
		}
	}
	if strings.ContainsAny(arg, ";|&$`><\\") {
		return fmt.Errorf("[%s] argument contains disallowed shell metacharacters", handler.ErrInvalidParam)
	}
	if ContainsForbiddenClipboard(arg) {
		return fmt.Errorf("[%s] clipboard policy allows %s only; wl-copy and wl-paste are blocked", ErrPolicyDenied, ClipboardPolicy)
	}
	if ContainsForbiddenGhosttySignal(arg) {
		return fmt.Errorf("[%s] Ghostty reload uses %s or omarchy restart terminal; SIGUSR1 is blocked", ErrPolicyDenied, GhosttyReloadSignal)
	}
	if IsProtectedOrCredentialPath(arg) {
		return fmt.Errorf("[%s] access to credential or protected path is blocked", ErrPolicyDenied)
	}
	return nil
}

func ValidatePositionalArg(name, val string) error {
	val = strings.TrimSpace(val)
	if val == "" {
		return fmt.Errorf("[%s] %s is required", handler.ErrInvalidParam, name)
	}
	if strings.HasPrefix(val, "-") {
		return fmt.Errorf("[%s] %s must not start with '-'", handler.ErrInvalidParam, name)
	}
	for _, seg := range strings.Split(filepath.ToSlash(val), "/") {
		if seg == ".." {
			return fmt.Errorf("[%s] %s must not contain '..'", handler.ErrInvalidParam, name)
		}
	}
	return ValidateArg(val)
}

func ValidateIdentifier(name, val string) error {
	val = strings.TrimSpace(val)
	if err := ValidatePositionalArg(name, val); err != nil {
		return err
	}
	if !safeIdentRe.MatchString(val) || strings.Contains(val, "..") {
		return fmt.Errorf("[%s] %s %q must match [A-Za-z0-9._-]+ without '..'", handler.ErrInvalidParam, name, val)
	}
	return nil
}

func ValidateHookName(name, val string) error {
	val = strings.TrimSpace(val)
	if val == "" || val == "." || val == ".." || strings.Contains(val, "/") {
		return fmt.Errorf("[%s] invalid %s %q", handler.ErrInvalidParam, name, val)
	}
	if strings.HasPrefix(val, "-") {
		return fmt.Errorf("[%s] %s must not start with '-'", handler.ErrInvalidParam, name)
	}
	return ValidateArg(val)
}

func ValidateThemeName(val string) error {
	val = strings.TrimSpace(val)
	if err := ValidatePositionalArg("name", val); err != nil {
		return err
	}
	if strings.Contains(val, "/") {
		return fmt.Errorf("[%s] theme name %q must not contain '/'", handler.ErrInvalidParam, val)
	}
	if bad := ContainsForbiddenThemeOrColor(val); bad != "" {
		return fmt.Errorf("[%s] theme or color %q violates dark non-cyberpunk palette policy", ErrPolicyDenied, bad)
	}
	return nil
}

func ValidateSafeUserFile(path string, allowReadOnly bool) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("[%s] path is required", handler.ErrInvalidParam)
	}
	if err := ValidateArg(path); err != nil {
		return "", err
	}
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if seg == ".." {
			return "", fmt.Errorf("[%s] path must not contain '..'", handler.ErrInvalidParam)
		}
	}
	cleanPath := filepath.Clean(path)
	if IsProtectedOrCredentialPath(cleanPath) {
		return "", fmt.Errorf("[%s] credential or protected path is blocked", ErrPolicyDenied)
	}
	if !allowReadOnly && (cleanPath == "/usr/share/omarchy" || strings.HasPrefix(cleanPath, "/usr/share/omarchy/")) {
		return "", fmt.Errorf("[%s] /usr/share/omarchy is read-only; customize under ~/.config", ErrPolicyDenied)
	}
	fi, err := os.Lstat(cleanPath)
	if err != nil {
		return "", fmt.Errorf("[%s] stat path: %w", handler.ErrNotFound, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		resolved, rerr := filepath.EvalSymlinks(cleanPath)
		if rerr != nil {
			return "", fmt.Errorf("[%s] broken symlink", handler.ErrInvalidParam)
		}
		if IsProtectedOrCredentialPath(resolved) {
			return "", fmt.Errorf("[%s] symlink target resolves to protected path", ErrPolicyDenied)
		}
		if !allowReadOnly && (resolved == "/usr/share/omarchy" || strings.HasPrefix(resolved, "/usr/share/omarchy/")) {
			return "", fmt.Errorf("[%s] symlink target resolves to read-only /usr/share/omarchy", ErrPolicyDenied)
		}
	}
	return cleanPath, nil
}

func SanitizeOutput(text, homeDir string, maxBytes int) (string, bool) {
	if homeDir != "" && homeDir != "/" {
		text = strings.ReplaceAll(text, homeDir, "~")
	}
	text = emailScrubRe.ReplaceAllString(text, "[redacted-address]")
	if maxBytes <= 0 {
		maxBytes = MaxOutputBytes
	}
	truncated := false
	if len(text) > maxBytes {
		text = text[:maxBytes]
		truncated = true
	}
	return strings.TrimSpace(text), truncated
}

func (r *Runner) Run(ctx context.Context, binary string, args ...string) (ExecResult, error) {
	if !allowedBins[binary] {
		return ExecResult{}, fmt.Errorf("[%s] disallowed binary %q", ErrPolicyDenied, binary)
	}
	for _, a := range args {
		if err := ValidateArg(a); err != nil {
			return ExecResult{}, err
		}
	}
	if binary == "omarchy" && len(args) > 0 && args[0] == "debug" {
		if !slices.Contains(args[1:], "--no-sudo") || !slices.Contains(args[1:], "--print") {
			return ExecResult{}, fmt.Errorf("[%s] omarchy debug requires --no-sudo and --print", ErrPolicyDenied)
		}
		for _, flagArg := range args[1:] {
			if flagArg != "--no-sudo" && flagArg != "--print" {
				return ExecResult{}, fmt.Errorf("[%s] unsupported omarchy debug argument %q", ErrPolicyDenied, flagArg)
			}
		}
	}

	timeout := DefaultTimeout
	if r != nil && r.Timeout > 0 {
		timeout = r.Timeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	safeEnv := SafeExecEnv(r.effectiveEnv())
	homeDir := r.effectiveHome()

	if r != nil && r.ExecFunc != nil {
		res, err := r.ExecFunc(execCtx, safeEnv, binary, args...)
		res.Stdout, _ = SanitizeOutput(res.Stdout, homeDir, MaxOutputBytes)
		res.Stderr, _ = SanitizeOutput(res.Stderr, homeDir, MaxOutputBytes)
		return res, err
	}

	cmd := exec.CommandContext(execCtx, binary, args...)
	cmd.Stdin = nil
	cmd.Env = safeEnv
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return ExecResult{ExitCode: 124, Stderr: "command timed out"}, fmt.Errorf("[%s] %s timed out after %s", handler.ErrTimeout, binary, timeout)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	cleanOut, _ := SanitizeOutput(stdout.String(), homeDir, MaxOutputBytes)
	cleanErr, _ := SanitizeOutput(stderr.String(), homeDir, MaxOutputBytes)
	return ExecResult{
		Stdout:   cleanOut,
		Stderr:   cleanErr,
		ExitCode: exitCode,
	}, err
}
