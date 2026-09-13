package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/hairglasses-studio/mcpkit/middleware/gate"
	"github.com/hairglasses-studio/mcpkit/observability"
	"github.com/hairglasses-studio/mcpkit/registry"
	"github.com/hairglasses-studio/mcpkit/slogcfg"
	"github.com/hairglasses/open-workstation-mcp/internal/process"
	"github.com/hairglasses/open-workstation-mcp/internal/systemd"
	"github.com/hairglasses/open-workstation-mcp/internal/tmux"
)

func main() {
	listToolsFlag := flag.Bool("tools", false, "List registered tools and exit")
	flag.Parse()

	ctx := context.Background()

	slogcfg.Init(slogcfg.Config{
		ServiceName: "host-mcp",
	})

	obs, obsShutdown, err := observability.Init(ctx, observability.Config{
		ServiceName:    "host-mcp",
		ServiceVersion: "1.0.0",
		EnableMetrics:  true,
		EnableTracing:  true,
		OTLPEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		PrometheusPort: "9095",
	})
	if err != nil {
		slog.Warn("failed to initialize observability", "error", err)
	}
	defer func() {
		if obsShutdown != nil {
			_ = obsShutdown(context.Background())
		}
	}()

	systemd.InitDBus()

	reg := registry.NewToolRegistry(registry.Config{
		Middleware: []registry.Middleware{
			gate.Middleware(gate.Config{Gate: gate.PauseWrites}),
			registry.AuditMiddleware(""),
			registry.SafetyTierMiddleware(),
		},
	})

	systemdMod := &systemd.SystemdModule{}
	tmuxMod := &tmux.TmuxModule{}
	processMod := &process.ProcessModule{}

	reg.RegisterModule(systemdMod)
	reg.RegisterModule(tmuxMod)
	reg.RegisterModule(processMod)

	if *listToolsFlag {
		tools := reg.GetAllToolDefinitions()
		fmt.Printf("Registered %d tools across systemd, tmux, and process domains:\n", len(tools))
		for _, t := range tools {
			fmt.Printf("  - %s: %s\n", t.Tool.Name, t.Tool.Description)
		}
		return
	}

	s := registry.NewMCPServer("host-mcp", "1.0.0")
	reg.RegisterWithServer(s)

	systemd.RegisterResourcesAndPrompts(s)
	tmux.RegisterResourcesAndPrompts(s)
	process.RegisterResourcesAndPrompts(s)

	if obs != nil {
		reg.SetMiddleware([]registry.Middleware{
			obs.Middleware(),
			gate.Middleware(gate.Config{Gate: gate.PauseWrites}),
			registry.AuditMiddleware(""),
			registry.SafetyTierMiddleware(),
		})
	}

	slog.Info("host-mcp server starting", "tools", reg.ToolCount(), "prometheus_port", "9095")
	if err := registry.ServeAuto(s); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
