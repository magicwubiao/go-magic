package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FetchModels 从厂商 API 实时拉取可用模型列表。
//
// 各家端点差异在此统一收敛：
//
//	ollama     GET  {base}/api/tags                      → models[].name（无需鉴权）
//	gemini     GET  {base}/models?key=KEY               → models[].name（去掉 "models/" 前缀）
//	anthropic  GET  {base}/v1/models（x-api-key 头）     → data[].id
//	其余       OpenAI 兼容 GET {base}/models（Bearer）   → data[].id；404 时自动重试 {base}/v1/models
//
// baseURL 由调用方解析好（表单覆盖值 > 用户配置 > 内置目录），这里只负责
// 协议适配。wenxin 走独立的鉴权模型（apiKey+secretKey 换 access_token，且
// 每个模型是独立端点），不提供列表接口，直接报可读错误。
func FetchModels(ctx context.Context, name, apiKey, baseURL string) ([]string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("no base URL configured")
	}

	switch name {
	case "wenxin":
		return nil, fmt.Errorf("文心一言使用独立的鉴权模型，不支持在线获取模型列表，请手动填写")
	case "ollama":
		return fetchModelsOpenAIStyle(ctx, base+"/api/tags", "", parseOllamaModels)
	case "gemini":
		if apiKey == "" {
			return nil, fmt.Errorf("Gemini 需要 API key 才能获取模型列表")
		}
		return fetchModelsOpenAIStyle(ctx, base+"/models?key="+apiKey, "", parseGeminiModels)
	case "anthropic":
		return fetchAnthropicModels(ctx, base, apiKey)
	default:
		return fetchOpenAICompatibleModels(ctx, base, apiKey)
	}
}

// fetchOpenAICompatibleModels 拉取 OpenAI 兼容的 /models 列表。base 可能带
// 版本段（.../v1、.../api/v3）也可能不带（deepseek、perplexity）：先按原样
// 请求，404 时补一段 /v1 再试，兼容两种书写习惯。
func fetchOpenAICompatibleModels(ctx context.Context, base, apiKey string) ([]string, error) {
	urls := []string{base + "/models"}
	if !hasVersionSegment(base) {
		urls = append(urls, base+"/v1/models")
	}
	var lastErr error
	for _, u := range urls {
		models, err := fetchModelsOpenAIStyle(ctx, u, apiKey, parseOpenAIModels)
		if err == nil {
			return models, nil
		}
		lastErr = err
		if !isNotFound(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func hasVersionSegment(base string) bool {
	for _, seg := range strings.Split(base, "/") {
		switch seg {
		case "v1", "v2", "v3", "v4", "compatible-mode":
			return true
		}
	}
	return false
}

type modelIDParser func(body []byte) ([]string, error)

func parseOpenAIModels(body []byte) ([]string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("unexpected /models response: %w", err)
	}
	models := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	return models, nil
}

func parseOllamaModels(body []byte) ([]string, error) {
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("unexpected /api/tags response: %w", err)
	}
	models := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		if m.Name != "" {
			models = append(models, m.Name)
		}
	}
	return models, nil
}

func parseGeminiModels(body []byte) ([]string, error) {
	var out struct {
		Models []struct {
			Name string `json:"name"` // e.g. "models/gemini-2.0-flash"
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("unexpected models list response: %w", err)
	}
	models := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		name := strings.TrimPrefix(m.Name, "models/")
		if name != "" {
			models = append(models, name)
		}
	}
	return models, nil
}

func fetchAnthropicModels(ctx context.Context, base, apiKey string) ([]string, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Anthropic 需要 API key 才能获取模型列表")
	}
	return fetchModelsOpenAIStyle(ctx, base+"/v1/models", apiKey, parseOpenAIModels,
		map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		})
}

// fetchModelsOpenAIStyle 发起 GET 并用 parser 解析响应。extraHeaders 附加在
// Authorization 之外（anthropic 的 x-api-key 走这里）。
func fetchModelsOpenAIStyle(ctx context.Context, url, apiKey string, parse modelIDParser, extraHeaders ...map[string]string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for _, hdrs := range extraHeaders {
		for k, v := range hdrs {
			req.Header.Set(k, v)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 模型列表上限 4MB，防异常端点拖爆内存
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, &fetchModelsHTTPError{code: resp.StatusCode}
	}
	if resp.StatusCode != http.StatusOK {
		// 厂商错误体一般是 {"error":{"message":"..."}}，截出来放进错误信息
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	models, err := parse(body)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("model list is empty")
	}
	return dedupeModels(models), nil
}

type fetchModelsHTTPError struct{ code int }

func (e *fetchModelsHTTPError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

func isNotFound(err error) bool {
	he, ok := err.(*fetchModelsHTTPError)
	return ok && he.code == http.StatusNotFound
}

func dedupeModels(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, m := range in {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}
