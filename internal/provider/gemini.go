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
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// GeminiProvider implements the Google Gemini API
type GeminiProvider struct {
	apiKey      string
	model       string
	baseURL     string
	client      *http.Client
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	if model == "" {
		model = "gemini-1.5-flash" // Default to cost-effective model
	}
	return &GeminiProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://generativelanguage.googleapis.com/v1beta",
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *GeminiProvider) Name() string {
	return "gemini"
}

// GetCapabilities returns the capabilities of Gemini
func (p *GeminiProvider) GetCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:     true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      true,
		Vision:          true,
	}
}

// geminiContent represents a content part in Gemini
type geminiContent struct {
	Role  string        `json:"role,omitempty"`
	Parts []geminiPart  `json:"parts"`
}

// geminiPart represents a part of content
type geminiPart struct {
	Text string `json:"text,omitempty"`
	// For function calling
	FunctionCall *struct {
		Name      string                 `json:"name"`
		Args      map[string]interface{} `json:"args"`
	} `json:"functionCall,omitempty"`
	FunctionResponse *struct {
		Name       string                 `json:"name"`
		Response   map[string]interface{} `json:"response"`
	} `json:"functionResponse,omitempty"`
	// For media
	InlineData *struct {
		MimeType string `json:"mime_type"`
		Data     string `json:"data"`
	} `json:"inlineData,omitempty"`
}

// geminiRequest represents Gemini API request
type geminiRequest struct {
	Contents           []geminiContent       `json:"contents"`
	SystemInstruction *geminiContent        `json:"systemInstruction,omitempty"`
	Tools             []geminiTool          `json:"tools,omitempty"`
	GenerationConfig   *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

// geminiTool represents a tool definition
type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

// geminiFunctionDeclaration represents a function declaration
type geminiFunctionDeclaration struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Parameters  *geminiSchema             `json:"parameters"`
}

// geminiSchema represents a JSON schema
type geminiSchema struct {
	Type        string                  `json:"type"`
	Properties  map[string]interface{}  `json:"properties,omitempty"`
	Required    []string                `json:"required,omitempty"`
	Description string                  `json:"description,omitempty"`
	Items       *geminiSchema           `json:"items,omitempty"`
}

