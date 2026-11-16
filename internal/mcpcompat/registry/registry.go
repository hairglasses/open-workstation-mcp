package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/stdioguard"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type (
	Tool                 = mcp.Tool
	CallToolRequest      = mcp.CallToolRequest
	CallToolResult       = mcp.CallToolResult
	ToolInputSchema      = mcp.ToolInputSchema
	ToolOutputSchema     = mcp.ToolOutputSchema
	ToolAnnotation       = mcp.ToolAnnotation
	ToolMeta             = mcp.Meta
	TextContent          = mcp.TextContent
	Content              = mcp.Content
	Resource             = mcp.Resource
	ResourceTemplate     = mcp.ResourceTemplate
	ResourceContents     = mcp.ResourceContents
	TextResourceContents = mcp.TextResourceContents
	BlobResourceContents = mcp.BlobResourceContents
	ReadResourceRequest  = mcp.ReadResourceRequest
	ReadResourceResult   = mcp.ReadResourceResult
	Annotations          = mcp.Annotations
	Prompt               = mcp.Prompt
	PromptArgument       = mcp.PromptArgument
	PromptMessage        = mcp.PromptMessage
	GetPromptRequest     = mcp.GetPromptRequest
	GetPromptResult      = mcp.GetPromptResult
	Role                 = mcp.Role
	MCPServer            = server.MCPServer
	ResourceHandlerFunc  = func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)
	PromptHandlerFunc    = func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error)
)

var (
	NewToolResultText   = mcp.NewToolResultText
	NewToolResultError  = mcp.NewToolResultError
	NewResource         = mcp.NewResource
	NewResourceTemplate = mcp.NewResourceTemplate
	NewPrompt           = mcp.NewPrompt
	NewPromptMessage    = mcp.NewPromptMessage
	NewTextContent      = mcp.NewTextContent
)

type ToolHandlerFunc = server.ToolHandlerFunc

type Middleware func(name string, td ToolDefinition, next ToolHandlerFunc) ToolHandlerFunc

type ToolComplexity string

const (
	ComplexitySimple   ToolComplexity = "simple"
	ComplexityModerate ToolComplexity = "moderate"
	ComplexityComplex  ToolComplexity = "complex"

	DefaultToolTimeout     = 30 * time.Second
	DefaultMaxResponseSize = 128 * 1024
)

type ToolDefinition struct {
	Tool                Tool
	Handler             ToolHandlerFunc
	Category            string
	Subcategory         string
	Tags                []string
	SearchTerms         []string
	UseCases            []string
	Complexity          ToolComplexity
	IsWrite             bool
	Deprecated          bool
	Successor           string
	Version             string
	Timeout             time.Duration
	CircuitBreakerGroup string
	RuntimeGroup        string
	OutputSchema        *ToolOutputSchema
	MaxResultChars      int
	MaxResultBytes      int
	DeferLoading        bool
	CallType            string
	ReadOnlyOverride    *bool
}

type ToolModule interface {
	Name() string
	Description() string
	Tools() []ToolDefinition
}

type Config struct {
	DefaultTimeout     time.Duration
	MaxResponseSize    int
	MaxResultBytes     int
	ToolNamePrefix     string
	RuntimeGroupMapper func(category string) string
	Middleware         []Middleware
}

type ToolRegistry struct {
	mu      sync.RWMutex
	modules map[string]ToolModule
	tools   map[string]ToolDefinition
	config  Config
}

var writeSuffixes = []string{
	"_create", "_delete", "_remove", "_reset", "_send", "_post",
	"_update", "_set", "_add", "_apply", "_import", "_publish",
	"_start", "_stop", "_restart", "_trigger", "_execute", "_run",
	"_record", "_assign", "_unassign", "_move", "_copy", "_rename",
	"_enable", "_disable", "_clear", "_flush", "_purge",
	"_archive", "_restore", "_sync", "_push", "_deploy", "_install",
	"_uninstall", "_register", "_deregister", "_subscribe", "_unsubscribe",
	"_approve", "_reject", "_resolve", "_close", "_reopen",
	"_expire", "_grant", "_revoke", "_patch", "_kill", "_terminate",
	"_rotate", "_migrate", "_merge", "_insert", "_truncate", "_drop",
	"_cancel", "_abort", "_mutate", "_mutation", "_write",
}

