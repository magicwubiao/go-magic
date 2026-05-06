package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// WenxinProvider implements the Baidu Wenxin (ERNIE) API
type WenxinProvider struct {
	apiKey       string
	secretKey    string
	model        string
	accessToken  string
	tokenExpiry  time.Time
	tokenMu      sync.RWMutex
	baseURL      string
	client       *http.Client
}

// NewWenxinProvider creates a new Wenxin provider
func NewWenxinProvider(apiKey, secretKey, model string) *WenxinProvider {
	if model == "" {
		model = "ernie-4.0-8k-latest" // Default model
	}
	return &WenxinProvider{
		apiKey:    apiKey,
		secretKey: secretKey,
		model:     model,
		baseURL:   "https://aip.baidubce.com/rpc/2.0/ai_custom/v1",
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *WenxinProvider) Name() string {
	return "wenxin"
}

// GetCapabilities returns the capabilities of Wenxin
func (p *WenxinProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:     true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      true,
		Vision:          true,
	}
}

// getAccessToken gets or refreshes the access token
func (p *WenxinProvider) getAccessToken(ctx context.Context) (string, error) {
	p.tokenMu.RLock()
	if p.accessToken != "" && time.Now().Before(p.tokenExpiry) {
		token := p.accessToken
		p.tokenMu.RUnlock()
		return token, nil
	}
	p.tokenMu.RUnlock()

	// Get new token
	url := fmt.Sprintf("https://aip.baidubce.com/oauth/2.0/token?grant_type=client_credentials&client_id=%s&client_secret=%s",
		p.apiKey, p.secretKey)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}

	if tokenResp.Error != "" {
		return "", fmt.Errorf("wenxin token error: %s - %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	p.tokenMu.Lock()
	p.accessToken = tokenResp.AccessToken
	p.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	p.tokenMu.Unlock()

	return tokenResp.AccessToken, nil
}

// Chat implements the Provider interface
func (p *WenxinProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	reqBody := p.buildRequest(messages, nil, false)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/wenxinworkshop/chat/%s?access_token=%s", p.baseURL, p.model, token)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

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
func (p *WenxinProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	reqBody := p.buildRequest(messages, tools, false)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/wenxinworkshop/chat/%s?access_token=%s", p.baseURL, p.model, token)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

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
func (p *WenxinProvider) Stream(ctx context.Context, messages []Message, handler StreamHandler) error {
	token, err := p.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	reqBody := p.buildRequest(messages, nil, true)
	reqBody["stream"] = true
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/wenxinworkshop/chat/%s?access_token=%s", p.baseURL, p.model, token)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

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

// buildRequest builds the Wenxin request from messages
func (p *WenxinProvider) buildRequest(messages []Message, tools []map[string]interface{}, stream bool) map[string]interface{} {
	req := map[string]interface{}{
		"messages": p.convertMessages(messages),
		"stream":   stream,
	}

	if len(tools) > 0 {
		// Wenxin uses "tools" field
		req["tools"] = tools
	}

	return req
}

// convertMessages converts messages to Wenxin format
func (p *WenxinProvider) convertMessages(messages []Message) []map[string]interface{} {
	var converted []map[string]interface{}

	for _, msg := range messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "assistant"
		} else if msg.Role == "system" {
			role = "user" // Wenxin handles system differently
		}

		m := map[string]interface{}{
			"role":    role,
			"content": msg.Content,
		}

		if msg.Role == "tool" {
			m["role"] = "assistant"
			m["content"] = msg.Content
		}

		converted = append(converted, m)
	}

	return converted
}

// parseResponse parses the Wenxin response
func (p *WenxinProvider) parseResponse(body []byte) (*ChatResponse, error) {
	var resp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Result  string `json:"result"`
		IsSafe  bool   `json:"is_safe"`
		Usage   struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		ErrorCode int    `json:"error_code"`
		ErrorMsg  string `json:"error_msg"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.ErrorCode != 0 {
		return nil, fmt.Errorf("wenxin error [%d]: %s", resp.ErrorCode, resp.ErrorMsg)
	}

	return &ChatResponse{
		Content: resp.Result,
		Usage: &Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

// parseStreamResponse parses streaming SSE response
func (p *WenxinProvider) parseStreamResponse(body io.Reader, handler StreamHandler) error {
	scanner := bufio.NewScanner(body)
	var accumulatedContent string

	for scanner.Scan() {
		line := scanner.Text()

		// Wenxin uses custom format: data: {"id":...,"object":"chat.completion.chunk",...}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}

		var chunk struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			Result  string `json:"result"`
			IsEnd   bool   `json:"is_end"`
			Usage    struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Result != "" {
			accumulatedContent += chunk.Result
			handler(&StreamResponse{
				Content: chunk.Result,
				Done:    false,
			})
		}

		if chunk.IsEnd {
			handler(&StreamResponse{
				Content: "",
				Done:    true,
			})
			return nil
		}
	}

	handler(&StreamResponse{
		Content: accumulatedContent,
		Done:    true,
	})

	return scanner.Err()
}

// parseError parses error responses
func (p *WenxinProvider) parseError(body []byte, statusCode int) error {
	var errResp struct {
		ErrorCode int    `json:"error_code"`
		ErrorMsg  string `json:"error_msg"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil && errResp.ErrorMsg != "" {
		return fmt.Errorf("wenxin error [%d]: %s", errResp.ErrorCode, errResp.ErrorMsg)
	}

	return fmt.Errorf("wenxin error (%d): %s", statusCode, string(body))
}
