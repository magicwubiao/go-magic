package compress

import (
	"fmt"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// TestCompressProtectsRecentTurnsProportionally 锁定"尾部保护量随历史长度放大"。
//
// 固定保护 4 条在长历史里太小：一轮并行读 4 个文件就产生 1 条 assistant(tool_calls)
// 加 4 条 tool 结果共 5 条消息，4 条的窗口刚好吃掉最早那份结果，模型为了比对只能
// 重读，重读又撑爆阈值再次压缩 —— "读完就忘"的正反馈死循环（2026-10-08 事故：
// 改导航栏的任务空转 30 分钟、283 次调用、86% 只读、写入仅 1 次）。
//
// 这里构造 40 条历史，断言倒数第 5 条（旧的固定 4 条窗口吃得掉、新的 len/4 窗口
// 保得住）在压缩后仍然存在。
func TestCompressProtectsRecentTurnsProportionally(t *testing.T) {
	c := NewCompressor(100) // 阈值极小，确保一定触发压缩
	c.ProtectLastN = 4      // 基准值保持默认；放大由 Compress 内部完成

	messages := make([]Message, 0, 40)
	for i := 0; i < 40; i++ {
		messages = append(messages, Message{
			Role:    "user",
			Content: fmt.Sprintf("MSG-%02d 用于撑开压缩窗口的历史内容 %s", i, strings.Repeat("x", 40)),
		})
	}

	result, err := c.Compress(messages, "sys")
	if err != nil {
		t.Fatal(err)
	}

	const marker = "MSG-35" // 倒数第 5 条
	kept := false
	for _, m := range result.Messages {
		if strings.Contains(m.Content, marker) {
			kept = true
			break
		}
	}
	if !kept {
		t.Fatalf("最近 1/4 的历史应被原样保留（%s 丢失）：尾部保护窗口没有随历史长度放大", marker)
	}
}

// TestDeterministicSummaryKeepsObservations 锁定"压缩后模型仍知道自己读过什么"。
//
// 旧实现只把 "Tool: read_file" 记进摘要，连目标路径都没有，模型压缩后对"读过
// 什么、内容是什么"完全失忆，只能反复重读同一批文件。新实现要求摘要保留工具
// 结果的目标资源与正文头部。
func TestDeterministicSummaryKeepsObservations(t *testing.T) {
	c := NewCompressor(100)

	middle := []Message{
		{
			Role: "assistant",
			ToolCalls: []types.ToolCall{
				{ID: "call_1", Name: "read_file", Arguments: map[string]interface{}{"path": "docs.html"}},
			},
		},
		{
			Role:       "tool",
			Name:       "read_file",
			ToolCallID: "call_1",
			Content:    "<nav class=\"nav-links\">首页 文档 联系我们</nav>" + strings.Repeat("y", 3000),
		},
	}

	summary := c.buildDeterministicSummary(middle)

	if !strings.Contains(summary, "docs.html") {
		t.Fatalf("摘要丢失了工具结果的目标路径:\n%s", summary)
	}
	if !strings.Contains(summary, "<nav") {
		t.Fatalf("摘要丢失了工具结果的正文头部:\n%s", summary)
	}
}

// TestDeterministicSummaryTruncatesToolHead 确保正文头部不会无限膨胀：摘要里
// 每条工具结果最多保留 ToolResultHeadChars 个字符。
func TestDeterministicSummaryTruncatesToolHead(t *testing.T) {
	c := NewCompressor(100)
	c.ToolResultHeadChars = 100

	head := strings.Repeat("H", 100)
	middle := []Message{
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c1", Name: "read_file", Arguments: map[string]interface{}{"path": "big.html"}}}},
		{Role: "tool", Name: "read_file", ToolCallID: "c1", Content: head + strings.Repeat("T", 5000)},
	}

	summary := c.buildDeterministicSummary(middle)

	if !strings.Contains(summary, "big.html") {
		t.Fatalf("摘要丢失目标路径:\n%s", summary)
	}
	if strings.Contains(summary, strings.Repeat("T", 200)) {
		t.Fatalf("正文头部未被截断，摘要会失控膨胀")
	}
}

func TestToolCallTarget(t *testing.T) {
	cases := []struct {
		name string
		tc   types.ToolCall
		want string
	}{
		{"path", types.ToolCall{Arguments: map[string]interface{}{"path": "a/b.go"}}, "a/b.go"},
		{"file_path", types.ToolCall{Arguments: map[string]interface{}{"file_path": "c.html"}}, "c.html"},
		{"pattern", types.ToolCall{Arguments: map[string]interface{}{"pattern": "TODO"}}, "TODO"},
		{"unknown-key", types.ToolCall{Arguments: map[string]interface{}{"foo": 1}}, ""},
		{"empty", types.ToolCall{}, ""},
	}
	for _, tc := range cases {
		if got := toolCallTarget(tc.tc); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}