func InferIsWrite(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range writeSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func NewToolRegistry(config ...Config) *ToolRegistry {
	var cfg Config
	if len(config) > 0 {
		cfg = config[0]
	}
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = DefaultToolTimeout
	}
	if cfg.MaxResponseSize == 0 {
		cfg.MaxResponseSize = DefaultMaxResponseSize
	}
	if cfg.MaxResultBytes == 0 {
		cfg.MaxResultBytes = DefaultMaxResponseSize
	}
	return &ToolRegistry{
		modules: make(map[string]ToolModule),
		tools:   make(map[string]ToolDefinition),
		config:  cfg,
	}
}

func (r *ToolRegistry) SetMiddleware(middleware []Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config.Middleware = middleware
}

func (r *ToolRegistry) RegisterModule(module ToolModule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modules[module.Name()] = module
	for _, tool := range module.Tools() {
		if tool.RuntimeGroup == "" && r.config.RuntimeGroupMapper != nil {
			tool.RuntimeGroup = r.config.RuntimeGroupMapper(tool.Category)
		}
		if tool.ReadOnlyOverride != nil {
			tool.IsWrite = !*tool.ReadOnlyOverride
		} else if !tool.IsWrite {
			tool.IsWrite = InferIsWrite(tool.Tool.Name)
		}
		r.tools[tool.Tool.Name] = tool
	}
}

func (r *ToolRegistry) GetTool(name string) (ToolDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	td, ok := r.tools[name]
	return td, ok
}

func (r *ToolRegistry) ListTools() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *ToolRegistry) GetAllToolDefinitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolDefinition, 0, len(r.tools))
	for _, td := range r.tools {
		out = append(out, td)
	}
	return out
}

func (r *ToolRegistry) ToolCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

func (r *ToolRegistry) wrapHandler(td ToolDefinition) ToolHandlerFunc {
	h := td.Handler
	if h == nil {
		h = func(_ context.Context, _ CallToolRequest) (*CallToolResult, error) {
			return MakeErrorResult("tool handler not configured"), nil
		}
	}
	r.mu.RLock()
	mw := append([]Middleware(nil), r.config.Middleware...)
	r.mu.RUnlock()
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](td.Tool.Name, td, h)
	}
	return h
}

func (r *ToolRegistry) RegisterWithServer(s *MCPServer) {
	defs := r.GetAllToolDefinitions()
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Tool.Name < defs[j].Tool.Name
	})
	prepared := make([]ToolDefinition, 0, len(defs))
	for _, td := range defs {
		tdCopy := td
		if tdCopy.OutputSchema != nil && tdCopy.Tool.OutputSchema.Type == "" {
			tdCopy.Tool.OutputSchema = *tdCopy.OutputSchema
		}
		baseTD := tdCopy
		tdCopy.Handler = func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			return r.wrapHandler(baseTD)(ctx, req)
		}
		prepared = append(prepared, tdCopy)
	}
	AddToolsToServer(s, prepared...)
}

func NewMCPServer(name, version string, opts ...server.ServerOption) *MCPServer {
	return server.NewMCPServer(name, version, opts...)
}

func AddToolToServer(s *MCPServer, tool mcp.Tool, handler ToolHandlerFunc) {
	AddToolsToServer(s, ToolDefinition{Tool: tool, Handler: handler})
}

func AddToolsToServer(s *MCPServer, definitions ...ToolDefinition) {
	if len(definitions) == 0 {
		return
	}
	tools := make([]server.ServerTool, 0, len(definitions))
	for _, td := range definitions {
		tools = append(tools, server.ServerTool{Tool: td.Tool, Handler: td.Handler})
	}
	s.AddTools(tools...)
}

func AddResourceToServer(s *MCPServer, resource mcp.Resource, handler ResourceHandlerFunc) {
	s.AddResource(resource, handler)
}

func AddResourceTemplateToServer(s *MCPServer, tmpl mcp.ResourceTemplate, handler ResourceHandlerFunc) {
	s.AddResourceTemplate(tmpl, handler)
}

func AddPromptToServer(s *MCPServer, prompt mcp.Prompt, handler PromptHandlerFunc) {
	s.AddPrompt(prompt, handler)
}

