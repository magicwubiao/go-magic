package server

import (
	"context"
	"strings"
	"testing"
)

// TestPersistAssistantMessageRejectsReasoningOnly 锁死"只有思考、没有回答"的
// assistant 消息不得落库。
//
// 事故背景（10-04，DeepSeek 长会话"反复询问用户"）：reasoning 模型在部分轮次
// 把产出全放在 reasoning_content、content 留空，流式 handler 产出的
// fullResponse 就变成一段未闭合的 "<think>User wants to ..."（实测线上某会话
// 49 条 assistant 中 29 条如此）。旧实现只判 TrimSpace 非空即落库，于是历史
// 堆满"只有思考没有结论"的消息——模型下一轮读到自己残缺的历史只能重新推导、
// 重新确认，表现为同一个简单任务反复询问、几十轮停不下来。
func TestPersistAssistantMessageRejectsReasoningOnly(t *testing.T) {
	const sid = "sess-reasoning-only"
	s, _, store := guideDupTestServer(t, sid)

	reasoningOnly := []struct {
		name string
		text string
	}{
		{"unclosed think only (utf8 reasoning)", "<think>User wants to retag v0.5.22 to latest commit and push.<think>PowerShell doesn't support `&&`."},
		{"closed think, no body", "<think>Let me check git status first.</think>\n"},
		{"think with only whitespace body", "<think>planning</think>\n   \n\t"},
	}
	for _, tc := range reasoningOnly {
		t.Run(tc.name, func(t *testing.T) {
			s.persistAssistantMessage(sid, tc.text, tc.text, nil, true)

			sess, err := store.LoadSession(context.Background(), sid)
			if err != nil {
				t.Fatalf("LoadSession: %v", err)
			}
			for _, m := range sess.Messages {
				if m.Role == "assistant" {
					t.Fatalf("reasoning-only 内容不该落库，却存进了 assistant: %q", m.Content)
				}
			}
		})
	}
}

// TestPersistAssistantMessageKeepsThinkWithBody 确保收紧后带正文的回合照常落库，
// 且 <think> 原样保留（UI 的 ReasoningContent 组件依赖它渲染思考过程）。
func TestPersistAssistantMessageKeepsThinkWithBody(t *testing.T) {
	const sid = "sess-think-with-body"
	s, _, store := guideDupTestServer(t, sid)

	full := "<think>User wants to retag v0.5.22.</think>\n标签已重新打好并推送完成。"
	s.persistAssistantMessage(sid, full, full, nil, true)

	sess, err := store.LoadSession(context.Background(), sid)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	var got string
	for _, m := range sess.Messages {
		if m.Role == "assistant" {
			got = m.Content
		}
	}
	if got != full {
		t.Fatalf("带正文的回合必须原样落库（含 <think>），got %q want %q", got, full)
	}
	if !strings.Contains(got, "标签已重新打好并推送完成。") {
		t.Fatal("正文丢失")
	}
}
