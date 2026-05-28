package workstationmcp

import (
	"context"
	"fmt"
	"strings"
)

func SampleGateway() *Gateway {
	g := NewGateway("open-workstation-mcp", "0.1.0", "Public-safe workstation automation pattern sample", DryRunPolicy(), ResponseBudget(8))
	must(g.Register(Tool{
		Name:        "workstation_status",
		Description: "Return a synthetic workstation readiness snapshot.",
		InputSchema: schema([]string{"profile"}, map[string]any{"profile": map[string]string{"type": "string"}}),
		ReadOnly:    true,
		Risk:        "low",
	}, statusHandler))
	must(g.Register(Tool{
		Name:        "window_focus_plan",
		Description: "Build a dry-run plan to focus a synthetic app window.",
		InputSchema: schema([]string{"app", "intent"}, map[string]any{"app": map[string]string{"type": "string"}, "intent": map[string]string{"type": "string"}}),
		ReadOnly:    true,
		Risk:        "medium",
	}, focusPlanHandler))
	must(g.Register(Tool{
		Name:        "config_drift_report",
		Description: "Summarize a synthetic workstation config drift check.",
		InputSchema: schema([]string{"component"}, map[string]any{"component": map[string]string{"type": "string"}}),
		ReadOnly:    true,
		Risk:        "low",
	}, driftReportHandler))
	must(g.Register(Tool{
		Name:        "safe_input_plan",
		Description: "Create a review-only text input plan without typing into the live desktop.",
		InputSchema: schema([]string{"target", "text"}, map[string]any{"target": map[string]string{"type": "string"}, "text": map[string]string{"type": "string"}}),
		ReadOnly:    true,
		Risk:        "medium",
	}, inputPlanHandler))
	return g
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func statusHandler(_ context.Context, req Request) (Response, error) {
	profile := clean(req.Params["profile"])
	if profile == "" {
		return Response{}, fmt.Errorf("profile parameter is required")
	}
	return Response{OK: true, Payload: map[string]any{
		"profile":             profile,
		"session":             "synthetic-wayland",
		"semantic_probe":      "available",
		"clipboard_policy":    "redacted",
		"mutation_mode":       "dry-run",
		"public_boundary":     "no live host state read",
		"recommended_profile": "desktop",
	}}, nil
}

func focusPlanHandler(_ context.Context, req Request) (Response, error) {
	app := clean(req.Params["app"])
	intent := clean(req.Params["intent"])
	if app == "" || intent == "" {
		return Response{}, fmt.Errorf("app and intent parameters are required")
	}
	return Response{OK: true, Payload: map[string]any{
		"app":    app,
		"intent": intent,
		"plan": []string{
			"discover visible windows from a synthetic tree",
			"choose the highest-confidence app match",
			"emit focus command as a review-only plan",
		},
	}}, nil
}

func driftReportHandler(_ context.Context, req Request) (Response, error) {
	component := clean(req.Params["component"])
	if component == "" {
		return Response{}, fmt.Errorf("component parameter is required")
	}
	return Response{OK: true, Payload: map[string]any{
		"component": component,
		"status":    "review",
		"checks": []string{
			"compare generated config against committed fixture",
			"flag unmanaged local-only changes",
			"require explicit approval before writing host config",
		},
	}}, nil
}

func inputPlanHandler(_ context.Context, req Request) (Response, error) {
	target := clean(req.Params["target"])
	text := clean(req.Params["text"])
	if target == "" || text == "" {
		return Response{}, fmt.Errorf("target and text parameters are required")
	}
	return Response{OK: true, Payload: map[string]any{
		"target":       target,
		"text_preview": preview(text),
		"plan": []string{
			"resolve a synthetic element reference",
			"show the exact text preview to the operator",
			"stop before any live keystroke or paste action",
		},
	}}, nil
}

func clean(value string) string {
	return strings.TrimSpace(value)
}

func preview(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 24 {
		return value
	}
	return value[:24] + "..."
}
