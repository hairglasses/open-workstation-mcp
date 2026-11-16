package registry

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type stubModule struct {
	defs []ToolDefinition
}

func (m stubModule) Name() string            { return "stub" }
func (m stubModule) Description() string     { return "stub module" }
func (m stubModule) Tools() []ToolDefinition { return m.defs }

func TestRegisterWithServerAndSetMiddlewareExecutesWithoutRecursion(t *testing.T) {
	baseDir := t.TempDir()
	auditDir := filepath.Join(baseDir, "state-dir")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	auditPath := filepath.Join(auditDir, "audit.jsonl")

	reg := NewToolRegistry()
	calls := 0
	reg.RegisterModule(stubModule{
		defs: []ToolDefinition{
			{
				Tool: Tool{
					Name:        "ping_read",
					Description: "ping tool",
				},
				Handler: func(_ context.Context, _ CallToolRequest) (*CallToolResult, error) {
					calls++
					return MakeTextResult("pong"), nil
				},
			},
		},
	})

	s := NewMCPServer("test-server", "1.0.0")
	reg.RegisterWithServer(s)

	mwCalled := 0
	reg.SetMiddleware([]Middleware{
		func(_ string, _ ToolDefinition, next ToolHandlerFunc) ToolHandlerFunc {
			return func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
				mwCalled++
				return next(ctx, req)
			}
		},
		AuditMiddleware(auditPath),
		SafetyTierMiddleware(),
	})

	initMsg := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	if resp := s.HandleMessage(context.Background(), initMsg); resp == nil {
		t.Fatal("nil initialize response")
	}

	callMsg := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ping_read","arguments":{"alpha":"1"}}}`)
	resp := s.HandleMessage(context.Background(), callMsg)
	if resp == nil {
		t.Fatal("nil tools/call response")
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if !strings.Contains(string(raw), "pong") {
		t.Fatalf("expected pong in response, got %s", string(raw))
	}
	if calls != 1 || mwCalled != 1 {
		t.Fatalf("expected calls=1 mwCalled=1, got calls=%d mwCalled=%d", calls, mwCalled)
	}

	if runtime.GOOS != "windows" {
		dfi, err := os.Lstat(auditDir)
		if err != nil {
			t.Fatalf("lstat auditDir: %v", err)
		}
		if dfi.Mode().Perm() != 0o700 {
			t.Fatalf("expected auditDir perm 0700, got %o", dfi.Mode().Perm())
		}
		ffi, err := os.Lstat(auditPath)
		if err != nil {
			t.Fatalf("lstat auditPath: %v", err)
		}
		if ffi.Mode().Perm() != 0o600 {
			t.Fatalf("expected auditPath perm 0600, got %o", ffi.Mode().Perm())
		}
	}
}

