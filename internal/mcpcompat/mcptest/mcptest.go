package mcptest

import (
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

type Server struct {
	t        testing.TB
	Registry *registry.ToolRegistry
	MCP      *registry.MCPServer
}

type Option func(*Server)

func NewServer(t testing.TB, reg *registry.ToolRegistry, opts ...Option) *Server {
	t.Helper()
	s := &Server{
		t:        t,
		Registry: reg,
		MCP:      registry.NewMCPServer("mcptest", "0.0.0-test"),
	}
	for _, opt := range opts {
		opt(s)
	}
	reg.RegisterWithServer(s.MCP)
	return s
}

func (s *Server) HasTool(name string) bool {
	_, ok := s.Registry.GetTool(name)
	return ok
}

func (s *Server) ToolNames() []string {
	return s.Registry.ListTools()
}

func ExtractText(t testing.TB, result *registry.CallToolResult) string {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	text, ok := registry.ExtractTextContent(result.Content[0])
	if !ok {
		t.Fatalf("first content is not TextContent, got %T", result.Content[0])
	}
	return text
}
