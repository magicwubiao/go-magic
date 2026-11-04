package cliconn

import (
	"strings"
	"testing"
)

func previewFixture() (Connector, ToolSpec) {
	return Connector{
			Command: "gh",
			Args:    []string{"--hostname", "github.com"},
			Env:     []string{"GH_TOKEN=ghp_0123456789abcdef", "REGION=cn-hangzhou"},
		}, ToolSpec{
			Name:        "pr_list",
			Description: "List pull requests",
			Args:        []string{"pr", "list", "--repo", "{{repo}}", "--limit", "{{limit}}", "--token", "{{env:GH_TOKEN}}"},
			Params: []ParamSpec{
				{Name: "repo", Required: true},
				{Name: "limit", Type: "integer", Default: 20},
			},
		}
}

// TestPreviewCommandMasksCredentials 是审批卡片的安全底线：卡片上可以出现
// 命令形状，但不能出现凭据。真实执行用真值（见 TestRunRedactsCredentialInArgv），
// 展示层必须是 ***。
func TestPreviewCommandMasksCredentials(t *testing.T) {
	conn, spec := previewFixture()
	got := PreviewCommand(conn, spec, map[string]any{"repo": "owner/name"})

	if !strings.Contains(got, "pr list --repo owner/name --limit 20") {
		t.Fatalf("preview = %q, want the expanded command", got)
	}
	if strings.Contains(got, "ghp_0123456789abcdef") {
		t.Fatalf("preview leaked the token: %q", got)
	}
	if !strings.Contains(got, MaskPlaceholder) {
		t.Fatalf("preview should mask the token, got %q", got)
	}
	// 非凭据的值要保留：审批时看得出打到哪个环境。
	if !strings.Contains(got, "github.com") {
		t.Fatalf("preview should keep non-secret args: %q", got)
	}
}

// TestPreviewCommandEnvRefsAndEnvFile 校验两种凭据通道的展示形态：
// {{env:}} 展开后脱敏，{{envfile:}} 只给一个标记（预览不落临时文件）。
func TestPreviewCommandEnvRefsAndEnvFile(t *testing.T) {
	conn := Connector{
		Command: "gcloud",
		Env:     []string{"ACCESS_TOKEN=${PREVIEW_TOKEN}", "SA_FILE=/tmp/key.json"},
	}
	t.Setenv("PREVIEW_TOKEN", "ya29.secret-value")

	spec := ToolSpec{
		Name:        "auth",
		Description: "d",
		Args:        []string{"auth", "activate-service-account", "--key-file", "{{envfile:SA_FILE}}"},
		Params:      nil,
	}
	got := PreviewCommand(conn, spec, nil)
	if !strings.Contains(got, "<secret-file:SA_FILE>") {
		t.Fatalf("envfile should be previewed as a marker, got %q", got)
	}
	if strings.Contains(got, "/tmp/") {
		t.Fatalf("preview must not materialize a real path: %q", got)
	}

	spec2 := ToolSpec{
		Name:        "call",
		Description: "d",
		Args:        []string{"api", "--access-token={{env:ACCESS_TOKEN}}"},
	}
	got2 := PreviewCommand(conn, spec2, nil)
	if !strings.Contains(got2, "api") || strings.Contains(got2, "ya29.secret-value") {
		t.Fatalf("env ref must be expanded then masked, got %q", got2)
	}
	if !strings.Contains(got2, MaskPlaceholder) {
		t.Fatalf("expected masked value, got %q", got2)
	}
}

// TestPreviewCommandFallsBackOnInvalidParams：参数不合法时返回空串，
// 让审批层回落到静态摘要——审批不该因为"模型少传一个必填项"而中断。
func TestPreviewCommandFallsBackOnInvalidParams(t *testing.T) {
	conn, spec := previewFixture()
	if got := PreviewCommand(conn, spec, map[string]any{}); got != "" {
		t.Fatalf("missing required param should yield empty preview, got %q", got)
	}
	if got := PreviewCommand(conn, spec, map[string]any{"repo": "o/r", "limit": "abc"}); got != "" {
		t.Fatalf("bad integer should yield empty preview, got %q", got)
	}
}

func TestPreviewRawCommand(t *testing.T) {
	conn := Connector{
		Command: "gh",
		Args:    []string{"--hostname", "{{env:HOST}}"},
		Env:     []string{"HOST=github.com"},
	}
	got := PreviewRawCommand(conn, map[string]any{
		"args": []any{"repo", "delete", "owner/name"},
		"cwd":  "/tmp/work",
	})
	if !strings.Contains(got, "gh --hostname github.com repo delete owner/name") {
		t.Fatalf("raw preview = %q", got)
	}
	if !strings.Contains(got, "cwd: /tmp/work") {
		t.Fatalf("raw preview should show cwd: %q", got)
	}
}

// TestApprovalSummaryWithShowsCommand 是 agent 审批卡片实际拿到的字符串：
// 就是那条命令本身（不带工具名前缀，否则审批层会把前缀当可执行文件解析）。
func TestApprovalSummaryWithShowsCommand(t *testing.T) {
	conn, spec := previewFixture()
	tool := NewTool("gh", conn, spec)
	got := tool.ApprovalSummaryWith(map[string]any{"repo": "owner/name"})
	if !strings.HasPrefix(got, "gh --hostname github.com pr list") {
		t.Fatalf("summary = %q, want the bare command", got)
	}

	// 展开失败时退回静态摘要（仍是一句人话，而不是空白）。
	fallback := tool.ApprovalSummaryWith(map[string]any{})
	if fallback != tool.ApprovalSummary() {
		t.Fatalf("fallback = %q, want %q", fallback, tool.ApprovalSummary())
	}

	raw := NewRawTool("gh", conn)
	rawSummary := raw.ApprovalSummaryWith(map[string]any{"args": []any{"api", "repos"}})
	if !strings.Contains(rawSummary, "gh --hostname github.com api repos") {
		t.Fatalf("raw summary = %q, want the argv", rawSummary)
	}
}
