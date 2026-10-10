package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAnthropicBuildRequestMergesSystemMessages 锁死 system 消息的合并语义。
//
// 旧实现 `systemPrompt = msg.Content` 在循环里逐条**覆盖**，Anthropic 请求里只
// 剩最后一条 system 消息。而 agent 侧会注入多段 system（基础 prompt / 规则链 /
// 工作目录 / 快照记忆 / 动态记忆 / 在案计划）—— 对 Anthropic 用户等于基础 system
// prompt 从未下发过，模型只看到最后一段注入块。
func TestAnthropicBuildRequestMergesSystemMessages(t *testing.T) {
	p := NewAnthropicProvider("k", "claude-sonnet-5")
	msgs := []Message{
		{Role: "system", Content: "BASE PROMPT"},
		{Role: "system", Content: "[Workspace] cwd=/x"},
		{Role: "system", Content: "[Active Plan] todo_x pending"},
		{Role: "user", Content: "hi"},
		{Role: "tool", ToolCallID: "t1", Content: "file contents"},
	}

	req := p.buildRequest(msgs, nil, false)

	blocks, ok := req.System.([]anthropicTextBlock)
	if !ok || len(blocks) != 1 {
		t.Fatalf("system must be a single merged text block, got %#v", req.System)
	}
	for _, want := range []string{"BASE PROMPT", "[Workspace] cwd=/x", "[Active Plan] todo_x pending"} {
		if !strings.Contains(blocks[0].Text, want) {
			t.Fatalf("merged system must contain %q, got: %s", want, blocks[0].Text)
		}
	}
	// 顺序必须与输入一致（稳定前缀在前）。
	if i, j := strings.Index(blocks[0].Text, "BASE PROMPT"), strings.Index(blocks[0].Text, "[Active Plan]"); i > j {
		t.Fatalf("system blocks must keep input order, got: %s", blocks[0].Text)
	}

	// 系统块上必须有缓存断点。
	if blocks[0].CacheControl == nil || blocks[0].CacheControl.Type != anthropicCacheControlEphemeral {
		t.Fatalf("system block must carry an ephemeral cache_control breakpoint, got %#v", blocks[0].CacheControl)
	}

	// 最后一条工具结果也要有断点（滚动缓存）。
	last := req.Messages[len(req.Messages)-1]
	tb, ok := last.Content.([]anthropicTextBlock)
	if !ok || len(tb) != 1 {
		t.Fatalf("last tool result must be emitted as a text block, got %#v", last.Content)
	}
	if tb[0].Text != "file contents" {
		t.Fatalf("tool result text mismatch: %q", tb[0].Text)
	}
	if tb[0].CacheControl == nil || tb[0].CacheControl.Type != anthropicCacheControlEphemeral {
		t.Fatalf("last tool result must carry an ephemeral cache_control breakpoint, got %#v", tb[0].CacheControl)
	}

	// 非最后一条的工具结果不应被打断点（每请求最多 4 个，别浪费）。
	// 这里再造一份带两条 tool 消息的请求验证。
	msgs2 := []Message{
		{Role: "system", Content: "S"},
		{Role: "tool", ToolCallID: "t1", Content: "first"},
		{Role: "tool", ToolCallID: "t2", Content: "second"},
	}
	req2 := p.buildRequest(msgs2, nil, false)
	if c, ok := req2.Messages[0].Content.([]anthropicTextBlock); ok && len(c) == 1 && c[0].CacheControl != nil {
		t.Fatalf("non-final tool result must NOT carry a cache breakpoint")
	}
	if c, ok := req2.Messages[1].Content.([]anthropicTextBlock); !ok || c[0].CacheControl == nil {
		t.Fatalf("final tool result must carry a cache breakpoint")
	}
}

// TestAnthropicRequestJSONShape 保证序列化形态确实是 Anthropic 期望的
// system 文本块数组 + 内联 cache_control，而不是裸字符串。
func TestAnthropicRequestJSONShape(t *testing.T) {
	p := NewAnthropicProvider("k", "claude-sonnet-5")
	req := p.buildRequest([]Message{
		{Role: "system", Content: "SYS"},
		{Role: "user", Content: "hi"},
	}, nil, false)

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"system":[{"type":"text","text":"SYS","cache_control":{"type":"ephemeral"}}]`) {
		t.Fatalf("unexpected system JSON shape: %s", got)
	}
}

// TestAnthropicNoSystemOmitsField 无 system 消息时不应发出空 system 字段。
func TestAnthropicNoSystemOmitsField(t *testing.T) {
	p := NewAnthropicProvider("k", "claude-sonnet-5")
	req := p.buildRequest([]Message{{Role: "user", Content: "hi"}}, nil, false)
	if req.System != nil {
		t.Fatalf("system must be nil when no system message present, got %#v", req.System)
	}
	raw, _ := json.Marshal(req)
	if strings.Contains(string(raw), `"system"`) {
		t.Fatalf("empty system must be omitted from JSON, got: %s", raw)
	}
}
