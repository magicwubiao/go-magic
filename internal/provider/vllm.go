package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type vLLMProvider struct {
	baseURL       string
	model         string
	ConvertConfig *ConvertConfig
}

// SetConvertConfig implements ConvertConfigProvider
func (p *vLLMProvider) SetConvertConfig(config *ConvertConfig) {
	p.ConvertConfig = config
}

// GetConvertConfig implements ConvertConfigProvider
func (p *vLLMProvider) GetConvertConfig() *ConvertConfig {
	return p.ConvertConfig
}

// vllmUsageInfo represents token usage from vLLM API
type vllmUsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewVLLMProvider(baseURL, model string) *vLLMProvider {
	if baseURL == "" {
		baseURL = "http://localhost:8000/v1"
	}
	if model == "" {
		model = "default"
	}
	return &vLLMProvider{
		baseURL: baseURL,
		model:   model,
	}
}

func (p *vLLMProvider) Name() string {
	return "vllm"
}

func (p *vLLMProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	// Convert messages to OpenAI format
	type ChatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	chatMessages := make([]ChatMessage, 0)
	for _, msg := range messages {
		chatMessages = append(chatMessages, ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Build request
	type Request struct {
		Model    string        `json:"model"`
		Messages []ChatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
	}

	reqBody := Request{
		Model:    p.model,
		Messages: chatMessages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// vLLM is OpenAI-compatible. Reuse the shared parser which handles
	// reasoning_content and well-known aliases (thinking_content /
	// reasoning / thinking) — critical for running local thinking models
	// (Qwen3.x, DeepSeek-R1, GLM thinking, etc.) behind vLLM which all
	// emit thinking via one of those field names.
	return parseTypedChatResponse(body)
}

// ChatWithTools implements the ToolCaller interface for vLLM
func (p *vLLMProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	// Convert messages to OpenAI format
	type ChatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	chatMessages := make([]ChatMessage, 0)
	for _, msg := range messages {
		chatMessages = append(chatMessages, ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Build request
	type Request struct {
		Model    string                   `json:"model"`
		Messages []ChatMessage            `json:"messages"`
		Tools    []map[string]interface{} `json:"tools"`
		Stream   bool                     `json:"stream"`
	}

	reqBody := Request{
		Model:    p.model,
		Messages: chatMessages,
		Tools:    tools,
		Stream:   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return parseTypedChatResponse(body)
}
