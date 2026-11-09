package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newLanAccessTestServer 构造只覆盖扫码接入路径的最小 Server：
// 这些 handler 只碰 pairMu/authToken/sessions/loginLimiter，不需要完整 NewServer。
func newLanAccessTestServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		authToken:    "$2a$10$somebcrypt_HASH_placeholder",
		sessions:     newWebSessionManagerFile(filepath.Join(t.TempDir(), "web_sessions.json")),
		loginLimiter: newLoginRateLimiter(10*time.Minute, 1000),
		pairTokens:   make(map[string]time.Time),
	}
}

func postBody(t *testing.T, body string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return m
}

// 开关关闭时签发配对码必须被拒绝：这是"扫码免密登录"默认关闭的安全底线。
func TestAccessPairDisabled(t *testing.T) {
	s := newLanAccessTestServer(t)

	w := httptest.NewRecorder()
	s.handleAccessPair(w, postBody(t, "{}"))
	if w.Code != 400 {
		t.Fatalf("pairing disabled: want 400, got %d", w.Code)
	}

	// 关掉开关后再签发同样拒绝，且之前的码一并作废。
	s.pairMu.Lock()
	s.pairingEnabled = true
	s.pairTokens["stale"] = time.Now().Add(time.Hour)
	s.pairMu.Unlock()

	w2 := httptest.NewRecorder()
	s.handleAccessPairing(w2, postBody(t, `{"enabled":false}`))
	if w2.Code != 200 {
		t.Fatalf("disable pairing: want 200, got %d", w2.Code)
	}
	s.pairMu.Lock()
	left := len(s.pairTokens)
	s.pairMu.Unlock()
	if left != 0 {
		t.Fatalf("stale tokens should be wiped on disable, left %d", left)
	}

	w3 := httptest.NewRecorder()
	s.handleAccessPair(w3, postBody(t, "{}"))
	if w3.Code != 400 {
		t.Fatalf("pair after disable: want 400, got %d", w3.Code)
	}
}

// 完整链路：开启开关 → 签发 → 兑换成功 → 码作废（重放拒绝）→ 坏码拒绝。
func TestAccessPairRedeemFlow(t *testing.T) {
	s := newLanAccessTestServer(t)

	w := httptest.NewRecorder()
	s.handleAccessPairing(w, postBody(t, `{"enabled":true}`))
	if w.Code != 200 {
		t.Fatalf("enable pairing: want 200, got %d", w.Code)
	}

	wPair := httptest.NewRecorder()
	s.handleAccessPair(wPair, postBody(t, "{}"))
	if wPair.Code != 200 {
		t.Fatalf("issue pair: want 200, got %d", wPair.Code)
	}
	res := decodeBody(t, wPair)
	url, _ := res["url"].(string)
	if !strings.Contains(url, "/#/login?pair=") {
		t.Fatalf("pair url should contain hash-router login link, got %q", url)
	}
	pairToken := strings.SplitN(url, "pair=", 2)[1]

	// 错误码拒绝（4xx，不能是 401 —— 前端把 401 当会话失效做全局登出跳转）。
	wBad := httptest.NewRecorder()
	s.handleAccessPairRedeem(wBad, postBody(t, `{"token":"wrong"}`))
	if wBad.Code != 400 {
		t.Fatalf("bad pair token: want 400, got %d", wBad.Code)
	}

	// 正确码兑换成功，返回的会话必须真实有效。
	wRedeem := httptest.NewRecorder()
	s.handleAccessPairRedeem(wRedeem, postBody(t, `{"token":"`+pairToken+`"}`))
	if wRedeem.Code != 200 {
		t.Fatalf("redeem: want 200, got %d", wRedeem.Code)
	}
	out := decodeBody(t, wRedeem)
	sessionToken, _ := out["token"].(string)
	if sessionToken == "" || !s.sessions.validate(sessionToken) {
		t.Fatalf("redeemed session should be valid, got %q", sessionToken)
	}

	// 单次有效：同一码重放必须拒绝。
	wReplay := httptest.NewRecorder()
	s.handleAccessPairRedeem(wReplay, postBody(t, `{"token":"`+pairToken+`"}`))
	if wReplay.Code != 400 {
		t.Fatalf("replay: want 400, got %d", wReplay.Code)
	}
}

// 过期码即使还在 map 里也不能兑换。
func TestAccessPairRedeemExpired(t *testing.T) {
	s := newLanAccessTestServer(t)
	s.pairMu.Lock()
	s.pairingEnabled = true
	s.pairTokens["expired"] = time.Now().Add(-time.Minute)
	s.pairMu.Unlock()

	w := httptest.NewRecorder()
	s.handleAccessPairRedeem(w, postBody(t, `{"token":"expired"}`))
	if w.Code != 400 {
		t.Fatalf("expired token: want 400, got %d", w.Code)
	}
}

// info 接口在禁用状态下也要如实回报开关，便于面板渲染。
func TestAccessInfo(t *testing.T) {
	s := newLanAccessTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", bytes.NewReader(nil))

	w := httptest.NewRecorder()
	s.handleAccessInfo(w, req)
	res := decodeBody(t, w)
	if got, _ := res["pairing_enabled"].(bool); got {
		t.Fatalf("pairing_enabled should default to false, got %v", res)
	}
}
