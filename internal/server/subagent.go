package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/subagent"
	"github.com/magicwubiao/go-magic/internal/tool"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// 本文件把 subagent 能力接入 Web 服务链路。
//
// 背景：delegate_task / poll_task / list_tasks / cancel_task 这四个委托工具原先
// 只在 CLI 的一次性任务路径（cmd/magic/agent.go）里注册过，Web / Bot 入口完全
// 没有接线。结果是超长任务在 Web 端只能在一个主循环里线性消耗回合数——没有
// 切分、没有并行，撞回合上限时也无法把子任务分摊出去。

// subAgentMaxTurns 是子代理自身的工具循环上限。
// 子代理拿到的应是边界清晰、可独立完成的子任务；跑不完说明切分粒度过大，
// 应当快速失败并把问题交回主 agent 重新分解，而不是让子代理硬啃。
const subAgentMaxTurns = 20

// subAgentToolTimeout 是子代理单次工具调用的硬超时。
// 子代理运行在 Manager 的并发槽里，一个卡死的工具会占住整个槽位。
const subAgentToolTimeout = 120 * time.Second

// subagentRegistryAdapter 把 *tool.Registry 适配为 subagent.ToolRegistry。
// 与 cmd/magic 下的同名适配器职责一致，但位于 server 包内：cmd 侧的类型
// 无法被 internal/server 复用（包边界），因此这里独立实现一份。
type subagentRegistryAdapter struct {
	registry *tool.Registry
}

func (a *subagentRegistryAdapter) List() []string {
	return a.registry.List()
}

func (a *subagentRegistryAdapter) Get(name string) (subagent.Tool, error) {
	t, err := a.registry.Get(name)
	if err != nil {
		return nil, err
	}
	return &subagentToolAdapter{tool: t}, nil
}

// subagentToolAdapter 把 tool.Tool 适配为 subagent.Tool。
// subagent.Tool 只声明元信息（Name/Description/Schema），但子代理的工具循环
// 必须真的能执行工具，所以额外暴露 Execute 供运行器按方法集取用。
type subagentToolAdapter struct {
	tool tool.Tool
}

func (a *subagentToolAdapter) Name() string { return a.tool.Name() }

func (a *subagentToolAdapter) Description() string { return a.tool.Description() }

func (a *subagentToolAdapter) Schema() map[string]interface{} { return a.tool.Schema() }

// Execute 供 subagentRunner 通过方法集断言调用。
func (a *subagentToolAdapter) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	return a.tool.Execute(ctx, params)
}

// subagentRunner 是带完整工具循环的子代理运行器（server 侧实现）。
type subagentRunner struct {
	provider     provider.Provider
	registry     subagent.ToolRegistry
	toolsSchema  []map[string]interface{}
	systemPrompt string
}

// RunConversation 执行子代理的工具循环，直到模型给出最终答复或触达回合上限。
func (r *subagentRunner) RunConversation(ctx context.Context, input string) (string, error) {
	messages := []provider.Message{
		{Role: "system", Content: r.systemPrompt},
		{Role: "user", Content: input},
	}

	// 不支持工具调用的 provider 退化为单次对话，避免让模型产生
	// "我调用了工具"的错觉（工具调用请求会被静默丢弃）。
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

		if trimmed := strings.TrimSpace(resp.Content); trimmed != "" {
			lastContent = resp.Content
		}

		// 没有工具调用 = 模型已给出最终答复。
		if len(resp.ToolCalls) == 0 {
			if strings.TrimSpace(resp.Content) == "" {
				return "", fmt.Errorf("no response from provider")
			}
			return resp.Content, nil
		}

		// 把 assistant 的工具调用意图写回历史：否则下一轮请求里那条 tool
		// 消息找不到对应的 tool_calls，会被 provider 以 400 拒绝。
		messages = append(messages, provider.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			messages = append(messages, provider.Message{
				Role:       "tool",
				Content:    r.executeTool(ctx, tc),
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

// executeTool 执行一次工具调用并序列化结果。工具自身的错误不向上抛：
// 错误信息对模型同样有价值，让它有机会换个做法，而不是让整个子代理失败。
func (r *subagentRunner) executeTool(ctx context.Context, tc types.ToolCall) string {
	name := tc.GetToolName()
	if name == "" {
		return "Error: tool call has no name"
	}

	t, err := r.registry.Get(name)
	if err != nil {
		return fmt.Sprintf("Error: tool %q not available", name)
	}

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

	return formatSubagentToolResult(result)
}

// formatSubagentToolResult 把工具返回值转成适合喂给模型的文本。
func formatSubagentToolResult(result interface{}) string {
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

// newSubagentRunner 是注入 subagent.Manager 的 AgentFactory。
// 每个子任务会用它产出一个独立的带工具循环的运行器。
func newSubagentRunner(
	p provider.Provider,
	reg subagent.ToolRegistry,
	toolsSchema []map[string]interface{},
	systemPrompt string,
) subagent.AgentRunner {
	return &subagentRunner{
		provider:     p,
		registry:     reg,
		toolsSchema:  toolsSchema,
		systemPrompt: systemPrompt,
	}
}

// buildSubagentConfig 从应用配置构造 subagent.Config，缺省项回落到默认值。
func buildSubagentConfig(cfg *appconfig.Config) *subagent.Config {
	subCfg := subagent.DefaultConfig()
	if cfg == nil || cfg.SubAgent == nil {
		return subCfg
	}
	if cfg.SubAgent.MaxConcurrent > 0 {
		subCfg.MaxConcurrent = cfg.SubAgent.MaxConcurrent
	}
	if cfg.SubAgent.MaxDepth > 0 {
		subCfg.MaxDepth = cfg.SubAgent.MaxDepth
	}
	if cfg.SubAgent.Timeout > 0 {
		subCfg.Timeout = cfg.SubAgent.Timeout
	}
	return subCfg
}
