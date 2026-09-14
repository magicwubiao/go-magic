package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// Regression（改 key 不生效事故）：模型设置页保存新 API Key 后，运行中的
// provider 实例必须立刻带上新 key。旧实现只写 config，缓存 agent 共享的实例
// 仍用旧 key 请求，于是聊天一直 401「无效的 API Key」，而设置页的"测试连接"
// 却是通的（它用新配置新建临时 provider）。
func TestApplyCredentialsUpdatesLiveInstance(t *testing.T) {
	p := NewZhipuProvider("sk-old", "", "glm-5.3")
	if p.BaseProvider.APIKey != "sk-old" {
		t.Fatalf("precondition: key = %q", p.BaseProvider.APIKey)
	}
	// 无效 key 连续失败会把熔断器打到 open（阈值 5）；换 key 时必须清零，
	// 否则新 key 的请求仍会被熔断器挡下，用户看到的还是"不行"。
	for i := 0; i < 5; i++ {
		p.BaseProvider.RecordFailure()
	}
	if p.BaseProvider.GetHealthState() != CircuitOpen {
		t.Fatal("precondition: circuit breaker should be open after repeated failures")
	}

	if !ApplyCredentials(p, "sk-new", "https://api.example.com/v1/") {
		t.Fatal("ApplyCredentials should handle the OpenAI-compatible family")
	}
	if p.BaseProvider.APIKey != "sk-new" {
		t.Fatalf("live key = %q, want sk-new", p.BaseProvider.APIKey)
	}
	if p.BaseProvider.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("live base URL = %q (trailing slash must be trimmed)", p.BaseProvider.BaseURL)
	}
	if got := p.BaseProvider.GetHealthState(); got != CircuitClosed {
		t.Fatalf("circuit breaker = %v, want closed after credential refresh", got)
	}

	// 空串表示保持原值（前端只改 base_url 时不至于把 key 清空）。
	ApplyCredentials(p, "", "")
	if p.BaseProvider.APIKey != "sk-new" {
		t.Fatalf("empty apiKey must keep the stored key, got %q", p.BaseProvider.APIKey)
	}
}

// 内嵌 *OpenAICompatibleProvider 的具体 provider（zhipu/longcat/openai/deepseek…）
// 通过方法提升自动获得 SetCredentials，无需逐文件实现。这里按行为验证：换 key 后
// 真实请求头里带的必须是新 key（凭据存在私有字段里时这一步会失败）。
func TestApplyCredentialsCoversEmbeddingProviders(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "pong"}}},
		})
	}))
	defer ts.Close()

	for _, tc := range []struct {
		name string
		p    Provider
	}{
		{"longcat", NewLongCatProvider("old-longcat", ts.URL, "LongCat-2.0-Preview")},
		{"openai", NewOpenAIProvider("old-openai", ts.URL, "gpt-5.6")},
		{"deepseek", NewDeepSeekProvider("old-deepseek", ts.URL, "deepseek-v4-flash", nil)},
		{"dashscope", NewDashScopeProvider("old-dashscope", ts.URL, "qwen3.8-flash")},
		{"zhipu", NewZhipuProvider("old-zhipu", ts.URL, "glm-5.3")},
	} {
		gotAuth = ""
		if !ApplyCredentials(tc.p, "sk-fresh", "") {
			t.Fatalf("%s: ApplyCredentials returned false", tc.name)
		}
		if _, err := tc.p.Chat(context.Background(), []types.Message{{Role: "user", Content: "ping"}}); err != nil {
			t.Fatalf("%s: chat failed: %v", tc.name, err)
		}
		if gotAuth != "Bearer sk-fresh" {
			t.Fatalf("%s: request sent %q, want %q", tc.name, gotAuth, "Bearer sk-fresh")
		}
	}
}

// 凭据存在私有字段里的实现（gemini/together/perplexity/wenxin/cohere…）没有
// 统一设置入口，必须如实返回 false，让 server 回退到"重建 provider + 清缓存
// agent"，而不是假装更新成功让用户继续撞 401。
func TestApplyCredentialsReportsUnsupportedProviders(t *testing.T) {
	p := NewGeminiProvider("old", "", "gemini-3.8-flash")
	if ApplyCredentials(p, "sk-fresh", "") {
		t.Fatal("gemini keeps its own apiKey; must report false so the caller rebuilds")
	}
	if ApplyCredentials(nil, "sk-fresh", "") {
		t.Fatal("nil provider must report false")
	}
}
