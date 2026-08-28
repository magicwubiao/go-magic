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

// DashScopeProvider 阿里云DashScope (兼容OpenAI格式)
type DashScopeProvider struct {
	apiKey  string
	model   string
	baseURL string
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
		apiKey:       apiKey,
		model:        model,
		baseURL:      baseURL,
		BaseProvider: NewBaseProvider(baseURL),
	}
}

func (p *DashScopeProvider) Name() string {
	return "dashscope"
}

// dashscopeDefaultTemperature 与 openai_compatible.applyExtraParams 的
// 默认值保持一致：显式温度降低服务端默认采样退化（重复思考循环）的概率。
const dashscopeDefaultTemperature = 0.7

func (p *DashScopeProvider) Chat(ctx context.Context, messages []Message) (*ChatResponse, error) {
	reqBody := map[string]interface{}{
		"model":       p.model,
		"messages":    ConvertMessagesForProvider(messages, p.BaseProvider),
		"temperature": dashscopeDefaultTemperature,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	// DashScope 兼容 OpenAI 格式
	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("dashscope api error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no response from dashscope")
	}

	respMsg := result.Choices[0].Message
	response := &ChatResponse{
		Content: respMsg.Content,
	}

	for _, tc := range respMsg.ToolCalls {
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		response.ToolCalls = append(response.ToolCalls, types.ToolCall{
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

	return response, nil
}

// ChatWithTools implements the ToolCaller interface for DashScope
func (p *DashScopeProvider) ChatWithTools(ctx context.Context, messages []Message, tools []map[string]interface{}) (*ChatResponse, error) {
	reqBody := map[string]interface{}{
		"model":       p.model,
		"messages":    ConvertMessagesForProvider(messages, p.BaseProvider),
		"tools":       tools,
		"temperature": dashscopeDefaultTemperature,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("dashscope api error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no response from dashscope")
	}

	respMsg := result.Choices[0].Message
	response := &ChatResponse{
		Content: respMsg.Content,
	}

	for _, tc := range respMsg.ToolCalls {
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		response.ToolCalls = append(response.ToolCalls, types.ToolCall{
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

	return response, nil
}
