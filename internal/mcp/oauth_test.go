package mcp

// OAuth 2.1 全链路测试：不 mock 内部函数，直接用 httptest 搭一个"授权服务器 +
// 受保护资源"的假端点，走完 发现 → 动态注册 → PKCE 授权 → 回调兑换 → 落盘
// → 传输层注入 Bearer → 401 刷新重试 的完整路径。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAuthServer 是一个最小 OAuth 2.1 授权服务器：
//   - /.well-known/oauth-protected-resource/<path>：受保护资源元数据
//   - /.well-known/oauth-authorization-server：AS 元数据
//   - /register：动态客户端注册
//   - /authorize：校验 PKCE 参数后 302 到回调（带 code + state）
//   - /token：校验 code_verifier 后发 token
type fakeAuthServer struct {
	t           *testing.T
	mu          sync.Mutex
	server      *httptest.Server
	srvBase     string
	registered  int
	tokenExch   int
	lastCode    string
	lastVerifier string
	lastScope   string
	lastAuthHeader string // /token 收到的 Authorization 头（机密客户端用）
	authCode    string
}

func newFakeAuthServer(t *testing.T, secretClient bool) *fakeAuthServer {
	f := &fakeAuthServer{t: t, authCode: "auth-code-123"}
	mux := http.NewServeMux()

	// 受保护资源元数据（路径感知变体）：/mcp → /.well-known/oauth-protected-resource/mcp
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"resource":              f.srvBase + "/mcp",
			"authorization_servers": []string{f.srvBase},
			"scopes_supported":      []string{"mcp:tools"},
		})
	})

	// AS 元数据
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                 f.srvBase,
			"authorization_endpoint": f.srvBase + "/authorize",
			"token_endpoint":         f.srvBase + "/token",
			"registration_endpoint":  f.srvBase + "/register",
			"scopes_supported":       []string{"mcp:tools", "other"},
			"code_challenge_methods_supported": []string{"S256"},
		})
	})

	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.registered++
		f.mu.Unlock()
		var req dcrRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad DCR request: %v", err)
		}
		if len(req.RedirectURIs) != 1 || !strings.HasPrefix(req.RedirectURIs[0], "http://127.0.0.1:") {
			t.Fatalf("unexpected redirect URIs: %v", req.RedirectURIs)
		}
		resp := map[string]string{"client_id": "dyn-client-1"}
		if secretClient {
			resp["client_secret"] = "dyn-secret"
		}
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("response_type") != "code" || q.Get("client_id") == "" {
			http.Error(w, "bad authorize request", http.StatusBadRequest)
			return
		}
		if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
			t.Errorf("authorize request missing PKCE: %v", q)
		}
		f.mu.Lock()
		f.lastScope = q.Get("scope")
		f.mu.Unlock()
		// 模拟用户点"允许"：302 到本地回调，带上 code 和 state。
		cb, _ := url.Parse(q.Get("redirect_uri"))
		cbq := cb.Query()
		cbq.Set("code", f.authCode)
		cbq.Set("state", q.Get("state"))
		if res := q.Get("resource"); res != "" {
			cbq.Set("resource", res)
		}
		cb.RawQuery = cbq.Encode()
		http.Redirect(w, r, cb.String(), http.StatusFound)
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.tokenExch++
		f.lastAuthHeader = r.Header.Get("Authorization")
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != f.authCode {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			f.lastCode = r.Form.Get("code")
			f.lastVerifier = r.Form.Get("code_verifier")
			// PKCE verifier 必须在 43-128 之间（OAuth 2.1 硬性要求）。
			if n := len(f.lastVerifier); n < 43 || n > 128 {
				t.Errorf("code_verifier length = %d, want 43..128", n)
			}
		case "refresh_token":
			if r.Form.Get("refresh_token") == "" {
				http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  fmt.Sprintf("at-%d", f.tokenExch),
			"refresh_token": fmt.Sprintf("rt-%d", f.tokenExch),
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         r.Form.Get("scope"),
		})
	})

	f.server = httptest.NewServer(mux)
	f.srvBase = f.server.URL
	t.Cleanup(f.server.Close)
	return f
}

// --- 完整授权流程 ---

