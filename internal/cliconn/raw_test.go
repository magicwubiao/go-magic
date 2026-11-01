package cliconn

import (
	"context"
	"strings"
	"testing"
)

// raw 逃生工具的测试：执行、审批语义、保留名校验、输入上限。
// 子进程复用 cliconn_test.go 的 TestHelperProcess（同一 exec 路径跨平台）。

var _ approvalAwareShape = (*RawTool)(nil)

func TestRawToolExecutesArbitraryArgs(t *testing.T) {
	conn := helperConnector("echo_args")
	tool := NewRawTool("gh", conn)

	out, err := tool.Execute(context.Background(), map[string]any{
		"args": []any{"pr", "list", "b c", "--x=1"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	if got := m["stdout"].(string); !strings.HasSuffix(got, "|pr|list|b c|--x=1") {
		t.Fatalf("stdout = %q", got)
	}
	// 回显的 command 以连接器命令开头，且包含用户参数
	if cmd := m["command"].(string); !strings.Contains(cmd, "b c") {
		t.Fatalf("command echo = %q", cmd)
	}
}

func TestRawToolAlwaysRequiresApproval(t *testing.T) {
	// read_only 与 require_approval 都对 raw 无效：任意子命令意味着没人能
	// 事前断言它只读，审批是它存在的安全前提。
	off := false
	conn := helperConnector("echo_args")
	conn.RequireApproval = &off
	tool := NewRawTool("gh", conn)
	if !tool.RequiresApproval() {
		t.Fatal("raw tool must always require approval")
	}
	if tool.ApprovalSummary() == "" {
		t.Fatal("approval summary must not be empty")
	}
}

func TestRawToolStdin(t *testing.T) {
	conn := helperConnector("echo_stdin")
	tool := NewRawTool("x", conn)

	out, err := tool.Execute(context.Background(), map[string]any{
		"args":  []any{},
		"stdin": "hello raw stdin",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	if got := m["stdout"].(string); got != "hello raw stdin" {
		t.Fatalf("stdout = %q", got)
	}
	if m["stdin_bytes"].(int) != len("hello raw stdin") {
		t.Fatalf("stdin_bytes = %v", m["stdin_bytes"])
	}
}

func TestRawToolEnvAndMasking(t *testing.T) {
	conn := helperConnector("echo_env")
	conn.Env = append(conn.Env, "RAW_SECRET=topsecret", "CLICONN_HELPER_ENV=RAW_SECRET")
	tool := NewRawTool("x", conn)

	out, err := tool.Execute(context.Background(), map[string]any{"args": []any{"x"}})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m := out.(map[string]any)
	if got := m["stdout"].(string); got != "topsecret" {
		t.Fatalf("env not injected into raw subprocess: %q", got)
	}
	// 凭据不得出现在回显的 command 里
	if cmd := m["command"].(string); strings.Contains(cmd, "topsecret") {
		t.Fatalf("credential leaked into command echo: %q", cmd)
	}
}

func TestRawToolParamCoercion(t *testing.T) {
	argv, _, err := parseRawParams(map[string]any{
		"args": []any{"--limit", float64(20), true},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{"--limit", "20", "true"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v", argv)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, argv[i], want[i])
		}
	}

	// 嵌套结构不是标量：直接报错，而不是被静默序列化
	if _, _, err := parseRawParams(map[string]any{
		"args": []any{map[string]any{"a": 1}},
	}); err == nil {
		t.Fatal("object arg should be rejected")
	}
	// stdin 超限
	if _, _, err := parseRawParams(map[string]any{
		"args":  []any{},
		"stdin": strings.Repeat("a", MaxRawStdinBytes+1),
	}); err == nil {
		t.Fatal("oversized stdin should be rejected")
	}
}

func TestRawToolTooManyArgs(t *testing.T) {
	conn := helperConnector("echo_args")
	tool := NewRawTool("x", conn)
	args := make([]any, MaxRawArgs+1)
	for i := range args {
		args[i] = "a"
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"args": args}); err == nil {
		t.Fatal("too many args should be rejected")
	}
}

func TestValidateReservesRawToolName(t *testing.T) {
	conn := Connector{
		Command: "gh",
		Raw:     true,
		Tools: []ToolSpec{{
			Name:        RawToolName,
			Description: "clash",
			Args:        []string{"x"},
		}},
	}
	if err := conn.Validate(); err == nil {
		t.Fatal("declared tool named raw must be rejected when raw is enabled")
	}
}

func TestRawConnectorNeedsNoDeclaredTools(t *testing.T) {
	conn := Connector{Command: "gh", Raw: true}
	if err := conn.Validate(); err != nil {
		t.Fatalf("raw-only connector should validate: %v", err)
	}
	// 没开 raw 的仍然要求至少一个工具
	if err := (Connector{Command: "gh"}).Validate(); err == nil {
		t.Fatal("connector without raw and without tools must be rejected")
	}

	m := New(map[string]Connector{"gh": conn})
	tools := m.ToolsForProfile("")
	if len(tools) != 1 || tools[0].Name() != "cli_gh_raw" {
		t.Fatalf("raw-only connector should register exactly cli_gh_raw, got %v", tools)
	}
}

func TestRawToolSchemaAndDescription(t *testing.T) {
	conn := Connector{Command: "gh", Description: "GitHub CLI", Raw: true}
	tool := NewRawTool("gh", conn)

	schema := tool.Schema()
	props := schema["properties"].(map[string]any)
	if _, ok := props["args"]; !ok {
		t.Fatal("schema must have args")
	}
	if _, ok := props["stdin"]; !ok {
		t.Fatal("schema must have stdin")
	}
	if req := schema["required"].([]string); len(req) != 1 || req[0] != "args" {
		t.Fatalf("required = %v", req)
	}
	if d := tool.Description(); !strings.Contains(d, "gh") {
		t.Fatalf("description should mention the command: %q", d)
	}
	if name := tool.Name(); name != "cli_gh_raw" {
		t.Fatalf("name = %q", name)
	}
}
