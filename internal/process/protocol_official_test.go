//go:build official_sdk

package process

import (
	"context"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOfficialSDKNegotiates20260728Discovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, server := Setup()

	var (
		mu                   sync.Mutex
		methods              []string
		discoverRequestTyped bool
		discoverVersion      string
		listToolsVersion     string
	)
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			mu.Lock()
			methods = append(methods, method)
			if method == "server/discover" {
				discover, ok := req.(*mcp.ServerRequest[*mcp.DiscoverParams])
				discoverRequestTyped = ok
				if ok && discover.Params != nil {
					discoverVersion, _ = discover.Params.GetMeta()[mcp.MetaKeyProtocolVersion].(string)
				}
			}
			if method == "tools/list" {
				listTools, ok := req.(*mcp.ServerRequest[*mcp.ListToolsParams])
				if ok && listTools.Params != nil {
					listToolsVersion, _ = listTools.Params.GetMeta()[mcp.MetaKeyProtocolVersion].(string)
				}
			}
			mu.Unlock()
			return next(ctx, method, req)
		}
	})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect process server: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "process-mcp-protocol-test",
		Version: "1.0.0-test",
	}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect official SDK client: %v", err)
	}
	defer clientSession.Close()

	negotiated := clientSession.InitializeResult()
	if negotiated == nil {
		t.Fatal("discovery did not populate the negotiated server result")
	}
	if got, want := negotiated.ProtocolVersion, "2026-07-28"; got != want {
		t.Fatalf("negotiated protocol version = %q, want %q", got, want)
	}

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools after discovery: %v", err)
	}
	gotTools := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		gotTools = append(gotTools, tool.Name)
	}
	sort.Strings(gotTools)
	wantTools := []string{
		"dbus_probe",
		"fd_list",
		"gpu_status",
		"investigate_cpu",
		"investigate_memory",
		"investigate_port",
		"investigate_service",
		"kill_process",
		"liveness_gate",
		"mem_map",
		"net_connections",
		"port_list",
		"proc_inspect",
		"ps_list",
		"ps_search",
		"ps_tree",
		"system_info",
	}
	if !slices.Equal(gotTools, wantTools) {
		t.Fatalf("discovered tools = %v, want %v", gotTools, wantTools)
	}

	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(methods, "server/discover") {
		t.Fatalf("wire methods = %v, want server/discover", methods)
	}
	if !discoverRequestTyped {
		t.Fatal("server/discover did not use the official SDK DiscoverParams request type")
	}
	if slices.Contains(methods, "initialize") {
		t.Fatalf("wire methods = %v, legacy initialize handshake must not run", methods)
	}
	if got, want := discoverVersion, "2026-07-28"; got != want {
		t.Fatalf("server/discover protocol metadata = %q, want %q", got, want)
	}
	if got, want := listToolsVersion, "2026-07-28"; got != want {
		t.Fatalf("stateless tools/list protocol metadata = %q, want %q", got, want)
	}
}
