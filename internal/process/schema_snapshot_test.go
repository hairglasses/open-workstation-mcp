package process

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
)

func TestInvestigatePort_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "investigate_port" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			return
		}
	}
	t.Fatalf("investigate_port not found in module tools")
}

func TestInvestigateService_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "investigate_service" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			return
		}
	}
	t.Fatalf("investigate_service not found in module tools")
}

func TestInvestigateMemory_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "investigate_memory" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			if td.Category != "composed" {
				t.Errorf("expected Category composed, got %s", td.Category)
			}
			return
		}
	}
	t.Fatalf("investigate_memory not found in module tools")
}

func TestInvestigateCpu_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "investigate_cpu" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			if td.Category != "composed" {
				t.Errorf("expected Category composed, got %s", td.Category)
			}
			return
		}
	}
	t.Fatalf("investigate_cpu not found in module tools")
}

func TestProcInspect_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "proc_inspect" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			if td.Category != "inspection" {
				t.Errorf("expected Category inspection, got %s", td.Category)
			}
			return
		}
	}
	t.Fatalf("proc_inspect not found in module tools")
}

func TestLivenessGate_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "liveness_gate" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			if td.Category != "safety" {
				t.Errorf("expected Category safety, got %s", td.Category)
			}
			return
		}
	}
	t.Fatalf("liveness_gate not found in module tools")
}

func TestDbusProbe_SchemaSnapshot(t *testing.T) {
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		if td.Tool.Name == "dbus_probe" {
			if td.MaxResultChars != 9000 {
				t.Errorf("expected MaxResultChars 9000, got %d", td.MaxResultChars)
			}
			if td.Category != "inspection" {
				t.Errorf("expected Category inspection, got %s", td.Category)
			}
			return
		}
	}
	t.Fatalf("dbus_probe not found in module tools")
}

func schemaAdvisories(node any, path string) []string {
	var issues []string
	m, ok := node.(map[string]any)
	if !ok {
		if arr, ok := node.([]any); ok {
			for i, item := range arr {
				issues = append(issues, schemaAdvisories(item, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
		return issues
	}
	if typ, _ := m["type"].(string); typ == "object" {
		props, _ := m["properties"].(map[string]any)
		if len(props) == 0 {
			issues = append(issues, fmt.Sprintf("%s: object missing properties", path))
		}
	} else if typ == "array" {
		if _, hasItems := m["items"]; !hasItems {
			issues = append(issues, fmt.Sprintf("%s: array missing items", path))
		}
	}
	for _, key := range []string{"properties", "$defs", "definitions"} {
		if sub, ok := m[key].(map[string]any); ok {
			for k, v := range sub {
				issues = append(issues, schemaAdvisories(v, path+"."+k)...)
			}
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		if sub, ok := m[key].(map[string]any); ok {
			issues = append(issues, schemaAdvisories(sub, path+"."+key)...)
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := m[key].([]any); ok {
			for i, v := range arr {
				issues = append(issues, schemaAdvisories(v, fmt.Sprintf("%s.%s[%d]", path, key, i))...)
			}
		}
	}
	return issues
}

func TestAllToolSchemasHaveNonEmptyObjectProperties(t *testing.T) {
	if got := schemaAdvisories(map[string]any{"type": "object"}, "."); len(got) == 0 {
		t.Fatal("negative control failed: expected advisory for empty object schema")
	}
	m := &ProcessModule{}
	for _, td := range m.Tools() {
		raw, err := json.Marshal(td.Tool)
		if err != nil {
			t.Fatalf("marshal %s: %v", td.Tool.Name, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("unmarshal %s: %v", td.Tool.Name, err)
		}
		inputSchema := doc["inputSchema"]
		if inputSchema == nil {
			t.Fatalf("%s missing inputSchema", td.Tool.Name)
		}
		if issues := schemaAdvisories(inputSchema, td.Tool.Name); len(issues) > 0 {
			t.Fatalf("%s schema advisories: %v", td.Tool.Name, issues)
		}
	}
}

func TestSystemInfo_IncludeCGroupOption(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("system_info reads /proc which requires Linux")
	}
	td := findTool(t, "system_info")

	resDefault, err := td.Handler(context.Background(), makeReq(map[string]any{}))
	if err != nil || resDefault.IsError {
		t.Fatalf("system_info with empty args failed: err=%v out=%v", err, resDefault)
	}
	var outDefault SystemInfoOutput
	unmarshalResult(t, resDefault, &outDefault)
	if outDefault.Hostname == "" {
		t.Fatal("expected non-empty hostname for empty args")
	}

	resNoCGroup, err := td.Handler(context.Background(), makeReq(map[string]any{"include_cgroup": false}))
	if err != nil || resNoCGroup.IsError {
		t.Fatalf("system_info with include_cgroup=false failed: err=%v out=%v", err, resNoCGroup)
	}
	var outNoCGroup SystemInfoOutput
	unmarshalResult(t, resNoCGroup, &outNoCGroup)
	if outNoCGroup.CGroup != "" {
		t.Fatalf("expected empty CGroup when include_cgroup=false, got %q", outNoCGroup.CGroup)
	}
}

func TestGpuStatus_HandlerInputOptions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub requires POSIX")
	}
	binDir := t.TempDir()
	scriptPath := filepath.Join(binDir, "nvidia-smi")
	script := "#!/bin/sh\ncase \"$1\" in\n  --query-gpu*) echo \"550.00, Mock GPU, 40, 10, 512, 8192, 45.5\" ;;\n  --query-compute-apps*) echo \"1234, sample-worker, 256\" ;;\nesac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake nvidia-smi: %v", err)
	}
	t.Setenv("PATH", binDir)

	var gpuTd *registry.ToolDefinition
	for _, td := range (&ProcessModule{}).Tools() {
		if td.Tool.Name == "gpu_status" {
			gpuTd = &td
			break
		}
	}
	if gpuTd == nil {
		t.Fatal("gpu_status tool not found")
	}

	resDefault, err := gpuTd.Handler(context.Background(), makeReq(map[string]any{}))
	if err != nil || resDefault.IsError {
		t.Fatalf("gpu_status default failed: err=%v out=%v", err, resDefault)
	}
	var outDefault GpuStatusOutput
	unmarshalResult(t, resDefault, &outDefault)
	if outDefault.GPU == nil || len(outDefault.Processes) != 1 || outDefault.Processes[0].PID != 1234 {
		t.Fatalf("unexpected default gpu_status output: %+v", outDefault)
	}

	resNoProcs, err := gpuTd.Handler(context.Background(), makeReq(map[string]any{"include_processes": false}))
	if err != nil || resNoProcs.IsError {
		t.Fatalf("gpu_status include_processes=false failed: err=%v out=%v", err, resNoProcs)
	}
	var outNoProcs GpuStatusOutput
	unmarshalResult(t, resNoProcs, &outNoProcs)
	if outNoProcs.GPU == nil || len(outNoProcs.Processes) != 0 {
		t.Fatalf("expected 0 processes when include_processes=false, got %+v", outNoProcs)
	}
}
