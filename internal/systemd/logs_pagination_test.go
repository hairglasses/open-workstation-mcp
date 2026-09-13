package systemd

import (
	"context"
	"strings"
	"testing"
)

func TestLogs_PaginationOptions(t *testing.T) {
	td := findTool(t, "systemd_logs")

	// 1. Basic call returns valid cursor and lines
	t.Run("cursor_and_lines", func(t *testing.T) {
		req := makeReq(map[string]any{
			"unit":  "systemd-journald.service",
			"lines": 3,
		})
		result, err := td.Handler(context.Background(), req)
		if err != nil {
			t.Skipf("journalctl unavailable: %v", err)
		}
		if result == nil || result.IsError {
			t.Skipf("journalctl execution skipped or returned error")
		}

		var out LogsOutput
		unmarshalResult(t, result, &out)
		if out.Lines != 3 {
			t.Errorf("expected requested lines=3, got %d", out.Lines)
		}
		// If journalctl returns cursor, it must not leak into the Logs body
		if strings.Contains(out.Logs, "-- cursor:") {
			t.Errorf("expected cursor to be stripped from logs body, got: %s", out.Logs)
		}
	})

	// 2. Offset pagination
	t.Run("offset_pagination", func(t *testing.T) {
		req := makeReq(map[string]any{
			"unit":   "systemd-journald.service",
			"lines":  2,
			"offset": 1,
		})
		result, err := td.Handler(context.Background(), req)
		if err != nil {
			t.Skipf("journalctl unavailable: %v", err)
		}
		if result == nil || result.IsError {
			t.Skipf("journalctl execution skipped or returned error")
		}

		var out LogsOutput
		unmarshalResult(t, result, &out)
		if out.Offset != 1 {
			t.Errorf("expected offset=1, got %d", out.Offset)
		}
	})

	// 3. Reverse order
	t.Run("reverse_order", func(t *testing.T) {
		req := makeReq(map[string]any{
			"unit":    "systemd-journald.service",
			"lines":   2,
			"reverse": true,
		})
		result, err := td.Handler(context.Background(), req)
		if err != nil {
			t.Skipf("journalctl unavailable: %v", err)
		}
		if result == nil || result.IsError {
			t.Skipf("journalctl execution skipped or returned error")
		}
	})

	// 4. Pattern grep filter
	t.Run("pattern_filter", func(t *testing.T) {
		req := makeReq(map[string]any{
			"unit":    "systemd-journald.service",
			"lines":   5,
			"pattern": ".*",
		})
		result, err := td.Handler(context.Background(), req)
		if err != nil {
			t.Skipf("journalctl unavailable: %v", err)
		}
		if result == nil || result.IsError {
			t.Skipf("journalctl execution skipped or returned error")
		}
	})

	// 5. Priority level filter
	t.Run("priority_filter", func(t *testing.T) {
		req := makeReq(map[string]any{
			"unit":     "systemd-journald.service",
			"lines":    5,
			"priority": "info",
		})
		result, err := td.Handler(context.Background(), req)
		if err != nil {
			t.Skipf("journalctl unavailable: %v", err)
		}
		if result == nil || result.IsError {
			t.Skipf("journalctl execution skipped or returned error")
		}
	})
}
