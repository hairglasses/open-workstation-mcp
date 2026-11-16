package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
	"github.com/mark3labs/mcp-go/mcp"
)

type ResourceHandlerFunc = registry.ResourceHandlerFunc

type Middleware func(uri string, rd ResourceDefinition, next ResourceHandlerFunc) ResourceHandlerFunc

type ResourceDefinition struct {
	Resource registry.Resource
	Handler  ResourceHandlerFunc
	Category string
	Tags     []string
	CacheTTL time.Duration
}

type TemplateDefinition struct {
	Template registry.ResourceTemplate
	Handler  ResourceHandlerFunc
	Category string
	Tags     []string
}

type ResourceModule interface {
	Name() string
	Description() string
	Resources() []ResourceDefinition
	Templates() []TemplateDefinition
}

type Config struct {
	Middleware []Middleware
}

type ResourceRegistry struct {
	mu        sync.RWMutex
	modules   map[string]ResourceModule
	resources map[string]ResourceDefinition
	templates []TemplateDefinition
	config    Config
}

func NewResourceRegistry(config ...Config) *ResourceRegistry {
	var cfg Config
	if len(config) > 0 {
		cfg = config[0]
	}
	return &ResourceRegistry{
		modules:   make(map[string]ResourceModule),
		resources: make(map[string]ResourceDefinition),
		config:    cfg,
	}
}

func (r *ResourceRegistry) RegisterModule(module ResourceModule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modules[module.Name()] = module
	for _, rd := range module.Resources() {
		r.resources[rd.Resource.URI] = rd
	}
	r.templates = append(r.templates, module.Templates()...)
}

func (r *ResourceRegistry) ResourceCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.resources)
}

func (r *ResourceRegistry) TemplateCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.templates)
}

func (r *ResourceRegistry) GetResource(uri string) (ResourceDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rd, ok := r.resources[uri]
	return rd, ok
}

func (r *ResourceRegistry) wrapHandler(rd ResourceDefinition) ResourceHandlerFunc {
	h := rd.Handler
	r.mu.RLock()
	mw := append([]Middleware(nil), r.config.Middleware...)
	r.mu.RUnlock()
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](rd.Resource.URI, rd, h)
	}
	return h
}

func (r *ResourceRegistry) RegisterWithServer(s *registry.MCPServer) {
	r.mu.RLock()
	defs := make([]ResourceDefinition, 0, len(r.resources))
	for _, rd := range r.resources {
		defs = append(defs, rd)
	}
	tmpls := append([]TemplateDefinition(nil), r.templates...)
	r.mu.RUnlock()

	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Resource.URI < defs[j].Resource.URI
	})

	for _, rd := range defs {
		rdCopy := rd
		registry.AddResourceToServer(s, rdCopy.Resource, r.wrapHandler(rdCopy))
	}
	for _, td := range tmpls {
		tdCopy := td
		registry.AddResourceTemplateToServer(s, tdCopy.Template, tdCopy.Handler)
	}
}

func TextResourceHandler(fn func(ctx context.Context, uri string) (mimeType, text string, err error)) ResourceHandlerFunc {
	return func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		mimeType, text, err := fn(ctx, req.Params.URI)
		if err != nil {
			return nil, err
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: mimeType,
				Text:     text,
			},
		}, nil
	}
}

func JSONResourceHandler(fn func(ctx context.Context, uri string) (any, error)) ResourceHandlerFunc {
	return func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		data, err := fn(ctx, req.Params.URI)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(b),
			},
		}, nil
	}
}

func CallHandlerText(ctx context.Context, h ResourceHandlerFunc, uri string) (string, string, error) {
	contents, err := h(ctx, mcp.ReadResourceRequest{Params: mcp.ReadResourceParams{URI: uri}})
	if err != nil {
		return "", "", err
	}
	if len(contents) == 0 {
		return "", "", fmt.Errorf("resources: handler returned no contents for %q", uri)
	}
	tc, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		return "", "", fmt.Errorf("resources: first content for %q is %T, want TextResourceContents", uri, contents[0])
	}
	return tc.MIMEType, tc.Text, nil
}
