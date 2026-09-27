package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchModelsOpenAICompatible(t *testing.T) {
	// baseURL 已带版本段（/v1）：直接命中 {base}/models
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q", got)
		}
		w.Write([]byte(`{"data":[{"id":"model-b"},{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), "deepseek", "sk-test", srv.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "model-b" || models[1] != "model-a" {
		t.Fatalf("models = %v, want deduped [model-b model-a]", models)
	}
}

func TestFetchModelsV1Fallback(t *testing.T) {
	// baseURL 不带版本段（deepseek 风格）：{base}/models 404 后回退 {base}/v1/models
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[{"id":"fallback-model"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), "custom", "sk-test", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0] != "fallback-model" {
		t.Fatalf("models = %v", models)
	}
}

func TestFetchModelsOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("ollama 不应带鉴权头")
		}
		w.Write([]byte(`{"models":[{"name":"qwen3:8b"},{"name":"llama4:latest"}]}`))
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), "ollama", "", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "qwen3:8b" {
		t.Fatalf("models = %v", models)
	}
}

func TestFetchModelsGemini(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("key") != "g-key" {
			t.Error("gemini 应通过 query key 鉴权")
		}
		w.Write([]byte(`{"models":[{"name":"models/gemini-2.0-flash"},{"name":"models/gemini-3-pro"}]}`))
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), "gemini", "g-key", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "gemini-2.0-flash" {
		t.Fatalf("models = %v, want 'models/' 前缀被剥掉", models)
	}
}

func TestFetchModelsAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != "ak-test" || r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic 应带 x-api-key 与 anthropic-version 头")
		}
		w.Write([]byte(`{"data":[{"id":"claude-sonnet-5"}]}`))
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), "anthropic", "ak-test", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0] != "claude-sonnet-5" {
		t.Fatalf("models = %v", models)
	}
}

func TestFetchModelsWenxinUnsupported(t *testing.T) {
	if _, err := FetchModels(context.Background(), "wenxin", "k", "https://x"); err == nil {
		t.Fatal("wenxin 应明确报不支持")
	}
}

func TestFetchModelsEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	if _, err := FetchModels(context.Background(), "deepseek", "k", srv.URL+"/v1"); err == nil {
		t.Fatal("空模型列表应报错而不是静默成功")
	}
}
