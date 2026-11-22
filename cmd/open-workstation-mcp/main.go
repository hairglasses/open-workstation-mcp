package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/middleware/gate"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/observability"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/slogcfg"
	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/stdioguard"
	"github.com/hairglasses/open-workstation-mcp/internal/omarchy"
	"github.com/hairglasses/open-workstation-mcp/internal/process"
	"github.com/hairglasses/open-workstation-mcp/internal/systemd"
	"github.com/hairglasses/open-workstation-mcp/internal/tmux"
	"github.com/hairglasses/open-workstation-mcp/internal/workstationmcp"
)

func BuildRegistry() *registry.ToolRegistry {
	reg := registry.NewToolRegistry(registry.Config{
		Middleware: []registry.Middleware{
			gate.Middleware(gate.Config{Gate: gate.PauseWrites}),
			registry.AuditMiddleware(""),
			registry.SafetyTierMiddleware(),
		},
	})

	reg.RegisterModule(&systemd.SystemdModule{})
	reg.RegisterModule(&tmux.TmuxModule{})
	reg.RegisterModule(&process.ProcessModule{})
	reg.RegisterModule(&omarchy.OmarchyModule{})

	return reg
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "manifest":
			if err := printJSON(workstationmcp.SampleGateway().Manifest()); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "call":
			if err := call(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "-tools", "--tools", "tools":
			reg := BuildRegistry()
			tools := reg.GetAllToolDefinitions()
			fmt.Printf("Registered %d tools across systemd, tmux, process, and omarchy domains:\n", len(tools))
			for _, t := range tools {
				fmt.Printf("  - %s: %s\n", t.Tool.Name, t.Tool.Description)
			}
			return
		case "help", "-h", "--help":
			if err := usage(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(0)
			}
			return
		}
	}

	if os.Getenv("MCP_SOCKET_PATH") == "" {
		_, restoreStdio, _ := stdioguard.GuardStdio()
		defer restoreStdio()
	}

	ctx := context.Background()

	slogcfg.Init(slogcfg.Config{
		ServiceName: "open-workstation-mcp",
	})

	obs, obsShutdown, err := observability.Init(ctx, observability.Config{
		ServiceName:    "open-workstation-mcp",
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

	reg := BuildRegistry()
	s := registry.NewMCPServer("open-workstation-mcp", "1.0.0")
	reg.RegisterWithServer(s)

	systemd.RegisterResourcesAndPrompts(s)
	tmux.RegisterResourcesAndPrompts(s)
	process.RegisterResourcesAndPrompts(s)
	omarchy.RegisterResourcesAndPrompts(s)

	if obs != nil {
		reg.SetMiddleware([]registry.Middleware{
			obs.Middleware(),
			gate.Middleware(gate.Config{Gate: gate.PauseWrites}),
			registry.AuditMiddleware(""),
			registry.SafetyTierMiddleware(),
		})
	}

	slog.Info("open-workstation-mcp server starting", "tools", reg.ToolCount(), "prometheus_port", "9095")
	if err := registry.ServeAuto(s); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func call(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: open-workstation-mcp call <tool> [--param key=value]")
	}
	tool := args[0]
	fs := flag.NewFlagSet("call", flag.ContinueOnError)
	params := multiFlag{}
	fs.Var(&params, "param", "key=value parameter")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	resp, err := workstationmcp.SampleGateway().Call(context.Background(), workstationmcp.Request{Tool: tool, Params: params.Map()})
	if err != nil {
		return err
	}
	return printJSON(resp)
}

func usage() error {
	return errors.New("usage: open-workstation-mcp [manifest | call <tool> [--param key=value] | -tools]")
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(value string) error {
	if !strings.Contains(value, "=") {
		return fmt.Errorf("param %q must be key=value", value)
	}
	*m = append(*m, value)
	return nil
}

func (m multiFlag) Map() map[string]string {
	out := map[string]string{}
	for _, item := range m {
		key, value, _ := strings.Cut(item, "=")
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