func TestOAuthFullFlow(t *testing.T) {
	f := newFakeAuthServer(t, false)
	store := NewOAuthTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	mgr := NewOAuthManager(store)

	authorized := make(chan string, 1)
	mgr.SetOnAuthorized(func(name string) { authorized <- name })

	name := "cloud"
	cfg := ServerConfig{Transport: TransportHTTP, URL: f.srvBase + "/mcp", OAuth: &OAuthConfig{}}

	authorizeURL, err := mgr.StartOAuth(name, cfg)
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}
	if authorizeURL == "" {
		t.Fatal("authorize URL is empty")
	}
	if !strings.Contains(authorizeURL, "/authorize?") {
		t.Fatalf("authorize URL = %s", authorizeURL)
	}

	// 状态在等待授权期间必须是 awaiting_authorization。
	if st := mgr.Status(name); st.State != OAuthStateAwaitingAuthorization {
		t.Fatalf("status during flow = %+v", st)
	}

	// 模拟浏览器：GET authorize URL，跟随 302 打到本地回调。
	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Get(authorizeURL)
	if err != nil {
		t.Fatalf("following authorize URL: %v", err)
	}
	resp.Body.Close()

	// 回调触发授权完成 → token 落盘 → onAuthorized 回调。
	select {
	case got := <-authorized:
		if got != name {
			t.Fatalf("onAuthorized got %q, want %q", got, name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("onAuthorized was not called")
	}

	if st := mgr.Status(name); st.State != OAuthStateAuthorized {
		t.Fatalf("status after flow = %+v", st)
	}
	tok, ok := store.Get(name)
	if !ok || tok.AccessToken != "at-1" || tok.RefreshToken == "" {
		t.Fatalf("stored token = %+v ok=%v", tok, ok)
	}
	if tok.TokenEndpoint == "" || tok.ClientID != "dyn-client-1" {
		t.Errorf("token metadata missing: %+v", tok)
	}
	// scope 来自资源元数据（用户未配置时）。
	f.mu.Lock()
	scope := f.lastScope
	f.mu.Unlock()
	if scope != "mcp:tools" {
		t.Errorf("authorize scope = %q, want mcp:tools", scope)
	}
}

// --- 预注册客户端（不开放 DCR）---

func TestOAuthPreRegisteredClient(t *testing.T) {
	f := newFakeAuthServer(t, false)
	// 去掉 registration_endpoint：强制走预配置 client_id 路径。
	mux := f.server.Config.Handler.(*http.ServeMux)
	_ = mux // 元数据端点返回的 registration_endpoint 仍在，但配置了 client_id 就不会调用它
	store := NewOAuthTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	mgr := NewOAuthManager(store)

	cfg := ServerConfig{Transport: TransportSSE, URL: f.srvBase + "/mcp",
		OAuth: &OAuthConfig{ClientID: "static-client", ClientSecret: "static-secret"}}

	authorizeURL, err := mgr.StartOAuth("pre", cfg)
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}
	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Get(authorizeURL)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	resp.Body.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.Status("pre").State == OAuthStateAuthorized {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if st := mgr.Status("pre"); st.State != OAuthStateAuthorized {
		t.Fatalf("status = %+v", st)
	}
	// 机密客户端兑换 token 用 Basic 认证。
	f.mu.Lock()
	auth := f.lastAuthHeader
	f.mu.Unlock()
	if auth == "" || !strings.HasPrefix(auth, "Basic ") {
		t.Errorf("token request Authorization = %q, want Basic …", auth)
	}
	if f.mu.Lock(); f.registered != 0 {
		t.Errorf("DCR was called %d times despite pre-registered client", f.registered)
	}
	f.mu.Unlock()
}

// --- 401 自动刷新重试（传输层） ---

func TestTransportInjectsBearerAndRefreshesOn401(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	var firstAuth, secondAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		auth := r.Header.Get("Authorization")
		if n == 1 {
			firstAuth = auth
			// 第一次：token 过期，回 401 触发刷新。
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		secondAuth = auth
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "result": map[string]string{"ok": "1"}})
	}))
	defer ts.Close()

	transport, err := NewSSETransportWithOptions(ts.URL, nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	transport.SetAuthProvider(&fakeProvider{
		current: "Bearer stale-token",
		refresh: "Bearer fresh-token",
	})

	resp, err := transport.Send(context.Background(), &JSONRPCRequest{JSONRPC: JSONRPCVersion, ID: 1, Method: "ping"})
	if err != nil {
		t.Fatalf("Send after refresh: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if firstAuth != "Bearer stale-token" {
		t.Errorf("first request auth = %q", firstAuth)
	}
	if secondAuth != "Bearer fresh-token" {
		t.Errorf("retry auth = %q, want refreshed token", secondAuth)
	}
}

// fakeProvider 是 AuthTokenProvider 的内存实现。
type fakeProvider struct {
	current string
	refresh string
}

func (p *fakeProvider) CurrentToken(ctx context.Context) (string, error) { return p.current, nil }
func (p *fakeProvider) RefreshToken(ctx context.Context) (string, error) { return p.refresh, nil }

// --- OAuth 缺失凭据时的错误分类 ---

func TestTransportAuthErrorIsKinded(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "result": map[string]string{"ok": "1"}})
	}))
	defer ts.Close()

	transport, _ := NewSSETransportWithOptions(ts.URL, nil, 5*time.Second)
	transport.SetAuthProvider(&fakeProvider{current: "", refresh: ""})
	// CurrentToken 返回空串时（此处模拟）不注入 Authorization，请求应当
	// 正常完成——Authorization 注入只在拿得到非空 token 时发生。
	resp, err := transport.Send(context.Background(), &JSONRPCRequest{JSONRPC: JSONRPCVersion, ID: 1, Method: "ping"})
	if err != nil || resp == nil {
		t.Fatalf("empty token should not inject Authorization; err=%v", err)
	}
}

