package process

import (
	"context"
	"fmt"

	"github.com/hairglasses-studio/mcpkit/prompts"
	"github.com/hairglasses-studio/mcpkit/registry"
	"github.com/hairglasses-studio/mcpkit/resources"
)

// newDocResource builds a registry-aliased Resource via mcpkit's compat
// constructor plus field assignment, instead of mcp-go's functional options
// (mcp.WithResourceDescription etc.), which the compat layer deliberately
// does not re-export. This file must not import mark3labs/mcp-go (or
// modelcontextprotocol/go-sdk) directly so it compiles under both build
// tags unchanged.
func newDocResource(uri, name, description, mimeType string) registry.Resource {
	r := registry.NewResource(uri, name)
	r.Description = description
	r.MIMEType = mimeType
	return r
}

type processResourceModule struct{}

func (m *processResourceModule) Name() string { return "process_context" }
func (m *processResourceModule) Description() string {
	return "Reusable process and service investigation context"
}

func (m *processResourceModule) Resources() []resources.ResourceDefinition {
	return []resources.ResourceDefinition{
		{
			Resource: newDocResource(
				"process://workflows/service-investigation",
				"Service Investigation Workflow",
				"Compact guide for tracing a process, service, or port issue",
				"text/markdown",
			),
			Handler: resources.TextResourceHandler(func(_ context.Context, _ string) (string, string, error) {
				return "text/markdown", "1. Use `ps_list` or `port_list` to identify the target process.\n2. Use `ps_tree` when parent/child relationships matter.\n3. Use `investigate_port` or `investigate_service` for a bounded composed investigation with recent logs.\n4. Only use `kill_process` after confirming the exact PID and signal.", nil
			}),
			Category: "workflow",
			Tags:     []string{"process", "debugging", "workflow"},
		},
	}
}

func (m *processResourceModule) Templates() []resources.TemplateDefinition { return nil }

type processPromptModule struct{}

func (m *processPromptModule) Name() string { return "process_prompts" }
func (m *processPromptModule) Description() string {
	return "Prompt workflows for process and service debugging"
}

func (m *processPromptModule) Prompts() []prompts.PromptDefinition {
	return []prompts.PromptDefinition{
		{
			Prompt: registry.MakePrompt(
				"process_debug_target",
				"Investigate a service or port with bounded outputs before any kill action",
				registry.PromptArgument{Name: "target", Description: "Service unit or port to investigate", Required: true},
				registry.PromptArgument{Name: "kind", Description: "service or port"},
			),
			Handler: prompts.TextPromptHandler(func(_ context.Context, args map[string]string) (string, string, error) {
				target := args["target"]
				kind := args["kind"]
				if kind == "" {
					kind = "service"
				}
				return "Investigate process target " + target, fmt.Sprintf(
					"Investigate the %s target %q. Prefer `investigate_service` or `investigate_port` for a bounded overview, then drill into `ps_tree`, `ps_list`, or `port_list` only if needed. Do not suggest `kill_process` unless the evidence clearly identifies the right PID and signal.",
					kind, target,
				), nil
			}),
			Category: "workflow",
			Tags:     []string{"process", "workflow", "debugging"},
		},
	}
}

func buildProcessResourceRegistry() *resources.ResourceRegistry {
	reg := resources.NewResourceRegistry()
	reg.RegisterModule(&processResourceModule{})
	return reg
}

func buildProcessPromptRegistry() *prompts.PromptRegistry {
	reg := prompts.NewPromptRegistry()
	reg.RegisterModule(&processPromptModule{})
	return reg
}

func RegisterResourcesAndPrompts(s *registry.MCPServer) {
	buildProcessResourceRegistry().RegisterWithServer(s)
	buildProcessPromptRegistry().RegisterWithServer(s)
}

