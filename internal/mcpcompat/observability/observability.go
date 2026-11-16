package observability

import (
	"context"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

type Config struct {
	ServiceName    string
	ServiceVersion string
	EnableMetrics  bool
	EnableTracing  bool
	OTLPEndpoint   string
	PrometheusPort string
}

type Provider struct {
	cfg Config
}

func Init(_ context.Context, cfg Config) (*Provider, func(context.Context) error, error) {
	p := &Provider{cfg: cfg}
	shutdown := func(_ context.Context) error { return nil }
	return p, shutdown, nil
}

func (p *Provider) Middleware() registry.Middleware {
	return func(_ string, _ registry.ToolDefinition, next registry.ToolHandlerFunc) registry.ToolHandlerFunc {
		return next
	}
}
