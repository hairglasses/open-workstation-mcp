package systemd

import (
	"context"
	"strings"
	"testing"

	"github.com/hairglasses-studio/mcpkit/handler"
)

func TestValidateUnitName(t *testing.T) {
	validUnits := []string{
		"nginx.service",
		"user@1000.service",
		"my-app_worker.slice",
		"custom.target",
		"foo:bar.service",
		"systemd-journald.socket",
		"docker.timer",
		"dev-sda1.device",
	}

	for _, u := range validUnits {
		if err := validateUnitName(u); err != nil {
			t.Errorf("expected unit %q to be valid, got error: %v", u, err)
		}
	}

	invalidUnits := []struct {
		name string
		unit string
	}{
		{"empty", ""},
		{"whitespace_only", "   "},
		{"path_traversal_relative", "../evil.service"},
		{"path_traversal_nested", "foo/../bar.service"},
		{"absolute_path", "/etc/systemd/system/evil.service"},
		{"semicolon_command_injection", "nginx.service; cat /etc/passwd"},
		{"ampersand_background", "nginx.service &"},
		{"pipe", "nginx.service | cat"},
		{"subshell_dollar", "nginx$(reboot).service"},
		{"subshell_backticks", "nginx`reboot`.service"},
		{"redirect_out", "nginx.service > /tmp/pwn"},
		{"redirect_in", "nginx.service < /dev/zero"},
		{"glob_star", "nginx*.service"},
		{"glob_question", "nginx?.service"},
		{"space_injection", "nginx service"},
		{"too_long", strings.Repeat("a", 257) + ".service"},
	}

	for _, tc := range invalidUnits {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUnitName(tc.unit)
			if err == nil {
				t.Fatalf("expected unit %q to be rejected, but validation passed", tc.unit)
			}
			if !strings.Contains(err.Error(), string(handler.ErrInvalidParam)) {
				t.Errorf("expected error to contain %q, got %q", handler.ErrInvalidParam, err.Error())
			}
		})
	}
}

func TestValidatePriority(t *testing.T) {
	valid := []string{"", "err", "warning", "info", "notice", "debug", "crit", "alert", "emerg", "3", "0..3", "err..alert"}
	for _, p := range valid {
		if err := validatePriority(p); err != nil {
			t.Errorf("expected priority %q to be valid, got: %v", p, err)
		}
	}

	invalid := []string{
		"err; rm -rf /",
		"warning | cat",
		"info > /tmp/out",
		"debug$(reboot)",
		strings.Repeat("a", 33),
	}
	for _, p := range invalid {
		if err := validatePriority(p); err == nil {
			t.Errorf("expected priority %q to be invalid, but validation passed", p)
		}
	}
}

func TestUnitValidation_HandlersRejectTraversal(t *testing.T) {
	dangerousUnit := "../../etc/shadow; reboot"

	// 1. systemd_status
	t.Run("status_rejects_malicious_unit", func(t *testing.T) {
		td := findTool(t, "systemd_status")
		req := makeReq(map[string]any{"unit": dangerousUnit})
		res, err := td.Handler(context.Background(), req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected status handler to reject malicious unit name")
		}
	})

	// 2. systemd_logs
	t.Run("logs_rejects_malicious_unit", func(t *testing.T) {
		td := findTool(t, "systemd_logs")
		req := makeReq(map[string]any{"unit": dangerousUnit})
		res, err := td.Handler(context.Background(), req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected logs handler to reject malicious unit name")
		}
	})

	// 3. systemd_start
	t.Run("start_rejects_malicious_unit", func(t *testing.T) {
		td := findTool(t, "systemd_start")
		req := makeReq(map[string]any{"unit": dangerousUnit})
		res, err := td.Handler(context.Background(), req)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatal("expected start handler to reject malicious unit name")
		}
	})
}
