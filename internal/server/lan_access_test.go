package server

import (
	"bytes"
	"encoding/json"
	"net"
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

// cidr 构造一个 *net.IPNet 形式的网卡地址（pickLanIP 的输入形态）。
// 注意 ParseCIDR 返回的 IPNet.IP 是掩码后的网络地址（如 10.10.22.0），
// 而 InterfaceAddrs 给出的是完整主机地址 —— 这里必须用 ParseCIDR 的第一个
// 返回值（原始 IP）重建，否则测出来的和真实网卡形态对不上。
func cidr(t *testing.T, s string) *net.IPNet {
	t.Helper()
	ip, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("parse cidr %q: %v", s, err)
	}
	return &net.IPNet{IP: ip, Mask: ipNet.Mask}
}

// 选 IP 必须：跳过回环与 169.254 链路本地（断网网卡的 APIPA 地址），
// 优先 RFC1918 私网段，公网地址只作兜底。
func TestPickLanIP(t *testing.T) {
	cases := []struct {
		name  string
		addrs []net.Addr
		want  string
	}{
		{
			name:  "prefer private over link-local",
			addrs: []net.Addr{cidr(t, "169.254.108.14/16"), cidr(t, "10.10.22.242/24")},
			want:  "10.10.22.242",
		},
		{
			name:  "only link-local means no LAN",
			addrs: []net.Addr{cidr(t, "169.254.108.14/16")},
			want:  "",
		},
		{
			name:  "public address as fallback",
			addrs: []net.Addr{cidr(t, "203.0.113.7/24")},
			want:  "203.0.113.7",
		},
		{
			name:  "loopback ignored",
			addrs: []net.Addr{cidr(t, "127.0.0.1/8"), cidr(t, "192.168.1.5/24")},
			want:  "192.168.1.5",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickLanIP(tc.addrs); got != tc.want {
				t.Fatalf("pickLanIP = %q, want %q", got, tc.want)
			}
		})
	}
}
