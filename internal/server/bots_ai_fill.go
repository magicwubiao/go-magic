package server

// AI 填写（ai-fill）：用户新建 Bot 时只需一句话描述用途，由 LLM 生成
// title / description / system_prompt / avatar 等表单字段回填给前端。
// 生成的结果仅供参考，用户在弹窗里确认/修改后仍走既有 POST /api/bots
// 保存——本接口不落盘，与 Bot 存储完全解耦。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// aiFillTimeout 是一次生成调用的硬超时。表单填写是交互式操作，
// 用户在前端盯着 loading，太长会让人以为挂了。
const aiFillTimeout = 60 * time.Second

// aiFillMaxBodyBytes 限制请求体大小（idea 只是几句话）。
const aiFillMaxBodyBytes = 64 << 10

// botAIFillRequest 是 POST /api/bots/ai-fill 的请求体。
type botAIFillRequest struct {
	// Name 是可选的 Bot 名称（用户可能先填了名称），给模型当命名线索。
	Name string `json:"name"`
	// Idea 是用户对 Bot 用途的一句话描述，必填。
	Idea string `json:"idea"`
}

// botAIFillResult 是返回给前端的生成结果，字段与创建表单一一对应。
type botAIFillResult struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	SystemPrompt string `json:"system_prompt"`
	Avatar       string `json:"avatar"`
}

// aiFillSystemPrompt 要求模型只输出一个 JSON 对象，字段固定。
// 语言跟随用户输入；avatar 优先给单个 emoji。
const aiFillSystemPrompt = `You are a configuration assistant for a chat-bot platform. The user describes a bot in one sentence; you fill in its creation form.
Respond with ONLY a JSON object (no markdown fences, no commentary) with exactly these keys:
{
  "title": "short role label, 2-8 words",
  "description": "one sentence describing what this bot does and when to use it",
  "system_prompt": "a concise persona prompt (100-300 words): personality, expertise, tone, and standing instructions. Written in second person ('You are ...')",
  "avatar": "a single emoji that fits the bot's role, or empty string"
}
Write title/description/system_prompt in the SAME language as the user's description (default to Chinese if ambiguous). Do not invent tool or skill names.`

// handleBotAIFill POST /api/bots/ai-fill — generate bot form fields via LLM.
// 注册为精确路径（比 /api/bots/ 子树更长匹配优先），不会被名为
// "ai-fill" 的 Bot 路由拦截；且本接口不访问 bot store，无需 manager。
func (s *Server) handleBotAIFill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.provider == nil {
		http.Error(w, "no LLM provider configured", http.StatusServiceUnavailable)
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, aiFillMaxBodyBytes))
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	var req botAIFillRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	idea := strings.TrimSpace(req.Idea)
	if idea == "" {
		http.Error(w, "idea is required", http.StatusBadRequest)
		return
	}

	userMsg := fmt.Sprintf("Bot description: %s", idea)
	if name := strings.TrimSpace(req.Name); name != "" {
		userMsg += fmt.Sprintf("\nBot name (already chosen): %s", name)
	}

	ctx, cancel := context.WithTimeout(r.Context(), aiFillTimeout)
	defer cancel()
	resp, err := s.provider.Chat(ctx, []types.Message{
		{Role: "system", Content: aiFillSystemPrompt},
		{Role: "user", Content: userMsg},
	})
	if err != nil {
		http.Error(w, "AI generation failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		http.Error(w, "AI returned an empty response", http.StatusBadGateway)
		return
	}

	result, err := parseAIFillJSON(resp.Content)
	if err != nil {
		http.Error(w, "failed to parse AI response: "+err.Error(), http.StatusBadGateway)
		return
	}
	// 兜底：模型偶尔会把用途塞进 title 而留空 description。
	if strings.TrimSpace(result.Description) == "" && strings.TrimSpace(result.Title) != "" {
		result.Description = result.Title
	}
	jsonResponse(w, result)
}

// parseAIFillJSON 从模型回复中提取 JSON 对象（容忍代码围栏和前后闲话）。
func parseAIFillJSON(content string) (*botAIFillResult, error) {
	text := strings.TrimSpace(content)
	// 常见的 ```json ... ``` 围栏直接剥掉。
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		if idx := strings.LastIndex(text, "```"); idx >= 0 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	var result botAIFillResult
	if err := json.Unmarshal([]byte(text[start:end+1]), &result); err != nil {
		return nil, err
	}
	return &result, nil
}
