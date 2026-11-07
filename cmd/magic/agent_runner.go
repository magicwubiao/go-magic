package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/subagent"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// subAgentMaxTurns 是子代理自身的工具循环上限。子代理拿到的应是边界清晰、
// 可独立完成的子任务，所以这里比主 agent 的 maxTurns 小得多：跑不完说明
// 任务切分粒度过大，应当快速失败并把问题交回主 agent 重新分解，而不是
// 让子代理消耗大量回合数去硬啃。
const subAgentMaxTurns = 20

// subAgentToolTimeout 是子代理单次工具调用的硬超时。子代理在 Manager 的
// 并发槽里运行，一个卡死的工具会占住整个槽位。
const subAgentToolTimeout = 120 * time.Second

// 中断抢救内容的规模上限：主 agent 的上下文有限，这里只留"够判断工作是否
// 已经落盘"的证据，不做完整回放。
const (
	salvageMaxNotes        = 2
	salvageNoteRunes       = 400
	salvageMaxToolResults  = 3
	salvageToolResultRunes = 1200
)

// simpleAgentRunner 是 subagent.AgentRunner 的实现：带完整工具循环的子代理。
//
// 早期实现只调一次 provider.Chat 就返回，既没把 toolsSchema 传给模型，也没有
// 工具执行循环——子代理因此拿不到任何工具，所谓"自主完成子任务"实际退化成
// 一次纯文本问答（工具调用请求会被静默丢弃，输出看起来像"我打算这么做"）。
type simpleAgentRunner struct {
	provider     provider.Provider
	registry     subagent.ToolRegistry
	toolsSchema  []map[string]interface{}
	systemPrompt string
}

// RunConversation 执行子代理的工具循环，直到模型给出最终答复或触达回合上限。
func (r *simpleAgentRunner) RunConversation(ctx context.Context, input string) (string, error) {
	messages := []provider.Message{
		{Role: "system", Content: r.systemPrompt},
		{Role: "user", Content: input},
	}

	// 不支持工具调用的 provider 退化为单次对话。这仍然比"调用带 tools 的
	// 重载然后丢弃工具请求"要好：不会让模型产生"我调用了工具"的错觉。
	toolCaller, canCallTools := r.provider.(provider.ToolCaller)
	if !canCallTools || len(r.toolsSchema) == 0 {
		resp, err := r.provider.Chat(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("provider chat failed: %w", err)
		}
		if resp == nil || strings.TrimSpace(resp.Content) == "" {
			return "", fmt.Errorf("no response from provider")
		}
		return resp.Content, nil
	}

	var lastContent string
	// toolNames 记下 tool_call_id → 工具名，用于中断时标注"这一步做了什么"。
	// provider 回喂的 tool 消息本身不含工具名。
	toolNames := make(map[string]string)
	toolCalls := 0

	for turn := 0; turn < subAgentMaxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			return salvageAbortedRun(messages, toolNames, lastContent, turn, toolCalls),
				fmt.Errorf("subagent aborted after %d turn(s): %w", turn, err)
		}

		resp, err := toolCaller.ChatWithTools(ctx, messages, r.toolsSchema)
		if err != nil {
			// 上游断流/超时最常见的就是"活全干完了、只差最后一段总结"。
			// 此时若只回一句错误，调用方拿不到任何"改动已落盘"的信号，
			// 重派一次就是白烧 token，还可能覆盖掉正确实现。
			return salvageAbortedRun(messages, toolNames, lastContent, turn, toolCalls),
				fmt.Errorf("provider chat failed (turn %d): %w", turn, err)
		}
		if resp == nil {
			return salvageAbortedRun(messages, toolNames, lastContent, turn, toolCalls),
				fmt.Errorf("empty response from provider (turn %d)", turn)
		}

		// 保留已有内容：模型可能在调用工具的同时输出一段说明文字。
		if trimmed := strings.TrimSpace(resp.Content); trimmed != "" {
			lastContent = resp.Content
		}

		// 没有工具调用 = 模型已给出最终答复，收工。
		if len(resp.ToolCalls) == 0 {
			if strings.TrimSpace(resp.Content) == "" {
				return "", fmt.Errorf("no response from provider")
			}
			return resp.Content, nil
		}

		// 把 assistant 的工具调用意图写回历史，否则下一次请求里那条
		// tool 消息会找不到对应的 tool_calls，被 provider 以 400 拒绝。
		messages = append(messages, provider.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			output := r.executeTool(ctx, tc)
			if name := tc.GetToolName(); name != "" && tc.ID != "" {
				toolNames[tc.ID] = name
			}
			toolCalls++
			messages = append(messages, provider.Message{
				Role:       "tool",
				Content:    output,
				ToolCallID: tc.ID,
			})
		}
	}

	// 触达回合上限：把最后的可见内容一并返回，避免调用方只拿到一句错误、
	// 丢失子代理已经产出的进度。
	if strings.TrimSpace(lastContent) != "" {
		return lastContent, fmt.Errorf("subagent exceeded maximum turns (%d)", subAgentMaxTurns)
	}
	return "", fmt.Errorf("subagent exceeded maximum turns (%d)", subAgentMaxTurns)
}

