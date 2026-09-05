package provider

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// AnthropicProvider implements the Anthropic Claude API
type AnthropicProvider struct {
	apiKey        string
	model         string
	client        *http.Client
	ConvertConfig *ConvertConfig
}

// SetConvertConfig implements ConvertConfigProvider
func (p *AnthropicProvider) SetConvertConfig(config *ConvertConfig) {
	p.ConvertConfig = config
}

// GetConvertConfig implements ConvertConfigProvider
func (p *AnthropicProvider) GetConvertConfig() *ConvertConfig {
	return p.ConvertConfig
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if model == "" {
		model = "claude-sonnet-5" // Default to balanced model
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// GetCapabilities returns the capabilities of Anthropic
func (p *AnthropicProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:    true,
		Streaming:      true,
		StreamingTools: true,
		MultiModal:     true,
		Vision:         true,
	}
}

// anthropicMessage represents Anthropic's message format
type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []interface{} for multi-modal
}

// anthropicRequest represents Anthropic's chat request
type anthropicRequest struct {
	Model         string               `json:"model"`
	Messages      []anthropicMessage   `json:"messages"`
	SystemPrompt  string               `json:"system,omitempty"`
	MaxTokens     int                  `json:"max_tokens"`
	Tools         []anthropicToolDef   `json:"tools,omitempty"`
	ToolChoice    *anthropicToolChoice `json:"tool_choice,omitempty"`
	Stream        bool                 `json:"stream,omitempty"`
	Temperature   *float64             `json:"temperature,omitempty"`
	TopP          *float64             `json:"top_p,omitempty"`
	StopSequences []string             `json:"stop_sequences,omitempty"`
}

// anthropicToolDef represents Anthropic's tool definition
type anthropicToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// anthropicToolChoice represents tool choice options
type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// anthropicResponse represents Anthropic's response
type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type      string                 `json:"type"`
		Text      string                 `json:"text,omitempty"`
		ID        string                 `json:"id,omitempty"`
		Name      string                 `json:"name,omitempty"`
		Input     map[string]interface{} `json:"input,omitempty"`
		ToolUseID string                 `json:"tool_use_id,omitempty"`
		Content   string                 `json:"content,omitempty"`
	} `json:"content"`
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence,omitempty"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Chat implements the Provider interface
func (p *AnthropicProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, nil, false)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-dangerous-direct-browser-access", "true")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, p.parseError(body, resp.StatusCode)
	}

	return p.parseResponse(body)
}

// ChatWithTools implements the ToolCaller interface
func (p *AnthropicProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, tools, false)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-dangerous-direct-browser-access", "true")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, p.parseError(body, resp.StatusCode)
	}

	return p.parseResponse(body)
}