// geminiGenerationConfig represents generation configuration
type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	TopK            *int     `json:"topK,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

// geminiResponse represents Gemini API response
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Role  string        `json:"role"`
			Parts []geminiPart  `json:"parts"`
		} `json:"content"`
		FinishReason   string `json:"finishReason"`
		Index          int    `json:"index"`
		SafetyRatings []struct {
			Category    string `json:"category"`
			Probability string `json:"probability"`
		} `json:"safetyRatings"`
	} `json:"candidates"`
	PromptFeedback struct {
		SafetyRatings []struct {
			Category    string `json:"category"`
			Probability string `json:"probability"`
		} `json:"safetyRatings"`
	} `json:"promptFeedback"`
	UsageMetadata struct {
		PromptTokenCount         int `json:"promptTokenCount"`
		CandidatesTokenCount     int `json:"candidatesTokenCount"`
		TotalTokenCount          int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// geminiStreamChunk represents a streaming response chunk
type geminiStreamChunk struct {
	Candidates []struct {
		Content struct {
			Role  string        `json:"role"`
			Parts []geminiPart  `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
		Index        int    `json:"index"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount         int `json:"promptTokenCount"`
		CandidatesTokenCount     int `json:"candidatesTokenCount"`
		TotalTokenCount          int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// Chat implements the Provider interface
func (p *GeminiProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, nil, false)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, p.model, p.apiKey)
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
func (p *GeminiProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	reqBody := p.buildRequest(messages, tools, false)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, p.model, p.apiKey)
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
func (p *GeminiProvider) Stream(ctx context.Context, messages []Message, handler StreamHandler) error {
	reqBody := p.buildRequest(messages, nil, true)
	
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?key=%s&alt=sse", p.baseURL, p.model, p.apiKey)
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

	scanner := bufio.NewScanner(resp.Body)
	var accumulatedText string

	for scanner.Scan() {
		line := scanner.Text()
		
		// Skip empty lines and "data:" prefix
		if line == "" || strings.HasPrefix(line, "data:") {
			continue
		}

		var chunk geminiStreamChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}

		for _, candidate := range chunk.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					accumulatedText += part.Text
					handler(&StreamResponse{
						Content: part.Text,
						Done:    candidate.FinishReason != "",
					})
				}

				if part.FunctionCall != nil {
					handler(&StreamResponse{
						ToolCall: &types.ToolCall{
							ID:       fmt.Sprintf("call_%d", time.Now().UnixNano()),
							Type:     "function",
							Function: types.Function{Name: part.FunctionCall.Name, Arguments: part.FunctionCall.Args},
						},
						Done: true,
					})
				}
			}
		}
	}

	handler(&StreamResponse{
		Content: "",
		Done:    true,
	})

	return scanner.Err()
}

// buildRequest builds the Gemini request from messages
func (p *GeminiProvider) buildRequest(messages []Message, tools []map[string]interface{}, stream bool) *geminiRequest {
	var contents []geminiContent
	var systemInstruction *geminiContent

	for i, msg := range messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}

		// Extract system message
		if msg.Role == "system" {
			if systemInstruction == nil {
				systemInstruction = &geminiContent{
					Parts: []geminiPart{{Text: msg.Content}},
				}
			}
			continue
		}

		// Skip system messages as we've extracted them
		if msg.Role == "system" {
			continue
		}

		part := geminiPart{Text: msg.Content}

		// Handle tool results
		if msg.ToolCallID != "" {
			part = geminiPart{
				FunctionResponse: &struct {
					Name       string                 `json:"name"`
					Response   map[string]interface{} `json:"response"`
				}{
					Name:     msg.ToolName,
					Response: map[string]interface{}{"result": msg.Content},
				},
			}
		}

		content := geminiContent{
			Role:  role,
			Parts: []geminiPart{part},
		}

		contents = append(contents, content)
	}

	req := &geminiRequest{
		Contents:           contents,
		SystemInstruction: systemInstruction,
	}

	// Add tools if provided
	if len(tools) > 0 {
		req.Tools = []geminiTool{{
			FunctionDeclarations: p.convertTools(tools),
		}}
	}

	return req
}

// convertTools converts OpenAI-style tools to Gemini format
func (p *GeminiProvider) convertTools(tools []map[string]interface{}) []geminiFunctionDeclaration {
	var declarations []geminiFunctionDeclaration

	for _, tool := range tools {
		if fn, ok := tool["function"].(map[string]interface{}); ok {
			decl := geminiFunctionDeclaration{
				Name:        getString(fn, "name"),
				Description: getString(fn, "description"),
			}

			if params, ok := fn["parameters"].(map[string]interface{}); ok {
				decl.Parameters = p.convertSchema(params)
			}

			declarations = append(declarations, decl)
		}
	}

	return declarations
}

// convertSchema converts OpenAI-style schema to Gemini format
func (p *GeminiProvider) convertSchema(params map[string]interface{}) *geminiSchema {
	schema := &geminiSchema{
		Type: getString(params, "type"),
	}

	if desc, ok := params["description"].(string); ok {
		schema.Description = desc
	}

	if props, ok := params["properties"].(map[string]interface{}); ok {
		schema.Properties = make(map[string]interface{})
		for k, v := range props {
			if propMap, ok := v.(map[string]interface{}); ok {
				schema.Properties[k] = p.convertSchema(propMap)
			} else {
				schema.Properties[k] = v
			}
		}
	}

	if req, ok := params["required"].([]interface{}); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	return schema
}

// parseResponse parses the Gemini response
func (p *GeminiProvider) parseResponse(body []byte) (*ChatResponse, error) {
	var geminiResp geminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates in response")
	}

	candidate := geminiResp.Candidates[0]
	var content string
	var toolCalls []types.ToolCall

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			content += part.Text
		}

		if part.FunctionCall != nil {
			tc := types.ToolCall{
				ID:       fmt.Sprintf("call_%d", time.Now().UnixNano()),
				Type:     "function",
				Function: types.Function{
					Name:      part.FunctionCall.Name,
					Arguments: part.FunctionCall.Args,
				},
			}
			toolCalls = append(toolCalls, tc)
		}
	}

	return &ChatResponse{
		Content:   content,
		ToolCalls: toolCalls,
		Usage: &Usage{
			PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

// parseError parses error responses
func (p *GeminiProvider) parseError(body []byte, statusCode int) error {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		return fmt.Errorf("gemini error [%s]: %s", errResp.Error.Status, errResp.Error.Message)
	}

	return fmt.Errorf("gemini error (%d): %s", statusCode, string(body))
}

// getString safely gets a string from a map
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
