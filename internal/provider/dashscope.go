package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// DashScopeProvider 阿里云DashScope (兼容OpenAI格式)
type DashScopeProvider struct {
	apiKey         string
	model          string
	baseURL        string
	enableThinking bool
	*BaseProvider
}

func NewDashScopeProvider(apiKey, baseURL, model string) *DashScopeProvider {
	if model == "" {
		model = "qwen-max"
	}
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	return &DashScopeProvider{
		apiKey:         apiKey,
		model:          model,
		baseURL:        baseURL,
		enableThinking: true,
		BaseProvider:   NewBaseProvider(baseURL),
	}
}

// SetThinking toggles the DashScope-wide enable_thinking extra body flag.
// Thinking models (QwQ, qwen3.8-max thinking-only variants) ignore this
// setting; non-thinking models need it explicitly enabled to emit
// reasoning_content chunks. Default is true so callers see the thinking
// process shown in the UI without extra configuration.
func (p *DashScopeProvider) SetThinking(v bool) { p.enableThinking = v }

func (p *DashScopeProvider) Name() string {
	return "dashscope"
}

// dashscopeDefaultTemperature 与 openai_compatible.applyExtraParams 的
// 默认值保持一致：显式温度降低服务端默认采样退化（重复思考循环）的概率。
const dashscopeDefaultTemperature = 0.7

// dashscopeExtra returns a map of provider-specific top-level request keys
// (enable_thinking / thinking_budget / preserve_thinking) that must be
// merged into every outbound request body.
func (p *DashScopeProvider) dashscopeExtra() map[string]interface{} {
	if !p.enableThinking {
		return nil
	}
	extra := make(map[string]interface{}, 2)
	extra["enable_thinking"] = true
	// preserve_thinking keeps the previous turn's reasoning_content in
	// context so follow-up reasoning stays coherent across tool loops.
	extra["preserve_thinking"] = true
	return extra
}

// prepareMessages trims turns and converts to DashScope payload format.
// Callers pass in the raw message list; prepareMessages is responsible for
// enforcing the 150-turn hard cap before conversion.
func (p *DashScopeProvider) prepareMessages(messages []Message) []map[string]interface{} {
	trimmed := TrimDashScopeTurns(messages)
	if len(trimmed) != len(messages) {
		log.Infof("[DashScope] trimmed turns: %d -> %d (assistant-msgs limit %d)",
			len(messages), len(trimmed), DashScopeTurnMaxSend)
	}
	return ConvertMessagesForProvider(trimmed, p.BaseProvider)
}

// buildRequestBody unifies request body construction for Chat/ChatWithTools
// and their streaming counterparts.
func (p *DashScopeProvider) buildRequestBody(messages []Message, tools []map[string]interface{}, stream bool) map[string]interface{} {
	reqBody := map[string]interface{}{
		"model":       p.model,
		"messages":    p.prepareMessages(messages),
		"temperature": dashscopeDefaultTemperature,
	}
	if stream {
		reqBody["stream"] = true
		reqBody["stream_options"] = map[string]interface{}{"include_usage": true}
		// DashScope compatible-mode streams require incremental_output=true
		// when enable_thinking is on; otherwise reasoning_content chunks
		// are not delivered incrementally through SSE.
		reqBody["incremental_output"] = true
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
		// Explicit parallel tool calls match DashScope default ("auto"
		// would be silently ignored anyway per openai_compatible).
		reqBody["parallel_tool_calls"] = true
	}
	for k, v := range p.dashscopeExtra() {
		reqBody[k] = v
	}
	return reqBody
}

// post performs the raw HTTP request common to streaming and non-streaming.
// The caller is responsible for closing resp.Body on success.
func (p *DashScopeProvider) post(ctx context.Context, body interface{}) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	return http.DefaultClient.Do(req)
}