func TestServeUnixSocketRefusesSymlinkParentAndBlocker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix domain socket test requires linux or darwin")
	}
	baseDir := t.TempDir()
	s := NewMCPServer("uds-test", "1.0.0")

	targetDir := filepath.Join(baseDir, "external-target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir targetDir: %v", err)
	}
	if err := os.Chmod(targetDir, 0o755); err != nil {
		t.Fatalf("chmod targetDir: %v", err)
	}
	symParent := filepath.Join(baseDir, "sym-parent")
	if err := os.Symlink(targetDir, symParent); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := serveUnixSocket(s, filepath.Join(symParent, "mcp.sock")); err == nil {
		t.Fatal("expected refusal on symlink parent")
	}
	if err := serveUnixSocket(s, filepath.Join(symParent, "nested", "mcp.sock")); err == nil {
		t.Fatal("expected refusal on symlink ancestor")
	}
	tfi, err := os.Lstat(targetDir)
	if err != nil {
		t.Fatalf("lstat targetDir: %v", err)
	}
	if tfi.Mode().Perm() != 0o755 {
		t.Fatalf("symlink target dir permissions mutated: got %o, want 0755", tfi.Mode().Perm())
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		t.Fatalf("readdir targetDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty targetDir, got %d entries", len(entries))
	}

	blocker := filepath.Join(baseDir, "file-blocker")
	if err := os.WriteFile(blocker, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	if err := serveUnixSocket(s, filepath.Join(blocker, "mcp.sock")); err == nil {
		t.Fatal("expected refusal on regular-file parent blocker")
	}

	sockDir := filepath.Join(baseDir, "sock-dir")
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatalf("mkdir sockDir: %v", err)
	}
	regularAtSock := filepath.Join(sockDir, "regular.sock")
	if err := os.WriteFile(regularAtSock, []byte("not-a-socket"), 0o600); err != nil {
		t.Fatalf("write regularAtSock: %v", err)
	}
	if err := serveUnixSocket(s, regularAtSock); err == nil {
		t.Fatal("expected refusal on regular file at socket path")
	}
	if _, err := os.Lstat(regularAtSock); err != nil {
		t.Fatalf("regular file at socket path was removed: %v", err)
	}

	liveSock, err := os.MkdirTemp("", "owm-uds-")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(liveSock) }()
	liveSockPath := filepath.Join(liveSock, "live.sock")
	ln, err := net.Listen("unix", liveSockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer func() { _ = ln.Close() }()
	if err := serveUnixSocket(s, liveSockPath); err == nil {
		t.Fatal("expected refusal on active unix socket")
	}
}

func TestAuditWriterRefusesSymlinkParentAndSymlinkFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink guard test requires linux or darwin")
	}
	baseDir := t.TempDir()

	targetDir := filepath.Join(baseDir, "audit-ext")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir targetDir: %v", err)
	}
	if err := os.Chmod(targetDir, 0o755); err != nil {
		t.Fatalf("chmod targetDir: %v", err)
	}
	symDir := filepath.Join(baseDir, "sym-audit")
	if err := os.Symlink(targetDir, symDir); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	wSymDir := &auditWriter{logPath: filepath.Join(symDir, "audit.jsonl")}
	wSymDir.write(AuditEntry{Tool: "ps_list", Tier: TierReadonly})
	dfi, err := os.Lstat(targetDir)
	if err != nil {
		t.Fatalf("lstat targetDir: %v", err)
	}
	if dfi.Mode().Perm() != 0o755 {
		t.Fatalf("targetDir permissions mutated: got %o", dfi.Mode().Perm())
	}
	if _, err := os.Lstat(filepath.Join(targetDir, "audit.jsonl")); !os.IsNotExist(err) {
		t.Fatal("audit.jsonl unexpectedly created inside symlink target dir")
	}

	realDir := filepath.Join(baseDir, "real-audit")
	if err := os.MkdirAll(realDir, 0o700); err != nil {
		t.Fatalf("mkdir realDir: %v", err)
	}
	victim := filepath.Join(baseDir, "victim.txt")
	if err := os.WriteFile(victim, []byte("SAFE"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	symLog := filepath.Join(realDir, "sym.jsonl")
	if err := os.Symlink(victim, symLog); err != nil {
		t.Fatalf("symlink log: %v", err)
	}
	wSymLog := &auditWriter{logPath: symLog}
	wSymLog.write(AuditEntry{Tool: "ps_list", Tier: TierReadonly})
	gotVictim, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(gotVictim) != "SAFE" {
		t.Fatalf("victim file modified through symlink: %q", string(gotVictim))
	}

	missingTarget := filepath.Join(baseDir, "dangling-target.txt")
	danglingLog := filepath.Join(realDir, "dangling.jsonl")
	if err := os.Symlink(missingTarget, danglingLog); err != nil {
		t.Fatalf("symlink dangling: %v", err)
	}
	wDangling := &auditWriter{logPath: danglingLog}
	wDangling.write(AuditEntry{Tool: "ps_list", Tier: TierReadonly})
	if _, err := os.Lstat(missingTarget); !os.IsNotExist(err) {
		t.Fatal("dangling symlink target was created")
	}
}