// salvageAbortedRun 在子代理因上游断流 / ctx 到期而中断时，拼出一段"中断前
// 已经发生了什么"的摘要，随错误一起返回给 Manager（最终出现在 poll_task 的
// partial_output 里）。
//
// 为什么需要它：任务状态 failed 不等于工作没做。实测过一个真实场景——子代理
// 已经改完源文件、写完表驱动测试、跑过 go test、执行了 git add -N，只在最后
// 生成回传摘要时被上游断掉。此时 output 为空，调用方只看 status 就会判定"白跑
// 了"，重派一遍既烧 token，又可能覆盖掉已经正确的实现。
//
// 因此这里不强求还原完整结果，只给出足以让调用方决定"先去看一眼工作区"的证据：
// 最近两轮助手的说明文字 + 最近几次工具调用的名称与输出片段。
// 完全没有内容可抢救时返回空串，避免伪造一段看似有产出的文本。
func salvageAbortedRun(messages []provider.Message, toolNames map[string]string, lastContent string, turns, toolCalls int) string {
	lastContent = strings.TrimSpace(lastContent)
	if lastContent == "" && toolCalls == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[aborted run salvage] turns=%d tool_calls=%d — upstream stream ended before the final answer.\n", turns, toolCalls)
	b.WriteString("NOTE: a failed task does NOT mean nothing happened. Files may already be modified on disk.\n")
	b.WriteString("Inspect the workspace (git status / git diff) before re-delegating or reverting; the work below may be complete.\n")

	if lastContent != "" {
		// 助手在每轮工具调用旁写的说明往往就是"我改了什么"的线索，
		// 取最近两条即可，再多就挤占工具输出的预算。
		var notes []string
		for _, msg := range messages {
			if msg.Role != "assistant" {
				continue
			}
			if c := strings.TrimSpace(msg.Content); c != "" {
				notes = append(notes, c)
			}
		}
		// lastContent 可能来自本轮尚未写回 messages 的响应，补齐但不重复。
		if len(notes) == 0 || notes[len(notes)-1] != lastContent {
			notes = append(notes, lastContent)
		}
		if len(notes) > salvageMaxNotes {
			notes = notes[len(notes)-salvageMaxNotes:]
		}
		fmt.Fprintf(&b, "\n--- last %d assistant note(s), oldest first ---\n", len(notes))
		for _, n := range notes {
			fmt.Fprintf(&b, "- %s\n", clipRunes(n, salvageNoteRunes))
		}
	}

	// 取最后 N 条工具输出，按时间正序呈现：越靠后越接近"最后做了什么"。
	type toolEntry struct{ name, output string }
	var entries []toolEntry
	for _, msg := range messages {
		if msg.Role != "tool" || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		name := toolNames[msg.ToolCallID]
		if name == "" {
			name = "unknown"
		}
		entries = append(entries, toolEntry{name: name, output: msg.Content})
	}
	if len(entries) > salvageMaxToolResults {
		entries = entries[len(entries)-salvageMaxToolResults:]
	}
	if len(entries) > 0 {
		fmt.Fprintf(&b, "\n--- last %d tool result(s), oldest first (truncated) ---\n", len(entries))
		for i, e := range entries {
			fmt.Fprintf(&b, "[%d] %s\n%s\n", i+1, e.name, clipRunes(strings.TrimSpace(e.output), salvageToolResultRunes))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// clipRunes 按 rune 截断并标注省略位置。
// 不能用 s[:n]：中文按字节切会把多字节字符劈成乱码。
func clipRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + fmt.Sprintf("\n…[truncated, original %d runes]", len(runes))
}

// executeTool 执行一次工具调用并把结果序列化为字符串。
// 工具自身的错误不向上抛：错误信息对模型同样是有价值的信息，让它有机会
// 换一种做法重试，而不是让整个子代理因一次工具报错而失败。
func (r *simpleAgentRunner) executeTool(ctx context.Context, tc types.ToolCall) string {
	name := tc.GetToolName()
	if name == "" {
		return "Error: tool call has no name"
	}

	t, err := r.registry.Get(name)
	if err != nil {
		return fmt.Sprintf("Error: tool %q not available", name)
	}

	// subagent.Tool 接口不含 Execute，但 cmd 侧注入的适配器（toolAdapter）
	// 底层持有真正可执行的工具，这里按方法集取回执行能力。
	executable, ok := t.(interface {
		Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
	})
	if !ok {
		return fmt.Sprintf("Error: tool %q is not executable", name)
	}

	params := tc.Arguments
	// 兼容部分 provider 把参数放在 Function.Arguments 里以 JSON 字符串传回。
	if len(params) == 0 && tc.Function.Arguments != "" {
		var parsed map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); jsonErr == nil {
			params = parsed
		} else {
			return fmt.Sprintf("Error: failed to parse arguments for tool %q: %v", name, jsonErr)
		}
	}

	toolCtx, cancel := context.WithTimeout(ctx, subAgentToolTimeout)
	defer cancel()

	result, err := executable.Execute(toolCtx, params)
	if err != nil {
		return fmt.Sprintf("Error executing %s: %v", name, err)
	}

	return formatToolResult(result)
}

// formatToolResult 把工具返回值转成适合喂给模型的文本。
// 字符串直接透传（避免被 JSON 编码后带上一层引号和转义），
// 其余类型走 JSON；无法序列化时退化为 fmt 输出。
func formatToolResult(result interface{}) string {
	if result == nil {
		return "(no output)"
	}
	if s, ok := result.(string); ok {
		if strings.TrimSpace(s) == "" {
			return "(no output)"
		}
		return s
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("%v", result)
	}
	return string(data)
}
