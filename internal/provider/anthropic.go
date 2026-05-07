package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	
	"github.com/magicwubiao/go-magic/pkg/types"
)

// AnthropicProvider implements the Anthropic Claude API
type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	if model == "" {
		model = "claude-3-5-haiku-20241022" // Default to cost-effective model
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
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      true,
		Vision:          true,
	}
}

// anthropicMessage represents Anthropic's message format
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicRequest represents Anthropic's chat request
type anthropicRequest struct {
	Model         string               `json:"model"`
	Messages      []anthropicMessage    `json:"messages"`
	SystemPrompt  string                `json:"system,omitempty"`
	MaxTokens     int                   `json:"max_tokens"`
	Tools         []anthropicToolDef    `json:"tools,omitempty"`
	ToolChoice    *anthropicToolChoice  `json:"tool_choice,omitempty"`
	Stream        bool                  `json:"stream,omitempty"`
	Temperature   *float64              `json:"temperature,omitempty"`
	TopP          *float64              `json:"top_p,omitempty"`
	StopSequences []string              `json:"stop_sequences,omitempty"`
}

// anthropicToolDef represents Anthropic's tool definition
type anthropicToolDef struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description,omitempty"`
	InputSchema map[string]interface{}     `json:"input_schema"`
}

// anthropicToolChoice represents tool choice options
type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// anthropicResponse represents Anthropic's response
type anthropicResponse struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Role         string `json:"role"`
	Content      []struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		ID          string `json:"id,omitempty"`
		Name        string `json:"name,omitempty"`
		Input       map[string]interface{} `json:"input,omitempty"`
		ToolUseID   string `json:"tool_use_id,omitempty"`
		Content      string `json:"content,omitempty"`
	} `json:"content"`
	StopReason  string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence,omitempty"`
	Usage       struct {
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
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return p.parseError(body, resp.StatusCode)
	}

	return p.parseStreamResponse(resp.Body, handler)
}

// StreamWithTools implements the StreamingToolCaller interface
func (p *AnthropicProvider) StreamWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}, handler StreamHandler) error {
	reqBody := p.buildRequest(messages, tools, true)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

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
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return p.parseError(body, resp.StatusCode)
	}

	return p.parseStreamResponse(resp.Body, handler)
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
			anthropicMessages = append(anthropicMessages, anthropicMessage{
				Role:    role,
				Content: msg.Content,
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

	// Convert tools
	if len(tools) > 0 {
		for _, tool := range tools {
			name, _ := tool["name"].(string)
			description, _ := tool["description"].(string)
			parameters, _ := tool["parameters"].(map[string]interface{})
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
		if content.Type == "text" {
			response.Content = content.Text
		} else if content.Type == "tool_use" {
			// This is a tool call request
			args, _ := json.Marshal(content.Input)
			tc := types.ToolCall{
				ID:   content.ID,
				Name: content.Name,
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
			Type            string `json:"type"`
			Index           int    `json:"index,omitempty"`
			Content         []struct {
				Type   string `json:"type"`
				Text   string `json:"text,omitempty"`
				ID     string `json:"id,omitempty"`
				Name   string `json:"name,omitempty"`
				Input  map[string]interface{} `json:"input,omitempty"`
			} `json:"content,omitempty"`
			Delta           *struct {
				Type          string `json:"type"`
				Text          string `json:"text,omitempty"`
				PartialJson   string `json:"partial_json,omitempty"`
				Index         int    `json:"index,omitempty"`
				ContentBlock  *struct {
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
				// Skip thinking blocks for now
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
					json.Unmarshal([]byte(functionArgs.String()), &args)
					currentToolCall.Arguments = args
					toolCalls = append(toolCalls, *currentToolCall)
					functionName.Reset()
					functionArgs.Reset()
				}
				currentToolCall = &types.ToolCall{
					ID:   content.ID,
					Name: content.Name,
				}
			}

		case "message_delta":
			// Message complete

		case "message_stop":
			// Finalize last tool call
			if currentToolCall != nil {
				var args map[string]interface{}
				if functionArgs.Len() > 0 {
					json.Unmarshal([]byte(functionArgs.String()), &args)
				}
				currentToolCall.Arguments = args
				toolCalls = append(toolCalls, *currentToolCall)
			}

			handler(&StreamResponse{
				Content:   fullContent.String(),
				ToolCalls: toolCalls,
				Done:      true,
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
