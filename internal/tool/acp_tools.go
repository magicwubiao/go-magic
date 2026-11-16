package tool

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/acp"
)

// acp_tools.go —— 把外部 ACP agent 暴露成模型可调用的工具。
//
// 四条边界（都是刻意设的）：
//  1. **必经审批**：acp_prompt 会让一个外部编码 agent 在自己的工作目录里
//     读改文件、跑命令，这比 register 一个 MCP 工具危险得多，所以
//     RequiresApproval 恒 true，摘要里带上 agent 名与实际提示词。
//  2. **输出截断**：agent 的一轮回答动辄上万字，原样回传会把模型上下文
//     吃穿；按 rune 截断（CJK 安全）到 8K 字符并显式标注。
//  3. **没配就隐身**：acp.json 不存在或全部 disabled 时 Available() 为 false，
//     工具不出现在清单里，避免模型对着必然失败的工具空转。
//  4. **图片有上限**：附带的本地图片按 base64 内联进 JSON-RPC，一张 4K 截图
//     就是几 MB，不设上限会把 stdio 帧和模型上下文一起打爆。

// acpMaxResponseChars 是单次回传给模型的正文上限（按字符计）。
const acpMaxResponseChars = 8192

const (
	// acpMaxImages 是单次 prompt 最多附带的图片数。
	acpMaxImages = 4
	// acpMaxImageBytes 是单张图片的大小上限（原始字节，不是 base64 后）。
	acpMaxImageBytes = 10 << 20
)

// acpImageMimeByExt 是允许的图片类型。只按扩展名判定：agent 侧不会自己嗅探，
// 声明错 MIME 比拒绝更糟（有的实现会按 MIME 决定解码器）。
var acpImageMimeByExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
}

// ACPTools 返回 ACP 工具集。注入 manager 便于测试；
// 生产路径用 Registry.RegisterACPTools() 拿进程级单例。
func ACPTools(mgr *acp.Manager) []Tool {
	return []Tool{
		&acpAgentsTool{BaseTool: newACPAgentsBase(), mgr: mgr},
		&acpPromptTool{BaseTool: newACPPromptBase(), mgr: mgr},
		&acpResetSessionTool{BaseTool: newACPResetBase(), mgr: mgr},
	}
}

func acpHasAgents(mgr *acp.Manager) bool {
	if mgr == nil {
		return false
	}
	agents, err := mgr.Agents()
	if err != nil {
		return false
	}
	for _, a := range agents {
		if a.Enabled {
			return true
		}
	}
	return false
}

// ---------- acp_agents ----------

type acpAgentsTool struct {
	*BaseTool
	mgr *acp.Manager
}

func newACPAgentsBase() *BaseTool {
	return NewBaseTool(
		"acp_agents",
		"List external ACP (Agent Client Protocol) agents configured on this machine. "+
			"Use this before acp_prompt to learn which agents exist and what they are for.",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	)
}

func (t *acpAgentsTool) Available() bool { return acpHasAgents(t.mgr) }

func (t *acpAgentsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agents, err := t.mgr.Agents()
	if err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, 0, len(agents))
	for _, a := range agents {
		if !a.Enabled {
			continue
		}
		item := map[string]interface{}{
			"name":      a.Name,
			"command":   a.Command,
			"connected": a.Connected,
		}
		if a.Description != "" {
			item["description"] = a.Description
		}
		if a.Workspace != "" {
			item["workspace"] = a.Workspace
		}
		out = append(out, item)
	}

	return map[string]interface{}{
		"agents": out,
		"count":  len(out),
		"config": t.mgr.ConfigPath(),
	}, nil
}

// ---------- acp_prompt ----------

type acpPromptTool struct {
	*BaseTool
	mgr *acp.Manager
}

func newACPPromptBase() *BaseTool {
	return NewBaseTool(
		"acp_prompt",
		"Send a prompt to an external ACP agent (e.g. a coding agent such as Claude Code or Gemini CLI) "+
			"and wait for the turn to finish. The agent runs its own tools in its own workspace; "+
			"its final answer is returned as text, with a compact list of the tool calls it made. "+
			"Conversation context persists across calls for the same agent — use acp_reset_session to start over. "+
			"Attach local images with `images` (absolute paths); the agent must declare image prompt support.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent": map[string]interface{}{
					"type":        "string",
					"description": "Agent name from acp_agents",
				},
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "Message to send to the agent",
				},
				"images": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "string",
					},
					"description": fmt.Sprintf(
						"Absolute paths of local image files (png/jpeg/webp/gif/bmp) to attach, "+
							"up to %d files and %d MB each. Use for screenshots, diagrams or UI mockups "+
							"the agent needs to see.",
						acpMaxImages, acpMaxImageBytes>>20),
				},
				"reset": map[string]interface{}{
					"type":        "boolean",
					"description": "Start a fresh session (drop previous context) before sending",
				},
			},
			"required": []string{"agent", "prompt"},
		},
	)
}

func (t *acpPromptTool) Available() bool { return acpHasAgents(t.mgr) }

// Timeout 声明执行预算：编码 agent 的一轮常常几分钟，
// 沿用 60s 默认值会把正常任务判成超时。
func (t *acpPromptTool) Timeout() time.Duration { return MaxTimeout }

// RequiresApproval 恒 true：外部 agent 能读写工作区、跑命令，
// 与 video_generate 同级，必须人工确认。
func (t *acpPromptTool) RequiresApproval() bool { return true }

func (t *acpPromptTool) ApprovalSummary() string { return "Run external ACP agent" }