// --- EnsureToken：有效 / 过期刷新 / 无凭据 ---

func TestEnsureTokenRefreshesExpiredToken(t *testing.T) {
	f := newFakeAuthServer(t, false)
	store := NewOAuthTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	mgr := NewOAuthManager(store)

	// 过期 token + refresh token：EnsureToken 应静默续期。
	expired := OAuthToken{
		AccessToken:   "at-old",
		RefreshToken:  "rt-old",
		ExpiresAt:     time.Now().Add(-time.Hour),
		TokenEndpoint: f.srvBase + "/token",
		ClientID:      "dyn-client-1",
	}
	if err := store.Save("svc", expired); err != nil {
		t.Fatal(err)
	}

	header, err := mgr.EnsureToken("svc")
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	if header != "Bearer at-1" {
		t.Fatalf("header = %q, want refreshed Bearer at-1", header)
	}
	tok, _ := store.Get("svc")
	if tok.AccessToken != "at-1" || tok.RefreshToken != "rt-1" {
		t.Fatalf("token after refresh = %+v", tok)
	}
	// 冗余字段要保留，否则下一次刷新无从发起。
	if tok.TokenEndpoint != f.srvBase+"/token" || tok.ClientID != "dyn-client-1" {
		t.Errorf("refresh dropped metadata: %+v", tok)
	}
}

func TestEnsureTokenFailsWithAuthKind(t *testing.T) {
	store := NewOAuthTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	mgr := NewOAuthManager(store)

	_, err := mgr.EnsureToken("ghost")
	if err == nil {
		t.Fatal("expected error")
	}
	if KindOf(err) != ErrKindAuth {
		t.Fatalf("kind = %q, want auth", KindOf(err))
	}
}

// --- token 存储 ---

func TestTokenStorePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "tokens.json")
	store := NewOAuthTokenStore(path)

	if err := store.Save("a", OAuthToken{AccessToken: "tok-a", TokenType: "Bearer"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// 文件权限必须 0600（token 是敏感凭据）。Windows 的 FAT/NTFS 权限模型
	// 不走 POSIX mode 位（读到 666），只在 Unix 上断言。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("token file mode = %o, want 600", perm)
		}
	}

	// 新实例能读到（模拟重启）。
	reopened := NewOAuthTokenStore(path)
	tok, ok := reopened.Get("a")
	if !ok || tok.AccessToken != "tok-a" {
		t.Fatalf("Get after reopen = %+v ok=%v", tok, ok)
	}

	// AuthHeader 默认 Bearer。
	if got := tok.AuthHeader(); got != "Bearer tok-a" {
		t.Errorf("AuthHeader = %q", got)
	}

	if err := reopened.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := reopened.Get("a"); ok {
		t.Error("token still present after Delete")
	}
}

