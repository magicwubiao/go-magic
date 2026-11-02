package agent

import (
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/bus"
	"github.com/magicwubiao/go-magic/internal/provider"
)

// TestContextBreakdownCategories 校验上下文用量浮层的分类口径：
//   - 技能块嵌在 history[0] 的 system 消息里，必须从系统提示词里扣除单列
//     （否则技能被数两次）；
//   - mcp_/ap_ 前缀工具归"连接器及MCP"，其余工具归"工具及子智能体"；
//   - 注入块（规则链等，不写回 history）计入系统提示词；
//   - Total 等于各分类之和。
func TestContextBreakdownCategories(t *testing.T) {
	a := &Agent{bus: bus.NewEventBus()}
	skillsBlock := strings.Repeat("s", 4000) // 4000 字节 ≈ 1000 token
	sysBase := strings.Repeat("p", 8000)
	a.history = []provider.Message{
		{Role: "system", Content: sysBase + "\n\n" + skillsBlock},
		{Role: "user", Content: strings.Repeat("u", 4000)},
		{Role: "assistant", Content: strings.Repeat("a", 2000)},
	}
	a.skillsCtx = skillsBlock
	a.ruleContext = strings.Repeat("r", 400) // 注入块：只在字段里，不在 history
	a.tools = []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{
			"name": "read_file", "description": strings.Repeat("d", 800),
		}},
		{"type": "function", "function": map[string]interface{}{
			"name": "mcp_github_create_issue", "description": strings.Repeat("d", 1600),
		}},
	}

	b := a.ContextBreakdown()

	if b.Skills != 1000 {
		t.Errorf("Skills = %d, want 1000 (skillsCtx bytes/4)", b.Skills)
	}
	// system = (8000 base + 400 rule) / 4，技能块已从 system 里扣除
	if b.System != (len(sysBase)+len(a.ruleContext))/4 {
		t.Errorf("System = %d, want %d (base + rule blocks, skills excluded)", b.System, (len(sysBase)+len(a.ruleContext))/4)
	}
	if b.Messages != (4000+2000)/4 {
		t.Errorf("Messages = %d, want %d", b.Messages, (4000+2000)/4)
	}
	if b.Tools == 0 || b.MCP == 0 {
		t.Errorf("Tools = %d, MCP = %d, both should be > 0", b.Tools, b.MCP)
	}
	if b.ToolCount != 1 || b.MCPToolCount != 1 {
		t.Errorf("ToolCount = %d, MCPToolCount = %d, want 1/1", b.ToolCount, b.MCPToolCount)
	}
	if b.Total != b.System+b.Skills+b.Messages+b.Tools+b.MCP {
		t.Errorf("Total = %d, sum of parts = %d", b.Total, b.System+b.Skills+b.Messages+b.Tools+b.MCP)
	}
}

// TestContextBreakdownEmptySystem 校验空会话（无 history、无 agent 注入块）
// 不 panic 且各分类为 0 —— 新会话打开浮层是正常操作。
func TestContextBreakdownEmptySystem(t *testing.T) {
	a := &Agent{bus: bus.NewEventBus()}
	b := a.ContextBreakdown()
	if b.Total != 0 {
		t.Errorf("Total = %d, want 0 on empty agent", b.Total)
	}
}
