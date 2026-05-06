package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/magicwubiao/go-magic/pkg/types"
)

type vLLMProvider struct {
	baseURL string
	model   string
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

	// Parse response (OpenAI compatible)
	type Choice struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}

	type Response struct {
		Choices []Choice `json:"choices"`
	}

	var respData Response
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(respData.Choices) == 0 {
		return nil, fmt.Errorf("no response from vLLM")
	}

	return &ChatResponse{
		Content: respData.Choices[0].Message.Content,
	}, nil
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

	// Parse response (OpenAI compatible with tools)
	type ToolCall struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}

	type Choice struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	}

	type Response struct {
		Choices []Choice `json:"choices"`
	}

	var respData Response
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(respData.Choices) == 0 {
		return nil, fmt.Errorf("no response from vLLM")
	}

	response := &ChatResponse{
		Content: respData.Choices[0].Message.Content,
	}

	for _, tc := range respData.Choices[0].Message.ToolCalls {
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		response.ToolCalls = append(response.ToolCalls, types.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return response, nil
}