// Stream implements the Streamer interface
func (p *AnthropicProvider) Stream(ctx context.Context, messages []Message, handler StreamHandler) error {
	reqBody := p.buildRequest(messages, nil, true)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	const maxRetries = 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", strings.NewReader(string(jsonBody)))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", p.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("anthropic-dangerous-direct-browser-access", "true")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := p.client.Do(req)
		if err != nil {
			if attempt < maxRetries {
				delay := time.Duration(1<<uint(attempt)) * time.Second
				log.Debugf("[Anthropic:Stream] Network error on attempt %d/%d, retrying after %v: %v",
					attempt+1, maxRetries, delay, err)
				select {
				case <-time.After(delay):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			apiErr := p.parseError(body, resp.StatusCode)

			if attempt < maxRetries && isRetryableStatus(resp.StatusCode) {
				delay := time.Duration(1<<uint(attempt)) * time.Second
				log.Debugf("[Anthropic:Stream] API error on attempt %d/%d (status %d), retrying after %v: %v",
					attempt+1, maxRetries, resp.StatusCode, delay, apiErr)
				select {
				case <-time.After(delay):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return apiErr
		}

		defer resp.Body.Close()
		return p.parseStreamResponse(resp.Body, handler)
	}

	return fmt.Errorf("max retries exceeded")
}

// StreamWithTools implements the StreamingToolCaller interface
func (p *AnthropicProvider) StreamWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}, handler StreamHandler) error {
	reqBody := p.buildRequest(messages, tools, true)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	const maxRetries = 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", strings.NewReader(string(jsonBody)))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", p.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("anthropic-dangerous-direct-browser-access", "true")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := p.client.Do(req)
		if err != nil {
			if attempt < maxRetries {
				delay := time.Duration(1<<uint(attempt)) * time.Second
				log.Debugf("[Anthropic:Stream] Network error on attempt %d/%d, retrying after %v: %v",
					attempt+1, maxRetries, delay, err)
				select {
				case <-time.After(delay):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			apiErr := p.parseError(body, resp.StatusCode)

			if attempt < maxRetries && isRetryableStatus(resp.StatusCode) {
				delay := time.Duration(1<<uint(attempt)) * time.Second
				log.Debugf("[Anthropic:Stream] API error on attempt %d/%d (status %d), retrying after %v: %v",
					attempt+1, maxRetries, resp.StatusCode, delay, apiErr)
				select {
				case <-time.After(delay):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return apiErr
		}

		defer resp.Body.Close()
		return p.parseStreamResponse(resp.Body, handler)
	}

	return fmt.Errorf("max retries exceeded")
}

// buildRequest builds an Anthropic API request from messages
func (p *AnthropicProvider) buildRequest(messages []Message, tools []map[string]interface{}, stream bool) *anthropicRequest {
	var systemPrompt string
	var anthropicMessages []anthropicMessage

	// Separate system prompt from messages
	for _, msg := range messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
		} else {
			// Convert to Anthropic format
			role := msg.Role
			if role == "assistant" {
				role = "assistant"
			} else if role == "tool" {
				role = "user" // Anthropic doesn't have tool role, use user with tool result
			} else {
				role = "user"
			}

			// Handle ContentParts for multi-modal content
			var content interface{}
			if len(msg.ContentParts) > 0 {
				cfg := p.ConvertConfig.WithAutoVision(p.model)
				contentParts := ConvertContentPartsToMap(msg.ContentParts, cfg)
				// The shared converter emits OpenAI-style parts
				// ({"type":"image_url","image_url":{...}}). Anthropic's
				// native Messages API rejects those with a 400 — restructure
				// image parts into Anthropic's image/source blocks.
				contentParts = toAnthropicContentParts(contentParts)
				if len(contentParts) > 0 {
					content = contentParts
				} else {
					content = msg.Content
				}
			} else {
				content = msg.Content
			}

			anthropicMessages = append(anthropicMessages, anthropicMessage{
				Role:    role,
				Content: content,
			})
		}
	}

	req := &anthropicRequest{
		Model:        p.model,
		Messages:     anthropicMessages,
		SystemPrompt: systemPrompt,
		MaxTokens:    4096, // Required by Anthropic
		Stream:       stream,
	}

	// Convert tools (support both OpenAI nested format and flat format)
	if len(tools) > 0 {
		for _, tool := range tools {
			var name, description string
			var parameters map[string]interface{}

			// Check for OpenAI nested format: {"type": "function", "function": {"name": "...", "description": "...", "parameters": {...}}}
			if funcObj, ok := tool["function"].(map[string]interface{}); ok {
				name, _ = funcObj["name"].(string)
				description, _ = funcObj["description"].(string)
				parameters, _ = funcObj["parameters"].(map[string]interface{})
			} else {
				// Flat format: {"name": "...", "description": "...", "parameters": {...}}
				name, _ = tool["name"].(string)
				description, _ = tool["description"].(string)
				parameters, _ = tool["parameters"].(map[string]interface{})
			}

			if parameters == nil {
				parameters = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			req.Tools = append(req.Tools, anthropicToolDef{
				Name:        name,
				Description: description,
				InputSchema: parameters,
			})
		}
		req.ToolChoice = &anthropicToolChoice{Type: "auto"}
	}

	return req
}

// toAnthropicContentParts restructures OpenAI-style content parts into the
// format Anthropic's native Messages API expects:
//
//	{"type":"image_url","image_url":{"url":"data:image/png;base64,..."}}
//	  → {"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}
//
// Text parts pass through unchanged. Sending raw image_url blocks to the
// native endpoint is a guaranteed 400 ("image_url is not a valid content
// block type"). Non-data-URL images (http links) cannot be inlined — they
// degrade to a short text note so the turn still goes through.
func toAnthropicContentParts(parts []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(parts))
	for _, part := range parts {
		ptype, _ := part["type"].(string)
		if ptype != "image_url" {
			out = append(out, part)
			continue
		}
		urlMap, ok := part["image_url"].(map[string]interface{})
		if !ok {
			continue
		}
		urlStr, _ := urlMap["url"].(string)
		if !strings.HasPrefix(urlStr, "data:") {
			out = append(out, map[string]interface{}{
				"type": "text",
				"text": "(image attachment omitted: " + urlStr + " — not inlineable for Anthropic)",
			})
			continue
		}
		commaIdx := strings.Index(urlStr, ",")
		if commaIdx < 0 {
			out = append(out, map[string]interface{}{
				"type": "text",
				"text": "(image attachment omitted: empty image data)",
			})
			continue
		}
		header := urlStr[5:commaIdx] // e.g. "image/png;base64"
		data := urlStr[commaIdx+1:]
		mediaType := strings.TrimSuffix(header, ";base64")
		if mediaType == "" {
			mediaType = "image/png"
		}
		// Anthropic only accepts image/* media types in base64 sources.
		if !strings.HasPrefix(mediaType, "image/") {
			out = append(out, map[string]interface{}{
				"type": "text",
				"text": "(image attachment omitted: unsupported media type " + mediaType + ")",
			})
			continue
		}
		if _, err := base64.StdEncoding.DecodeString(data); err != nil {
			log.Warnf("[Anthropic] dropping image part with invalid base64 payload: %v", err)
			out = append(out, map[string]interface{}{
				"type": "text",
				"text": "(image attachment omitted: invalid image data)",
			})
			continue
		}
		out = append(out, map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": mediaType,
				"data":       data,
			},
		})
	}
	return out
}

