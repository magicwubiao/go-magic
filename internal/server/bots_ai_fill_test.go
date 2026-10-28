package server

// ai-fill 的回归测试：模型第一次回复夹带闲话/围栏导致解析失败时，handler
// 必须自动重试一次（附更硬的格式要求），而不是直接把错误甩给用户。
// 另外钉死「两次都失败才回 502，且回的是第一次的原始错误」。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/internal/provider"
)

// aiFillStubProvider 按顺序吐出预设回复（超出下标后重复最后一条）。
type aiFillStubProvider struct {
	responses []string
	calls     int
}

func (p *aiFillStubProvider) Name() string { return "stub" }

func (p *aiFillStubProvider) Chat(ctx context.Context, messages []provider.Message) (*provider.ChatResponse, error) {
	i := p.calls
	p.calls++
	if i >= len(p.responses) {
		i = len(p.responses) - 1
	}
	return &provider.ChatResponse{Content: p.responses[i]}, nil
}

func aiFillTestServer(p provider.Provider) *Server {
	return &Server{provider: p}
}

func postAIFill(s *Server, idea string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/bots/ai-fill",
		strings.NewReader(`{"idea":"`+idea+`"}`))
	rec := httptest.NewRecorder()
	s.handleBotAIFill(rec, req)
	return rec
}

func TestAIFillRetriesOnceOnGarbageFirstReply(t *testing.T) {
	p := &aiFillStubProvider{responses: []string{
		// 第一次：JSON 前后夹带说明（带围栏但首行是闲话，start..end 截取后会
		// 混入正文导致解析失败——足够贴近真实故障）。
		"Sure! Here is the config: {oops this is not json",
		// 第二次：干净的 JSON。
		`{"title":"研究员","description":"做研究","system_prompt":"You are a researcher","avatar":"🔍"}`,
	}}
	rec := postAIFill(aiFillTestServer(p), "帮我做一个研究员")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s; want 200 (retry should recover)", rec.Code, rec.Body.String())
	}
	if p.calls != 2 {
		t.Fatalf("provider calls = %d; want 2 (one retry)", p.calls)
	}
	var got botAIFillResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	if got.Title != "研究员" {
		t.Fatalf("title = %q; want 研究员", got.Title)
	}
}

func TestAIFillGivesUpAfterRetry(t *testing.T) {
	p := &aiFillStubProvider{responses: []string{"no json here", "still no json"}}
	rec := postAIFill(aiFillTestServer(p), "帮我做一个研究员")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d; want 502", rec.Code)
	}
	if p.calls != 2 {
		t.Fatalf("provider calls = %d; want 2", p.calls)
	}
}

func TestAIFillFirstTryCleanJSON(t *testing.T) {
	p := &aiFillStubProvider{responses: []string{
		`{"title":"Writer","description":"writes","system_prompt":"You write","avatar":"✍"}`,
	}}
	rec := postAIFill(aiFillTestServer(p), "写手")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s; want 200", rec.Code, rec.Body.String())
	}
	if p.calls != 1 {
		t.Fatalf("provider calls = %d; want 1 (no needless retry)", p.calls)
	}
}
