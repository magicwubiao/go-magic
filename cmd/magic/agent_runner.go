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

	for turn := 0; turn < subAgentMaxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("subagent aborted after %d turn(s): %w", turn, err)
		}

		resp, err := toolCaller.ChatWithTools(ctx, messages, r.toolsSchema)
		if err != nil {
			return "", fmt.Errorf("provider chat failed (turn %d): %w", turn, err)
		}
		if resp == nil {
			return "", fmt.Errorf("empty response from provider (turn %d)", turn)
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
