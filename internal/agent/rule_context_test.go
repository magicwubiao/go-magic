package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// writeTestRule 在临时目录写入规则文件。
func writeTestRule(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// newRuleTestAgent 构造只带 history 的裸 Agent（不经 provider），用于验证
// 出站消息组装阶段的规则/记忆注入语义。
func newRuleTestAgent(history []provider.Message) *Agent {
	return &Agent{history: history}
}

// TestWithContextBlocksOrderAndReload 验证出站注入顺序
// （base system → 规则 → 动态记忆）与规则文件变更后的自动重载。
func TestWithContextBlocksOrderAndReload(t *testing.T) {
	root := t.TempDir()
	agentsFile := filepath.Join(root, "AGENTS.md")
	writeTestRule(t, agentsFile, "v1: always run tests")

	base := "You are a test assistant."
	history := []provider.Message{
		{Role: "system", Content: base},
		{Role: "user", Content: "hello"},
	}
	a := newRuleTestAgent(history)
	a.dynamicMemory = "user prefers coffee"
	a.SetRuleDir(root)

	out := a.buildLLMMessages()
	roles := make([]string, 0, len(out))
	for _, m := range out {
		roles = append(roles, m.Role)
	}
	// system(system+system) 允许连续：base、规则、记忆 三个 system 在最前
	if len(out) < 4 || out[0].Role != "system" || out[1].Role != "system" || out[2].Role != "system" || out[3].Role != "user" {
		t.Fatalf("unexpected structure: roles=%v", roles)
	}
	if !strings.Contains(out[0].Content, base) {
		t.Errorf("out[0] should be base system prompt")
	}
	if !strings.Contains(out[1].Content, "v1: always run tests") {
		t.Errorf("out[1] should carry rule file content, got: %q", out[1].Content)
	}
	if !strings.Contains(out[2].Content, "user prefers coffee") {
		t.Errorf("out[2] should carry dynamic memory, got: %q", out[2].Content)
	}

	// 无规则/记忆时原样返回（长度不增加）
	a.ruleDir = ""
	a.ruleContext = ""
	a.dynamicMemory = ""
	plain := a.buildLLMMessages()
	if len(plain) != 2 {
		t.Fatalf("expected unchanged message count, got %d", len(plain))
	}

	// 规则文件变更 → 同一 agent 下一轮自动重载新内容
	a.SetRuleDir(root)
	writeTestRule(t, agentsFile, "v2: also run lint")
	out2 := a.buildLLMMessages()
	if !strings.Contains(out2[1].Content, "v2: also run lint") {
		t.Errorf("rule file edit should auto-reload, got: %q", out2[1].Content)
	}
}

// TestWithContextBlocksNoSystemHead 验证无 system 头部时规则/记忆前置。
func TestWithContextBlocksNoSystemHead(t *testing.T) {
	root := t.TempDir()
	writeTestRule(t, filepath.Join(root, "AGENTS.md"), "rule content")
	a := newRuleTestAgent([]provider.Message{{Role: "user", Content: "hi"}})
	a.dynamicMemory = "mem"
	a.SetRuleDir(root)

	out := a.buildLLMMessages()
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	if !strings.Contains(out[0].Content, "rule content") {
		t.Errorf("rules should be prepended first, got %q", out[0].Content)
	}
	if out[1].Content != "mem" {
		t.Errorf("memory should follow rules, got %q", out[1].Content)
	}
	if out[2].Role != "user" {
		t.Errorf("user message must remain last, got role %s", out[2].Role)
	}
}
