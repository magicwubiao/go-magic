package cliconn

import (
	"context"
	"strings"
	"testing"
	"time"
)

func compactFixture() Connector {
	return Connector{
		Command:     "gh",
		Description: "GitHub CLI",
		Tools: []ToolSpec{
			{
				Name:        "pr_list",
				Description: "List pull requests of a repository",
				Args:        []string{"pr", "list", "--repo", "{{repo}}", "--limit", "{{limit}}"},
				ReadOnly:    true,
				Params: []ParamSpec{
					{Name: "repo", Description: "owner/name", Required: true},
					{Name: "limit", Type: "integer", Default: 20},
				},
			},
			{
				Name:        "pr_view",
				Description: "View one pull request",
				Args:        []string{"pr", "view", "{{number}}"},
				ReadOnly:    true,
				Params:      []ParamSpec{{Name: "number", Type: "integer", Required: true}},
			},
		},
	}
}

func TestCompactToolsCollapseIntoOne(t *testing.T) {
	conn := compactFixture()
	conn.Compact = true
	if err := conn.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	m := New(map[string]Connector{"gh": conn})
	tools := m.ToolsForProfile("")
	if len(tools) != 1 {
		names := make([]string, 0, len(tools))
		for _, tl := range tools {
			names = append(names, tl.Name())
		}
		t.Fatalf("compact mode should register 1 tool, got %d: %v", len(tools), names)
	}
	if got := tools[0].Name(); got != "cli_gh_call" {
		t.Fatalf("tool name = %q, want cli_gh_call", got)
	}

	// 非紧凑模式仍然逐条展开，证明这只是 opt-in。
	plain := New(map[string]Connector{"gh": compactFixture()})
	if got := len(plain.ToolsForProfile("")); got != 2 {
		t.Fatalf("non-compact connector should register 2 tools, got %d", got)
	}
}

func TestCompactToolSchemaAndDescription(t *testing.T) {
	conn := compactFixture()
	conn.Compact = true
	tool := NewCompactTool("gh", conn)

	schema := tool.Schema()
	props, _ := schema["properties"].(map[string]interface{})
	toolProp, _ := props["tool"].(map[string]interface{})
	enum, _ := toolProp["enum"].([]string)
	if len(enum) != 2 || enum[0] != "pr_list" || enum[1] != "pr_view" {
		t.Fatalf("enum = %v, want declared tool names in order", enum)
	}
	if _, ok := props["params"]; !ok {
		t.Fatal("schema must expose a free-form params object")
	}

	desc := tool.Description()
	for _, want := range []string{
		"pr_list(repo*, limit:integer)",
		"pr_view(number:integer*)",
		"List pull requests",
		"Runs: gh <tool>",
		"Read-only and pre-approved",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q:\n%s", want, desc)
		}
	}
}

func TestCompactToolExecuteDispatches(t *testing.T) {
	conn := helperConnector("echo_args")
	conn.Compact = true
	conn.Tools = []ToolSpec{
		{
			Name:        "echo",
			Description: "Echo the payload",
			Args:        []string{"{{payload}}"},
			ReadOnly:    true,
			Params:      []ParamSpec{{Name: "payload", Required: true}},
		},
	}

	tool := NewCompactTool("helper", conn)
	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"tool":   "echo",
		"params": map[string]interface{}{"payload": "hello world"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	res, _ := out.(map[string]interface{})
	// helper 会把全部 argv 用 | 拼起来（含测试二进制的入口参数），
	// 这里只关心工具参数是否原样到达。
	if got, _ := res["stdout"].(string); !strings.HasSuffix(got, "|hello world") {
		t.Fatalf("stdout = %q", got)
	}

	// 未知子命令要报出可用清单，而不是让模型猜。
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"tool": "nope"}); err == nil ||
		!strings.Contains(err.Error(), "available: echo") {
		t.Fatalf("unknown tool error = %v", err)
	}

	// 必填校验仍然生效（只是从 schema 挪到了执行时）。
	if _, err := tool.Execute(context.Background(), map[string]interface{}{
		"tool":   "echo",
		"params": map[string]interface{}{},
	}); err == nil || !strings.Contains(err.Error(), "missing required parameter") {
		t.Fatalf("missing required error = %v", err)
	}

	if _, err := tool.Execute(context.Background(), map[string]interface{}{
		"tool":   "echo",
		"params": "not-an-object",
	}); err == nil {
		t.Fatal("non-object params should be rejected")
	}
}

func TestCompactToolApprovalIsConservative(t *testing.T) {
	readOnly := compactFixture()
	readOnly.Compact = true
	if NewCompactTool("gh", readOnly).RequiresApproval() {
		t.Fatal("all-read-only connector should stay pre-approved in compact mode")
	}

	mixed := compactFixture()
	mixed.Compact = true
	mixed.Tools = append(mixed.Tools, ToolSpec{
		Name: "repo_delete", Description: "Delete a repository",
		Args:   []string{"repo", "delete", "{{repo}}"},
		Params: []ParamSpec{{Name: "repo", Required: true}},
	})
	ct := NewCompactTool("gh", mixed)
	if !ct.RequiresApproval() {
		t.Fatal("one write tool must make the whole dispatcher require approval")
	}

	// 连接器级 require_approval 优先，与逐条工具时同一套判定。
	forced := compactFixture()
	forced.Compact = true
	no := false
	forced.RequireApproval = &no
	if NewCompactTool("gh", forced).RequiresApproval() {
		t.Fatal("connector-level require_approval=false should win")
	}
}

func TestCompactToolApprovalSummaryShowsCommand(t *testing.T) {
	conn := compactFixture()
	conn.Compact = true
	tool := NewCompactTool("gh", conn)

	got := tool.ApprovalSummaryWith(map[string]interface{}{
		"tool":   "pr_list",
		"params": map[string]interface{}{"repo": "owner/name"},
	})
	if !strings.HasPrefix(got, "gh pr list --repo owner/name --limit 20") {
		t.Fatalf("summary = %q, want the real command", got)
	}

	// 参数不合法时退回静态摘要 + 选中的子命令，仍然比空白强。
	fallback := tool.ApprovalSummaryWith(map[string]interface{}{"tool": "pr_list"})
	if !strings.Contains(fallback, "[tool=pr_list]") {
		t.Fatalf("fallback = %q", fallback)
	}
}

func TestCompactToolTimeoutUsesLongest(t *testing.T) {
	conn := Connector{
		Command:        "gh",
		TimeoutSeconds: 30,
		Tools: []ToolSpec{
			{Name: "quick", Description: "d", Args: []string{"a"}},
			{Name: "slow", Description: "d", Args: []string{"b"}, TimeoutSeconds: 240},
		},
		Compact: true,
	}
	if got := NewCompactTool("gh", conn).Timeout(); got != 240*time.Second {
		t.Fatalf("timeout = %v, want the longest declared tool timeout", got)
	}
}

func TestCompactValidation(t *testing.T) {
	reserved := compactFixture()
	reserved.Compact = true
	reserved.Tools[0].Name = CompactToolName
	if err := reserved.Validate(); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("tool named %q should be rejected, got %v", CompactToolName, err)
	}

	empty := Connector{Command: "gh", Compact: true, Raw: true}
	if err := empty.Validate(); err == nil || !strings.Contains(err.Error(), "compact_tools requires") {
		t.Fatalf("compact without tools should be rejected, got %v", err)
	}
}