// parseResponse parses a non-streaming Anthropic response
func (p *AnthropicProvider) parseResponse(body []byte) (*ChatResponse, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	response := &ChatResponse{
		Usage: &Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}

	for _, content := range resp.Content {
		switch content.Type {
		case "text":
			response.Content = content.Text
		case "thinking":
			// Extended thinking blocks from Claude 3.5+ thinking / Claude 4.
			// "content" field contains the raw reasoning text, "thinking"
			// signature block is in a different block (skipped here).
			if content.Content != "" {
				response.ReasoningContent = content.Content
			} else if content.Text != "" {
				response.ReasoningContent = content.Text
			}
		case "tool_use":
			// This is a tool call request
			args, _ := json.Marshal(content.Input)
			argsStr := string(args)
			tc := types.ToolCall{
				ID:   content.ID,
				Name: content.Name,
				Type: "function",
				Function: types.Function{
					Name:      content.Name,
					Arguments: argsStr,
				},
				Arguments: content.Input,
			}
			if tc.Arguments == nil {
				tc.Arguments = make(map[string]interface{})
				json.Unmarshal(args, &tc.Arguments)
			}
			response.ToolCalls = append(response.ToolCalls, tc)
		}
	}

	return response, nil
}

// parseStreamResponse parses a streaming Anthropic response
func (p *AnthropicProvider) parseStreamResponse(body io.Reader, handler StreamHandler) error {
	reader := bufio.NewReader(body)
	var fullContent strings.Builder
	var fullReasoning strings.Builder
	var toolCalls []types.ToolCall
	var currentToolCall *types.ToolCall
	var functionName strings.Builder
	var functionArgs strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("stream read error: %w", err)
		}

		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == "[DONE]" {
			continue
		}

		// Parse event type
		var event struct {
			Type    string `json:"type"`
			Index   int    `json:"index,omitempty"`
			Content []struct {
				Type  string                 `json:"type"`
				Text  string                 `json:"text,omitempty"`
				ID    string                 `json:"id,omitempty"`
				Name  string                 `json:"name,omitempty"`
				Input map[string]interface{} `json:"input,omitempty"`
			} `json:"content,omitempty"`
			Delta *struct {
				Type         string `json:"type"`
				Text         string `json:"text,omitempty"`
				Thinking     string `json:"thinking,omitempty"`
				PartialJson  string `json:"partial_json,omitempty"`
				Index        int    `json:"index,omitempty"`
				ContentBlock *struct {
					Type string `json:"type"`
					ID   string `json:"id,omitempty"`
					Name string `json:"name,omitempty"`
				} `json:"content_block,omitempty"`
			} `json:"delta,omitempty"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta == nil {
				continue
			}
			switch event.Delta.Type {
			case "text_delta":
				text := event.Delta.Text
				fullContent.WriteString(text)
				handler(&StreamResponse{
					Content: text,
					Done:    false,
				})
			case "thinking_delta":
				// Extended thinking: collect and forward as ReasoningContent
				// so the UI can render the <think> wrapper inline.
				var reasoning string
				if event.Delta.Thinking != "" {
					reasoning = event.Delta.Thinking
				} else if event.Delta.Text != "" {
					reasoning = event.Delta.Text
				}
				if reasoning != "" {
					fullReasoning.WriteString(reasoning)
					handler(&StreamResponse{
						ReasoningContent: reasoning,
						Done:             false,
					})
				}
			case "input_json_delta":
				if currentToolCall == nil {
					continue
				}
				functionArgs.WriteString(event.Delta.PartialJson)
			}

		case "content_block_start":
			if event.Index >= len(event.Content) {
				continue
			}
			content := event.Content[event.Index]
			if content.Type == "tool_use" {
				if currentToolCall != nil {
					// Finalize previous tool call
					var args map[string]interface{}
					argsStr := functionArgs.String()
					if argsStr != "" {
						json.Unmarshal([]byte(argsStr), &args)
					}
					currentToolCall.Arguments = args
					currentToolCall.Function.Arguments = argsStr
					toolCalls = append(toolCalls, *currentToolCall)
					functionName.Reset()
					functionArgs.Reset()
				}
				currentToolCall = &types.ToolCall{
					ID:   content.ID,
					Name: content.Name,
					Type: "function",
					Function: types.Function{
						Name: content.Name,
					},
				}
			}

		case "message_delta":
			// Message complete

		case "message_stop":
			// Finalize last tool call
			if currentToolCall != nil {
				var args map[string]interface{}
				argsStr := functionArgs.String()
				if argsStr != "" {
					json.Unmarshal([]byte(argsStr), &args)
				}
				currentToolCall.Arguments = args
				currentToolCall.Function.Arguments = argsStr
				toolCalls = append(toolCalls, *currentToolCall)
			}

			handler(&StreamResponse{
				Content:          fullContent.String(),
				ReasoningContent: fullReasoning.String(),
				ToolCalls:        toolCalls,
				Done:             true,
			})
		}
	}

	return nil
}

// parseError parses an Anthropic error response
func (p *AnthropicProvider) parseError(body []byte, statusCode int) error {
	var errResp struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Error   struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil {
		if errResp.Error.Message != "" {
			return fmt.Errorf("anthropic error [%s]: %s", errResp.Error.Type, errResp.Error.Message)
		}
		if errResp.Message != "" {
			return fmt.Errorf("anthropic error: %s", errResp.Message)
		}
	}

	return fmt.Errorf("anthropic api error (%d): %s", statusCode, string(body))
}

// isRetryableStatus checks if an HTTP status code indicates a transient error worth retrying
func isRetryableStatus(statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 529:
		return true
	default:
		return false
	}
}