func ServeStdio(s *MCPServer) error {
	protoOut, restore, _ := stdioguard.GuardStdio()
	defer restore()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.NewStdioServer(s).Listen(ctx, os.Stdin, protoOut)
}

func ServeAuto(s *MCPServer) error {
	if socketPath := os.Getenv("MCP_SOCKET_PATH"); socketPath != "" {
		return serveUnixSocket(s, socketPath)
	}
	return ServeStdio(s)
}

func hasDotDotSegment(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func ensureSafeParentDir(targetPath string) (string, error) {
	if targetPath == "" || !filepath.IsAbs(targetPath) || hasDotDotSegment(targetPath) {
		return "", fmt.Errorf("unsafe path %s", targetPath)
	}
	cleanPath := filepath.Clean(targetPath)
	dir := filepath.Dir(cleanPath)
	if dir == "" || dir == "." || dir == cleanPath || dir == filepath.Dir(dir) {
		return "", fmt.Errorf("unsafe parent directory %s", dir)
	}
	var chain []string
	for cur := dir; cur != "" && cur != "." && cur != filepath.Dir(cur); cur = filepath.Dir(cur) {
		chain = append(chain, cur)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if runtime.GOOS == "darwin" && (chain[i] == "/var" || chain[i] == "/etc") {
			if sfi, serr := os.Stat(chain[i]); serr == nil && sfi.IsDir() {
				continue
			}
		}
		fi, err := os.Lstat(chain[i])
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return "", fmt.Errorf("refusing non-directory ancestor %s", chain[i])
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return "", fmt.Errorf("refusing non-directory %s", dir)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return cleanPath, nil
}

func serveUnixSocket(s *MCPServer, socketPath string) error {
	cleanPath, err := ensureSafeParentDir(socketPath)
	if err != nil {
		return err
	}
	if fi, err := os.Lstat(cleanPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || fi.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing non-socket path %s", cleanPath)
		}
		if probe, dialErr := net.DialTimeout("unix", cleanPath, 100*time.Millisecond); dialErr == nil {
			_ = probe.Close()
			return fmt.Errorf("refusing active socket %s", cleanPath)
		}
		if err := os.Remove(cleanPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", cleanPath)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()
	defer func() { _ = os.Remove(cleanPath) }()
	if err := os.Chmod(cleanPath, 0o600); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func(c net.Conn) {
			defer func() { _ = c.Close() }()
			_ = server.NewStdioServer(s).Listen(ctx, c, c)
		}(conn)
	}
}

func MakePrompt(name, description string, args ...PromptArgument) Prompt {
	return mcp.Prompt{Name: name, Description: description, Arguments: args}
}

func MakeTextContent(text string) Content {
	return mcp.TextContent{Type: "text", Text: text}
}

func MakeErrorResult(text string) *CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{MakeTextContent(text)},
		IsError: true,
	}
}

func MakeTextResult(text string) *CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{MakeTextContent(text)},
	}
}

func MakeStructuredResult(content Content, data any) *CallToolResult {
	return &mcp.CallToolResult{
		Content:           []mcp.Content{content},
		StructuredContent: data,
	}
}

func IsResultError(r *CallToolResult) bool {
	return r != nil && r.IsError
}

func ExtractTextContent(c Content) (string, bool) {
	tc, ok := c.(mcp.TextContent)
	if !ok {
		return "", false
	}
	return tc.Text, true
}

func ExtractResourceText(result *ReadResourceResult) (string, bool) {
	if result == nil || len(result.Contents) == 0 {
		return "", false
	}
	tc, ok := result.Contents[0].(mcp.TextResourceContents)
	if !ok {
		return "", false
	}
	return tc.Text, true
}

func ExtractArguments(req CallToolRequest) map[string]any {
	if req.Params.Arguments == nil {
		return nil
	}
	args, ok := req.Params.Arguments.(map[string]any)
	if !ok {
		return nil
	}
	return args
}

func NewCallToolRequest(name string, args map[string]any) (CallToolRequest, error) {
	req := CallToolRequest{}
	req.Params.Name = name
	if err := SetCallToolArguments(&req, args); err != nil {
		return CallToolRequest{}, err
	}
	return req, nil
}

func SetCallToolArguments(req *CallToolRequest, args map[string]any) error {
	if req == nil {
		return errors.New("registry: nil CallToolRequest")
	}
	req.Params.Arguments = args
	return nil
}

