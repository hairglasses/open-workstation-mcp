package gate

import (
	"context"
	"fmt"
	"sort"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

type Verdict int

const (
	VerdictProceed Verdict = iota
	VerdictPause
	VerdictDeny
)

func (v Verdict) String() string {
	switch v {
	case VerdictProceed:
		return "proceed"
	case VerdictPause:
		return "pause"
	case VerdictDeny:
		return "deny"
	default:
		return "unknown"
	}
}

type GateFunc func(ctx context.Context, name string, td registry.ToolDefinition, req registry.CallToolRequest) Verdict

type ApprovalFunc func(ctx context.Context, name string, td registry.ToolDefinition, req registry.CallToolRequest) bool

type Config struct {
	Gate                GateFunc
	Approval            ApprovalFunc
	DenyMessage         string
	PauseTimeoutMessage string
}

func Middleware(config Config) registry.Middleware {
	if config.DenyMessage == "" {
		config.DenyMessage = "tool call denied by gate"
	}
	if config.PauseTimeoutMessage == "" {
		config.PauseTimeoutMessage = "tool call not approved"
	}

	return func(name string, td registry.ToolDefinition, next registry.ToolHandlerFunc) registry.ToolHandlerFunc {
		return func(ctx context.Context, req registry.CallToolRequest) (*registry.CallToolResult, error) {
			verdict := config.Gate(ctx, name, td, req)

			switch verdict {
			case VerdictProceed:
				return next(ctx, req)

			case VerdictPause:
				if config.Approval == nil {
					return registry.MakeErrorResult(
						fmt.Sprintf("[GATE_DENIED] %s (no approval handler configured)", config.DenyMessage),
					), nil
				}
				if !config.Approval(ctx, name, td, req) {
					return registry.MakeErrorResult(
						fmt.Sprintf("[GATE_DENIED] %s", config.PauseTimeoutMessage),
					), nil
				}
				return next(ctx, req)

			case VerdictDeny:
				return registry.MakeErrorResult(
					fmt.Sprintf("[GATE_DENIED] %s", config.DenyMessage),
				), nil

			default:
				return registry.MakeErrorResult(
					fmt.Sprintf("[GATE_DENIED] unknown verdict: %d", verdict),
				), nil
			}
		}
	}
}

func AlwaysProceed(_ context.Context, _ string, _ registry.ToolDefinition, _ registry.CallToolRequest) Verdict {
	return VerdictProceed
}

func DenyWrites(_ context.Context, _ string, td registry.ToolDefinition, _ registry.CallToolRequest) Verdict {
	if td.IsWrite {
		return VerdictDeny
	}
	return VerdictProceed
}

func PauseWrites(_ context.Context, _ string, td registry.ToolDefinition, _ registry.CallToolRequest) Verdict {
	if td.IsWrite {
		return VerdictPause
	}
	return VerdictProceed
}

func ChainGates(gates ...GateFunc) GateFunc {
	return func(ctx context.Context, name string, td registry.ToolDefinition, req registry.CallToolRequest) Verdict {
		for _, g := range gates {
			if v := g(ctx, name, td, req); v != VerdictProceed {
				return v
			}
		}
		return VerdictProceed
	}
}

type PriorityGate struct {
	Priority int
	Gate     GateFunc
}

func PriorityChain(gates ...PriorityGate) GateFunc {
	sorted := make([]PriorityGate, len(gates))
	copy(sorted, gates)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority > sorted[j].Priority
	})
	funcs := make([]GateFunc, len(sorted))
	for i, g := range sorted {
		funcs[i] = g.Gate
	}
	return ChainGates(funcs...)
}