func (t *acpPromptTool) ApprovalSummaryWith(args map[string]interface{}) string {
	agent, _ := args["agent"].(string)
	prompt, _ := args["prompt"].(string)
	if agent == "" {
		return t.ApprovalSummary()
	}
	summary := fmt.Sprintf("ACP agent %s ← %s", agent, truncateRunes(strings.TrimSpace(prompt), 80))
	if n := len(acpArgStrings(args["images"])); n > 0 {
		summary += fmt.Sprintf(" (+%d image(s))", n)
	}
	return summary
}

func (t *acpPromptTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agent, _ := args["agent"].(string)
	if strings.TrimSpace(agent) == "" {
		return nil, fmt.Errorf("missing required parameter: agent")
	}
	prompt, _ := args["prompt"].(string)
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("missing required parameter: prompt")
	}

	if reset, _ := args["reset"].(bool); reset {
		if err := t.mgr.ResetSession(ctx, agent); err != nil {
			return nil, err
		}
	}

	// 先拿连接：未知 agent 在这里就报错（不会先读文件再失败），
	// 同时能用上 initialize 协商出的能力来判断图片是否可发。
	client, err := t.mgr.Client(ctx, agent)
	if err != nil {
		return nil, err
	}

	blocks := []acp.ContentBlock{acp.TextBlock(prompt)}
	images, err := acpImageBlocks(client, agent, args["images"])
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, images...)

	outcome, err := t.mgr.Prompt(ctx, agent, blocks, nil)
	if err != nil {
		return nil, err
	}

	text := outcome.Text
	truncated := false
	if len([]rune(text)) > acpMaxResponseChars {
		text = truncateRunes(text, acpMaxResponseChars)
		truncated = true
	}

	result := map[string]interface{}{
		"agent":       agent,
		"stop_reason": outcome.StopReason,
		"response":    text,
		"cancelled":   outcome.Cancelled,
	}
	if truncated {
		result["truncated"] = true
		result["note"] = fmt.Sprintf("response truncated to %d characters", acpMaxResponseChars)
	}
	if len(images) > 0 {
		result["images"] = len(images)
	}

	tools := make([]map[string]interface{}, 0, len(outcome.ToolCalls))
	for _, tc := range outcome.ToolCalls {
		item := map[string]interface{}{"id": tc.ID}
		if tc.Title != "" {
			item["title"] = tc.Title
		}
		if tc.Name != "" {
			item["name"] = tc.Name
		}
		if tc.Kind != "" {
			item["kind"] = tc.Kind
		}
		if tc.Status != "" {
			item["status"] = tc.Status
		}
		tools = append(tools, item)
	}
	if len(tools) > 0 {
		result["tool_calls"] = tools
	}

	if text == "" && outcome.Thought != "" {
		// 只输出了思考内容（某些 agent 在被取消时会这样），别让模型拿到空字符串。
		result["response"] = truncateRunes(outcome.Thought, acpMaxResponseChars)
		result["response_source"] = "thought"
	}
	return result, nil
}

// ---------- acp_reset_session ----------

type acpResetSessionTool struct {
	*BaseTool
	mgr *acp.Manager
}

func newACPResetBase() *BaseTool {
	return NewBaseTool(
		"acp_reset_session",
		"Discard the conversation context of an ACP agent and start a fresh session. "+
			"Use when switching to an unrelated task or when previous context confuses the agent.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent": map[string]interface{}{
					"type":        "string",
					"description": "Agent name from acp_agents",
				},
			},
			"required": []string{"agent"},
		},
	)
}

func (t *acpResetSessionTool) Available() bool { return acpHasAgents(t.mgr) }

func (t *acpResetSessionTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agent, _ := args["agent"].(string)
	if strings.TrimSpace(agent) == "" {
		return nil, fmt.Errorf("missing required parameter: agent")
	}
	if err := t.mgr.ResetSession(ctx, agent); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"agent":   agent,
		"reset":   true,
		"message": "new session started",
	}, nil
}

// acpImageBlocks 把本地图片路径转成 image 内容块。
//
// 读盘前先看 agent 的能力：没声明 image 支持的实现在收到 image 块时行为不一
// （有的直接报错、有的静默丢掉），后者会让模型以为"图已经看到了"。
// 这里提前失败，并把"该 agent 不支持图片"说清楚。
func acpImageBlocks(client *acp.Client, agent string, raw interface{}) ([]acp.ContentBlock, error) {
	paths := acpArgStrings(raw)
	if len(paths) == 0 {
		return nil, nil
	}
	if len(paths) > acpMaxImages {
		return nil, fmt.Errorf("too many images (%d), limit is %d", len(paths), acpMaxImages)
	}

	caps := client.Info().AgentCapabilities.PromptCapabilities
	if !caps.Image {
		return nil, fmt.Errorf(
			"agent %q does not declare image prompt support; remove `images` or use a vision-capable agent",
			agent)
	}

	blocks := make([]acp.ContentBlock, 0, len(paths))
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("image path must be absolute: %s", path)
		}
		mime, ok := acpImageMimeByExt[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil, fmt.Errorf("unsupported image type %q: %s", filepath.Ext(path), path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("read image %s: %w", path, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("image path is a directory: %s", path)
		}
		if info.Size() > acpMaxImageBytes {
			return nil, fmt.Errorf("image too large (%d bytes, limit %d): %s", info.Size(), acpMaxImageBytes, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read image %s: %w", path, err)
		}
		blocks = append(blocks, acp.ImageBlock(base64.StdEncoding.EncodeToString(data), mime))
	}
	return blocks, nil
}

// acpArgStrings 把 JSON 数组参数转成字符串切片。
// 模型传进来的是 []interface{}，单测里可能是 []string，两种都接受。
func acpArgStrings(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// RegisterACPTools 注册 ACP 工具（用进程级 manager）。
func (r *Registry) RegisterACPTools() {
	for _, tool := range ACPTools(acp.Default()) {
		r.Register(tool)
	}
}
