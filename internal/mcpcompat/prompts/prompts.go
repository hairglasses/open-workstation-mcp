package prompts

import (
	"context"
	"sort"
	"sync"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
	"github.com/mark3labs/mcp-go/mcp"
)

type PromptHandlerFunc = registry.PromptHandlerFunc

type Middleware func(name string, pd PromptDefinition, next PromptHandlerFunc) PromptHandlerFunc

type PromptDefinition struct {
	Prompt   registry.Prompt
	Handler  PromptHandlerFunc
	Category string
	Tags     []string
	Version  string
}

type PromptModule interface {
	Name() string
	Description() string
	Prompts() []PromptDefinition
}

type Config struct {
	Middleware []Middleware
}

type PromptRegistry struct {
	mu      sync.RWMutex
	modules map[string]PromptModule
	prompts map[string]PromptDefinition
	config  Config
}

func NewPromptRegistry(config ...Config) *PromptRegistry {
	var cfg Config
	if len(config) > 0 {
		cfg = config[0]
	}
	return &PromptRegistry{
		modules: make(map[string]PromptModule),
		prompts: make(map[string]PromptDefinition),
		config:  cfg,
	}
}

func (r *PromptRegistry) RegisterModule(module PromptModule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modules[module.Name()] = module
	for _, pd := range module.Prompts() {
		r.prompts[pd.Prompt.Name] = pd
	}
}

func (r *PromptRegistry) PromptCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.prompts)
}

func (r *PromptRegistry) GetPrompt(name string) (PromptDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pd, ok := r.prompts[name]
	return pd, ok
}

func (r *PromptRegistry) ListPrompts() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.prompts))
	for name := range r.prompts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *PromptRegistry) wrapHandler(pd PromptDefinition) PromptHandlerFunc {
	h := pd.Handler
	r.mu.RLock()
	mw := append([]Middleware(nil), r.config.Middleware...)
	r.mu.RUnlock()
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](pd.Prompt.Name, pd, h)
	}
	return h
}

func (r *PromptRegistry) RegisterWithServer(s *registry.MCPServer) {
	r.mu.RLock()
	defs := make([]PromptDefinition, 0, len(r.prompts))
	for _, pd := range r.prompts {
		defs = append(defs, pd)
	}
	r.mu.RUnlock()

	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Prompt.Name < defs[j].Prompt.Name
	})

	for _, pd := range defs {
		pdCopy := pd
		registry.AddPromptToServer(s, pdCopy.Prompt, r.wrapHandler(pdCopy))
	}
}

func TextPromptHandler(fn func(ctx context.Context, args map[string]string) (description, text string, err error)) PromptHandlerFunc {
	return func(ctx context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		description, text, err := fn(ctx, req.Params.Arguments)
		if err != nil {
			return nil, err
		}
		return &mcp.GetPromptResult{
			Description: description,
			Messages: []mcp.PromptMessage{
				mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(text)),
			},
		}, nil
	}
}