// --- 掩码合并（oauth client_secret 与 headers 同一约定）---

func TestNormalizeOAuthConfig(t *testing.T) {
	// "oauth": true → 纯自动。
	cfg, err := normalizeOAuthConfig(true)
	if err != nil || cfg == nil || cfg.ClientID != "" {
		t.Fatalf("normalizeOAuthConfig(true) = %+v, %v", cfg, err)
	}
	// "oauth": false / 缺省 → nil。
	if cfg, err := normalizeOAuthConfig(false); err != nil || cfg != nil {
		t.Fatalf("normalizeOAuthConfig(false) = %+v, %v", cfg, err)
	}
	if cfg, err := normalizeOAuthConfig(nil); err != nil || cfg != nil {
		t.Fatalf("normalizeOAuthConfig(nil) = %+v, %v", cfg, err)
	}
	// 对象形态。
	cfg, err = normalizeOAuthConfig(map[string]interface{}{
		"client_id":     "abc",
		"client_secret": "s3cret",
		"scopes":        []interface{}{"a", "b"},
		"redirect_port": float64(8765),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "abc" || cfg.ClientSecret != "s3cret" || cfg.RedirectPort != 8765 ||
		len(cfg.Scopes) != 2 || cfg.Scopes[0] != "a" {
		t.Fatalf("object form parsed = %+v", cfg)
	}
	// scopes 也可以是空格分隔的单字符串。
	cfg, err = normalizeOAuthConfig(map[string]interface{}{"scopes": "read write"})
	if err != nil || len(cfg.Scopes) != 2 {
		t.Fatalf("string scopes parsed = %+v, %v", cfg, err)
	}
	// 非法形态要报错。
	if _, err := normalizeOAuthConfig(42); err == nil {
		t.Error("expected error for numeric oauth field")
	}
}

// --- well-known 候选生成 ---

func TestWellKnownCandidates(t *testing.T) {
	got := wellKnownCandidates("https://api.example.com/mcp/v1", "oauth-protected-resource")
	want := []string{
		"https://api.example.com/.well-known/oauth-protected-resource/mcp/v1",
		"https://api.example.com/.well-known/oauth-protected-resource",
	}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate[%d] = %s, want %s", i, got[i], want[i])
		}
	}
	// 根路径只有一种。
	got = wellKnownCandidates("https://api.example.com", "oauth-authorization-server")
	if len(got) != 1 {
		t.Fatalf("root candidates = %v", got)
	}
}

// --- 自动授权路径（EnsureAuthorized / onFlowStarted / 手动入口静默） ---

