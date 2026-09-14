package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// Regression（"改了 key 还是 401" 事故）：PUT /api/providers/{name} 只把新 key 写进
// config.json，运行中的 provider 实例仍带旧 key —— 缓存 agent 与 server 共享同一
// 实例，于是聊天继续 401「无效的 API Key」，而设置页的"测试连接"却是通的（它用新
// 配置新建临时 provider，掩盖了问题）。这里用"只认正确 key"的假服务验证：保存新
// key 之后，运行实例必须立刻能聊通。
func TestProviderKeyUpdateReachesLiveProvider(t *testing.T) {
	const goodKey = "sk-good"
	var seenAuth []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		seenAuth = append(seenAuth, auth)
		if auth != "Bearer "+goodKey {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]string{
					"code":    "1002",
					"message": "无效的 API Key：该密钥不存在或已被删除/停用，请在平台「API 密钥」页面重新生成",
				},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"choices": []map[string]interface{}{{"index": 0, "message": map[string]string{"role": "assistant", "content": "pong"}}},
		})
	}))
	defer ts.Close()

	s := &Server{cfg: &appconfig.Config{
		Provider: "custom",
		Model:    "test-model",
		Providers: map[string]appconfig.ProviderConfig{
			"custom": {APIKey: "sk-wrong", BaseURL: ts.URL, Models: []string{"test-model"}},
		},
	}}
	s.provider = createProvider(s.cfg)
	if s.provider == nil {
		t.Fatal("provider not built from config")
	}

	// 前提：错的 key 确实会被服务端拒绝。
	if _, err := s.provider.Chat(context.Background(), pingMessages()); err == nil {
		t.Fatal("precondition failed: the wrong key should be rejected")
	}

	// 用户在模型设置页把 key 改成正确的值并保存 → PUT /api/providers/custom
	putProvider(t, s, `{"api_key":"`+goodKey+`"}`)
	if got := s.cfg.Providers["custom"].APIKey; got != goodKey {
		t.Fatalf("saved key = %q, want %q", got, goodKey)
	}

	// 聊天必须立刻用新 key 成功（旧实现此时仍带 sk-wrong → 401，且要重启才生效）。
	if _, err := s.provider.Chat(context.Background(), pingMessages()); err != nil {
		t.Fatalf("live provider still uses the old key: %v (auth headers sent: %v)", err, seenAuth)
	}
}

// 编辑弹窗用列表接口返回的**脱敏** key 预填输入框（maskAPIKey → "sk-g****-key"）。
// 用户不动 key 直接保存时，脱敏串不能被当成真 key 落盘 —— 否则配置里看不出问题
// （长度/前缀都像真的），但之后所有请求都 401。
func TestProviderUpdateIgnoresMaskedKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "pong"}}},
		})
	}))
	defer ts.Close()

	s := &Server{cfg: &appconfig.Config{
		Provider: "custom",
		Model:    "test-model",
		Providers: map[string]appconfig.ProviderConfig{
			"custom": {APIKey: "sk-good-real-key", BaseURL: ts.URL, Models: []string{"test-model"}},
		},
	}}
	s.provider = createProvider(s.cfg)

	putProvider(t, s, `{"api_key":"sk-g****-key","models":["test-model"]}`)

	if got := s.cfg.Providers["custom"].APIKey; got != "sk-good-real-key" {
		t.Fatalf("masked key must not overwrite the stored credential, got %q", got)
	}
	if _, err := s.provider.Chat(context.Background(), pingMessages()); err != nil {
		t.Fatalf("live provider broken after a masked-key save: %v", err)
	}
}

func pingMessages() []types.Message {
	return []types.Message{{Role: "user", Content: "ping"}}
}
