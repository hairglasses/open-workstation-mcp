package omarchy

import (
	"context"
	"fmt"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/prompts"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/resources"
)

func newDocResource(uri, name, description, mimeType string) registry.Resource {
	r := registry.NewResource(uri, name)
	r.Description = description
	r.MIMEType = mimeType
	return r
}

type omarchyResourceModule struct{}

func (m *omarchyResourceModule) Name() string { return "omarchy_context" }
func (m *omarchyResourceModule) Description() string {
	return "Omarchy, Hyprland, and Ghostty safe desktop control guidance"
}

func (m *omarchyResourceModule) Resources() []resources.ResourceDefinition {
	return []resources.ResourceDefinition{
		{
			Resource: newDocResource(
				"omarchy://workflows/desktop-control",
				"Omarchy Desktop Control Workflow",
				"Safe introspection and desktop control sequence for Omarchy 4.5, Hyprland, and Ghostty",
				"text/markdown",
			),
			Handler: resources.TextResourceHandler(func(_ context.Context, _ string) (string, string, error) {
				return "text/markdown", "1. Run `omarchy_status` and `omarchy_config_validate` to verify desktop flavor (`hyprland`, `SKIPPED_NIRI`, `SKIPPED_HEADLESS`), `hyprctl configerrors`, and Ghostty include order.\n2. Use `omarchy_commands` or `omarchy_hw` for read-only discovery and `omarchy_debug` (`--no-sudo --print` enforced) for bounded diagnostics.\n3. Preview controlled mutations with `dry_run: true` before invoking `omarchy_theme`, `omarchy_font`, `omarchy_plugin`, `omarchy_bar`, `omarchy_hook`, `omarchy_restart`, `omarchy_toggle`, or `omarchy_reminder`.\n4. Reload Ghostty via `omarchy_restart` (`target: terminal`, `SIGUSR2`) and respect the `clipse`-only clipboard policy.", nil
			}),
			Category: "workflow",
			Tags:     []string{"omarchy", "hyprland", "ghostty", "workflow"},
		},
	}
}

func (m *omarchyResourceModule) Templates() []resources.TemplateDefinition { return nil }

type omarchyPromptModule struct{}

func (m *omarchyPromptModule) Name() string { return "omarchy_prompts" }
func (m *omarchyPromptModule) Description() string {
	return "Prompt workflows for Omarchy workstation inspection and control"
}

func (m *omarchyPromptModule) Prompts() []prompts.PromptDefinition {
	return []prompts.PromptDefinition{
		{
			Prompt: registry.MakePrompt(
				"omarchy_safe_control",
				"Inspect Omarchy/Hyprland/Ghostty health and plan a safe desktop control action",
				registry.PromptArgument{Name: "goal", Description: "Desired desktop inspection or change", Required: true},
				registry.PromptArgument{Name: "component", Description: "Target surface: theme, font, plugin, bar, hook, ghostty, or hyprland"},
			),
			Handler: prompts.TextPromptHandler(func(_ context.Context, args map[string]string) (string, string, error) {
				goal := args["goal"]
				component := args["component"]
				if component == "" {
					component = "all"
				}
				return "Safe Omarchy control for " + component, fmt.Sprintf(
					"Achieve %q on component %q. Start with `omarchy_status` and `omarchy_config_validate` to check flavor fencing and config health, preview controlled changes with `dry_run: true`, never edit `/usr/share/omarchy`, use `SIGUSR2` / `omarchy_restart(target=\"terminal\")` for Ghostty, and enforce `clipse`-only clipboard handling.",
					goal, component,
				), nil
			}),
			Category: "workflow",
			Tags:     []string{"omarchy", "hyprland", "ghostty"},
		},
	}
}

func buildOmarchyResourceRegistry() *resources.ResourceRegistry {
	reg := resources.NewResourceRegistry()
	reg.RegisterModule(&omarchyResourceModule{})
	return reg
}

func buildOmarchyPromptRegistry() *prompts.PromptRegistry {
	reg := prompts.NewPromptRegistry()
	reg.RegisterModule(&omarchyPromptModule{})
	return reg
}

func RegisterResourcesAndPrompts(s *registry.MCPServer) {
	buildOmarchyResourceRegistry().RegisterWithServer(s)
	buildOmarchyPromptRegistry().RegisterWithServer(s)
}