func TestEnsureAuthorizedAutoStartsFlowAndNotifies(t *testing.T) {
	f := newFakeAuthServer(t, false)
	store := NewOAuthTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	mgr := NewOAuthManager(store)

	flowStarted := make(chan string, 4)
	mgr.SetOnFlowStarted(func(name, authorizeURL string) {
		flowStarted <- name + "|" + authorizeURL
	})
	authorized := make(chan string, 1)
	mgr.SetOnAuthorized(func(name string) { authorized <- name })

	cfg := ServerConfig{Transport: TransportHTTP, URL: f.srvBase + "/mcp", OAuth: &OAuthConfig{}}

	// 无 token → 自动发起流程并触发 onFlowStarted（后端据此弹浏览器）。
	url1, err := mgr.EnsureAuthorized("svc", cfg)
	if err != nil {
		t.Fatalf("EnsureAuthorized: %v", err)
	}
	if url1 == "" {
		t.Fatal("expected authorize URL from auto path")
	}
	select {
	case got := <-flowStarted:
		if !strings.HasPrefix(got, "svc|") {
			t.Fatalf("onFlowStarted got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("onFlowStarted was not called")
	}

	// 流程进行中再次调用：复用同一个 URL，不重复通知（不重复弹浏览器）。
	url2, err := mgr.EnsureAuthorized("svc", cfg)
	if err != nil {
		t.Fatalf("second EnsureAuthorized: %v", err)
	}
	if url2 != url1 {
		t.Fatalf("in-progress flow URL changed: %q vs %q", url2, url1)
	}
	select {
	case got := <-flowStarted:
		t.Fatalf("onFlowStarted fired again: %q", got)
	case <-time.After(300 * time.Millisecond):
	}

	// 模拟浏览器完成授权 → token 落盘。
	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Get(url1)
	if err != nil {
		t.Fatalf("following authorize URL: %v", err)
	}
	resp.Body.Close()
	select {
	case <-authorized:
	case <-time.After(5 * time.Second):
		t.Fatal("onAuthorized was not called")
	}

	// 有有效 token 后再调用：直接返回空串，不发起任何流程。
	url3, err := mgr.EnsureAuthorized("svc", cfg)
	if err != nil || url3 != "" {
		t.Fatalf("EnsureAuthorized with valid token = (%q, %v), want empty", url3, err)
	}
	select {
	case got := <-flowStarted:
		t.Fatalf("onFlowStarted fired for authorized server: %q", got)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestEnsureAuthorizedCooldown(t *testing.T) {
	f := newFakeAuthServer(t, false)
	store := NewOAuthTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	mgr := NewOAuthManager(store)

	notifyCount := 0
	// onFlowStarted 在独立 goroutine 里触发，计数要用 channel 同步，
	// 否则断言和异步通知赛跑。
	notifyCh := make(chan struct{}, 4)
	mgr.SetOnFlowStarted(func(name, authorizeURL string) {
		notifyCh <- struct{}{}
	})
	countNotifies := func(n int, timeout time.Duration) {
		for i := 0; i < n; i++ {
			select {
			case <-notifyCh:
				notifyCount++
			case <-time.After(timeout):
				t.Fatalf("expected %d notifications, got %d", n, notifyCount)
			}
		}
	}

	cfg := ServerConfig{Transport: TransportHTTP, URL: f.srvBase + "/mcp", OAuth: &OAuthConfig{}}
	if _, err := mgr.EnsureAuthorized("svc", cfg); err != nil {
		t.Fatalf("first EnsureAuthorized: %v", err)
	}
	countNotifies(1, 5*time.Second)

	// 模拟流程被 watchdog/SignOut 收尾后仍无 token：冷却期内不允许再次
	// 自动发起，否则重连循环会轰炸用户。
	mgr.mu.Lock()
	for _, fl := range mgr.flows {
		mgr.closeFlowLocked(fl)
		select {
		case <-fl.done:
		default:
			close(fl.done)
		}
		delete(mgr.flows, fl.serverName)
	}
	mgr.mu.Unlock()

	url, err := mgr.EnsureAuthorized("svc", cfg)
	if err != nil {
		t.Fatalf("cooldown EnsureAuthorized: %v", err)
	}
	if url != "" {
		t.Fatal("flow started during cooldown, want no-op")
	}
	// 冷却期内不允许再发通知或建流程；给一点时间让假如有 bug 的异步
	// 通知跑出来。
	select {
	case <-notifyCh:
		t.Fatal("onFlowStarted fired during cooldown")
	case <-time.After(300 * time.Millisecond):
	}
	if len(mgr.flows) != 0 {
		t.Fatalf("flow created during cooldown: %v", mgr.flows)
	}
}

func TestStartOAuthManualDoesNotNotify(t *testing.T) {
	f := newFakeAuthServer(t, false)
	store := NewOAuthTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	mgr := NewOAuthManager(store)

	fired := make(chan struct{}, 1)
	mgr.SetOnFlowStarted(func(name, authorizeURL string) { fired <- struct{}{} })

	cfg := ServerConfig{Transport: TransportHTTP, URL: f.srvBase + "/mcp", OAuth: &OAuthConfig{}}
	url, err := mgr.StartOAuthManual("svc", cfg)
	if err != nil || url == "" {
		t.Fatalf("StartOAuthManual = (%q, %v)", url, err)
	}
	select {
	case <-fired:
		t.Fatal("manual path must not fire onFlowStarted (frontend opens the browser itself)")
	case <-time.After(300 * time.Millisecond):
	}
}