// parseToolCallsFromMsg parses the DashScope "message.tool_calls" payload
// (same shape as OpenAI) into our internal types.ToolCall slice.
// The function name intentionally contains "FromMsg" (not the struct field
// "Message" we read below) — it's an alias to match call-site semantics.
func parseDashScopeToolCalls(src []struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}) []types.ToolCall {
	if len(src) == 0 {
		return nil
	}
	tcs := make([]types.ToolCall, 0, len(src))
	for _, tc := range src {
		var args map[string]interface{}
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		tcs = append(tcs, types.ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Type: "function",
			Function: types.Function{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
			Arguments: args,
		})
	}
	return tcs
}

// readDashScopeOK closes and decodes a 2xx non-streaming response body into
// a ChatResponse including reasoning_content. Falls back to common alternate
// names (thinking_content / reasoning / thinking) so DashScope-native
// wrappers and proxied endpoints still surface the thinking phase.
func readDashScopeOK(resp *http.Response) (*ChatResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// Typed decode for the well-known fields.
	var result struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *Usage `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("dashscope decode: %w (body=%s)", err, truncateBody(body))
	}
	// Raw decode used to probe for alternate reasoning-content names.
	var raw struct {
		Choices []struct {
			Message map[string]interface{} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(body, &raw)

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no response from dashscope (body=%s)", truncateBody(body))
	}
	msg := result.Choices[0].Message
	reasoning := msg.ReasoningContent
	if reasoning == "" && len(raw.Choices) > 0 && raw.Choices[0].Message != nil {
		for _, alt := range []string{"thinking_content", "reasoning", "thinking"} {
			if v, ok := raw.Choices[0].Message[alt].(string); ok && v != "" {
				reasoning = v
				break
			}
		}
	}
	response := &ChatResponse{
		Content:          msg.Content,
		ReasoningContent: reasoning,
		ToolCalls:        parseDashScopeToolCalls(msg.ToolCalls),
		Usage:            result.Usage,
	}
	return response, nil
}

func truncateBody(b []byte) string {
	if len(b) <= 512 {
		return string(b)
	}
	return string(b[:512]) + "...(truncated)"
}

// doNonStreaming runs a non-streaming request and converts any 4xx/5xx into
// a wrapped error string.
func (p *DashScopeProvider) doNonStreaming(ctx context.Context, body map[string]interface{}) (*ChatResponse, error) {
	resp, err := p.post(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("dashscope api error %d: %s", resp.StatusCode, string(b))
	}
	return readDashScopeOK(resp)
}

func (p *DashScopeProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	return p.doNonStreaming(ctx, p.buildRequestBody(messages, nil, false))
}

// ChatWithTools implements the ToolCaller interface for DashScope
func (p *DashScopeProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	return p.doNonStreaming(ctx, p.buildRequestBody(messages, tools, false))
}

// ---- Streaming (Stream + StreamWithTools) ---------------------------------

// Stream runs a plain chat stream without tools. DashScope supports tools
// through the same /chat/completions endpoint; this entry point exists to
// satisfy the provider.Streamer interface for callers that want just text.
func (p *DashScopeProvider) Stream(ctx context.Context, messages []Message, handler StreamHandler) error {
	return p.streamAny(ctx, messages, nil, handler)
}

// StreamWithTools implements provider.StreamingToolCaller. This is what the
// chat server checks (via type assertion) to decide whether to send real
// SSE deltas vs. the non-streaming "逐字" fallback.
func (p *DashScopeProvider) StreamWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}, handler StreamHandler) error {
	return p.streamAny(ctx, messages, tools, handler)
}

func (p *DashScopeProvider) streamAny(ctx context.Context, messages []Message, tools []map[string]interface{}, handler StreamHandler) error {
	body := p.buildRequestBody(messages, tools, true)
	resp, err := p.post(ctx, body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dashscope stream api error %d: %s", resp.StatusCode, string(b))
	}
	// OpenAIStreamParser already reads choices[0].delta.(content,
	// reasoning_content, tool_calls[index].*) which matches exactly what
	// DashScope's compatible-mode emits.
	return ParseStreamResponseWithTools(ctx, resp.Body, handler)
}