type SafetyTier string

const (
	TierReadonly    SafetyTier = "readonly"
	TierMutating    SafetyTier = "mutating"
	TierDestructive SafetyTier = "destructive"
)

type safetyTierKeyType struct{}

var safetyTierKey safetyTierKeyType

func ClassifySafetyTier(td ToolDefinition) SafetyTier {
	if !td.IsWrite {
		return TierReadonly
	}
	if td.Complexity == ComplexityComplex {
		return TierDestructive
	}
	return TierMutating
}

func SafetyTierMiddleware() Middleware {
	return func(_ string, td ToolDefinition, next ToolHandlerFunc) ToolHandlerFunc {
		tier := ClassifySafetyTier(td)
		return func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			ctx = context.WithValue(ctx, safetyTierKey, tier)
			return next(ctx, req)
		}
	}
}

type AuditEntry struct {
	Timestamp time.Time  `json:"timestamp"`
	Tool      string     `json:"tool"`
	Tier      SafetyTier `json:"tier"`
	Duration  string     `json:"duration"`
	Error     bool       `json:"error"`
	ParamKeys []string   `json:"param_keys"`
}

const maxAuditFileSize = 10 * 1024 * 1024

func defaultAuditLogPath() string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" || !filepath.IsAbs(stateHome) || hasDotDotSegment(stateHome) {
		home, _ := os.UserHomeDir()
		stateHome = filepath.Join(home, ".local", "state")
	} else {
		cleanState := filepath.Clean(stateHome)
		if cleanState == filepath.Dir(cleanState) {
			home, _ := os.UserHomeDir()
			stateHome = filepath.Join(home, ".local", "state")
		} else if fi, err := os.Lstat(cleanState); err == nil && (fi.Mode()&os.ModeSticky != 0 || fi.Mode().Perm()&0o002 != 0) {
			home, _ := os.UserHomeDir()
			stateHome = filepath.Join(home, ".local", "state")
		}
	}
	return filepath.Join(stateHome, "open-workstation-mcp", "audit.jsonl")
}

type auditWriter struct {
	mu      sync.Mutex
	logPath string
}

func (w *auditWriter) write(entry AuditEntry) {
	w.mu.Lock()
	defer w.mu.Unlock()

	cleanPath, err := ensureSafeParentDir(w.logPath)
	if err != nil {
		slog.Warn("audit: unsafe log directory", "path", w.logPath, "error", err)
		return
	}

	if fi, err := os.Lstat(cleanPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			slog.Warn("audit: refusing non-regular log path", "path", cleanPath)
			return
		}
		if fi.Size() >= maxAuditFileSize {
			rotPath := cleanPath + ".1"
			if rfi, rerr := os.Lstat(rotPath); rerr == nil {
				if rfi.Mode()&os.ModeSymlink != 0 || !rfi.Mode().IsRegular() {
					slog.Warn("audit: refusing non-regular rotated log path", "path", rotPath)
					return
				}
			} else if !os.IsNotExist(rerr) {
				return
			}
			_ = os.Rename(cleanPath, rotPath)
		}
	} else if !os.IsNotExist(err) {
		return
	}

	f, err := os.OpenFile(cleanPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	if ffi, ferr := f.Stat(); ferr != nil || !ffi.Mode().IsRegular() {
		return
	}
	if err := f.Chmod(0o600); err != nil {
		return
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(f, "%s\n", data)
}

func extractParamKeys(req CallToolRequest) []string {
	args := ExtractArguments(req)
	if len(args) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func AuditMiddleware(logPath string) Middleware {
	if logPath == "" {
		logPath = defaultAuditLogPath()
	}
	w := &auditWriter{logPath: logPath}
	return func(name string, td ToolDefinition, next ToolHandlerFunc) ToolHandlerFunc {
		tier := ClassifySafetyTier(td)
		return func(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
			start := time.Now()
			result, err := next(ctx, req)
			w.write(AuditEntry{
				Timestamp: start.UTC(),
				Tool:      name,
				Tier:      tier,
				Duration:  time.Since(start).Round(time.Millisecond).String(),
				Error:     err != nil || IsResultError(result),
				ParamKeys: extractParamKeys(req),
			})
			return result, err
		}
	}
}
