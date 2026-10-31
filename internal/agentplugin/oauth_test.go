package agentplugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/connectors"
)

func TestOAuthKeyRoundTrip(t *testing.T) {
	key := OAuthKey("mcp-notion", "notion")
	if key != "agentplugin:mcp-notion/notion" {
		t.Fatalf("OAuthKey = %q", key)
	}
	plugin, server, ok := ParseOAuthKey(key)
	if !ok || plugin != "mcp-notion" || server != "notion" {
		t.Fatalf("ParseOAuthKey = (%q,%q,%v)", plugin, server, ok)
	}

	// 连接器键（没有 agentplugin: 前缀）必须被识别为"不是插件"，
	// 否则授权回调会把连接器误当插件去找。
	if _, _, ok := ParseOAuthKey("notion"); ok {
		t.Fatal("connector key must not parse as plugin key")
	}
	if _, _, ok := ParseOAuthKey("agentplugin:/svc"); ok {
		t.Fatal("empty plugin name must not parse")
	}
	if _, _, ok := ParseOAuthKey("agentplugin:plugin/"); ok {
		t.Fatal("empty server name must not parse")
	}
}

func TestOAuthCapable(t *testing.T) {
	remote := func(mut func(*MCPServerSpec)) MCPServerSpec {
		spec := MCPServerSpec{Type: TransportStreamableHTTP, URL: "https://example.com/mcp"}
		mut(&spec)
		return spec
	}

	cases := []struct {
		name string
		spec MCPServerSpec
		want bool
	}{
		{"stdio never", MCPServerSpec{Type: TransportStdio, Command: "npx"}, false},
		{"remote declared", remote(func(s *MCPServerSpec) { s.OAuth = &mcp.OAuthConfig{} }), true},
		{"remote without creds", remote(func(*MCPServerSpec) {}), true},
		{
			"remote with required env",
			remote(func(s *MCPServerSpec) { s.Env = map[string]string{"API_KEY": ""} }),
			false,
		},
		{
			"remote with static auth header",
			remote(func(s *MCPServerSpec) {
				s.Headers = map[string]string{"Authorization": "Bearer fixed"}
			}),
			false,
		},
		{
			"declared wins over header",
			remote(func(s *MCPServerSpec) {
				s.Headers = map[string]string{"Authorization": "Bearer fixed"}
				s.OAuth = &mcp.OAuthConfig{}
			}),
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oauthCapable(tc.spec); got != tc.want {
				t.Fatalf("oauthCapable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOAuthServerConfigMapsTransport(t *testing.T) {
	cfg := oauthServerConfig(MCPServerSpec{
		Type: TransportSSE,
		URL:  "https://example.com/sse",
		OAuth: &mcp.OAuthConfig{
			ClientID: "cid",
			Scopes:   []string{"read", "write"},
		},
	})
	if cfg.Transport != mcp.TransportSSE {
		t.Fatalf("transport = %q", cfg.Transport)
	}
	if cfg.URL != "https://example.com/sse" || cfg.OAuth == nil || cfg.OAuth.ClientID != "cid" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	// 深拷贝：改副本不能影响原 spec（并发连接时共享 spec 是常态）。
	cfg.OAuth.Scopes[0] = "mutated"

	cfg2 := oauthServerConfig(MCPServerSpec{Type: TransportStreamableHTTP, URL: "https://e/mcp"})
	if cfg2.Transport != mcp.TransportHTTP {
		t.Fatalf("streamable-http should map to %q, got %q", mcp.TransportHTTP, cfg2.Transport)
	}
	if cfg2.OAuth != nil {
		t.Fatal("no oauth declaration must stay nil")
	}
}

// fakeTokenProvider 是 AuthTokenProvider 的内存实现。
type fakeTokenProvider struct {
	current string
	refresh string
}

func (f *fakeTokenProvider) CurrentToken(context.Context) (string, error) {
	return f.current, nil
}

func (f *fakeTokenProvider) RefreshToken(context.Context) (string, error) {
	if f.refresh == "" {
		return "", errors.New("no refresh token")
	}
	return f.refresh, nil
}

func TestHTTPTransportRefreshesOn401(t *testing.T) {
	var calls int
	var gotAuth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	tr := newHTTPTransport(srv.URL, nil, false)
	tr.SetAuthProvider(&fakeTokenProvider{current: "Bearer stale", refresh: "Bearer fresh"})
	resp, err := tr.Send(context.Background(), &mcp.JSONRPCRequest{JSONRPC: mcp.JSONRPCVersion, Method: "initialize", ID: 1})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts (401 then refresh), got %d", calls)
	}
	if len(gotAuth) != 2 || gotAuth[0] != "Bearer stale" || gotAuth[1] != "Bearer fresh" {
		t.Fatalf("Authorization headers = %v", gotAuth)
	}
}

func TestHTTPTransport401IsAuthKind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	tr := newHTTPTransport(srv.URL, nil, false)
	_, err := tr.Send(context.Background(), &mcp.JSONRPCRequest{JSONRPC: mcp.JSONRPCVersion, Method: "initialize", ID: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	if mcp.KindOf(err) != mcp.ErrKindAuth {
		t.Fatalf("kind = %q, want auth", mcp.KindOf(err))
	}
	var se *httpStatusError
	if !errors.As(err, &se) || se.status != http.StatusUnauthorized {
		t.Fatalf("status error not preserved: %v", err)
	}
}

func TestRuntimeEntryStatusCarriesOAuthState(t *testing.T) {
	store := mcp.NewOAuthTokenStore(filepath.Join(t.TempDir(), "oauth_tokens.json"))
	mgr := mcp.NewOAuthManager(store)

	plugin := &Plugin{
		Manifest: &Manifest{Name: "demo"},
		MCPServers: []MCPEntryStatus{{
			Name: "svc",
			Spec: MCPServerSpec{
				Type:  TransportStreamableHTTP,
				URL:   "https://example.com/mcp",
				OAuth: &mcp.OAuthConfig{},
			},
		}},
	}
	rt := NewRuntime(plugin)
	rt.SetOAuth(mgr)

	// 没 token：可用但未授权。
	st := rt.EntryStatuses()[0]
	if !st.OAuthAvailable || st.OAuthState != "none" {
		t.Fatalf("before authorize: available=%v state=%q", st.OAuthAvailable, st.OAuthState)
	}

	// 落一个有效 token 后应视为已授权（不触发任何网络请求）。
	if err := store.Save(OAuthKey("demo", "svc"), mcp.OAuthToken{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save token: %v", err)
	}
	st, ok := rt.EntryStatus("svc")
	if !ok {
		t.Fatal("entry not found")
	}
	if st.OAuthState != "authorized" {
		t.Fatalf("state = %q, want authorized", st.OAuthState)
	}
}

func TestValidateRejectsOAuthOnStdio(t *testing.T) {
	_, err := validateAndExpandSpec(MCPServerSpec{
		Type:    TransportStdio,
		Command: "npx",
		OAuth:   &mcp.OAuthConfig{},
	}, t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("oauth on stdio must be rejected")
	}
}

func TestCatalogSpecEmitsOAuth(t *testing.T) {
	spec := buildMCPSpec(connectors.Connector{
		ID:          "demo-remote",
		Name:        "Demo",
		Transport:   "http",
		URL:         "https://example.com/mcp",
		DefaultName: "demo",
		OAuth:       &connectors.OAuthRequirement{Scopes: []string{"read"}},
		Requires:    []connectors.Requirement{{Key: "LEGACY_TOKEN", Where: connectors.WhereHeader}},
	}, nil)
	if spec.OAuth == nil || len(spec.OAuth.Scopes) != 1 || spec.OAuth.Scopes[0] != "read" {
		t.Fatalf("oauth block missing: %+v", spec.OAuth)
	}
	// OAuth 条目不再要求用户填凭据：两套凭据并存会把"点一下登录"退化成手抄 token。
	if len(spec.Headers) != 0 || len(spec.Env) != 0 {
		t.Fatalf("oauth entry should not declare credentials: headers=%v env=%v", spec.Headers, spec.Env)
	}
	if keys := SpecCredentialKeys(mcpOutToSpec(spec)); len(keys) != 0 {
		t.Fatalf("credential keys = %v, want none", keys)
	}
}

// newAuthMCPServer 启动一个要求 Bearer token 的最小 MCP server 替身：
// 没有 Authorization 就 401，有就按 method 回 initialize / tools/list。
func newAuthMCPServer(t *testing.T, wantToken string) (*httptest.Server, *bool) {
	t.Helper()
	sawAuth := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+wantToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sawAuth = true
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			w.WriteHeader(http.StatusOK) // 通知：不回包
			return
		}
		result := "{}"
		switch req.Method {
		case "initialize":
			result = `{"protocolVersion":"2024-11-05"}`
		case "tools/list":
			result = `{"tools":[{"name":"echo","description":"d","inputSchema":{"type":"object"}}]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":` + result + `}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &sawAuth
}

func newOAuthRuntime(t *testing.T, url string, withToken bool) *Runtime {
	t.Helper()
	store := mcp.NewOAuthTokenStore(filepath.Join(t.TempDir(), "oauth_tokens.json"))
	mgr := mcp.NewOAuthManager(store)
	if withToken {
		if err := store.Save(OAuthKey("demo", "svc"), mcp.OAuthToken{
			AccessToken: "tok",
			ExpiresAt:   time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatalf("save token: %v", err)
		}
	}
	rt := NewRuntime(&Plugin{
		Manifest: &Manifest{Name: "demo"},
		MCPServers: []MCPEntryStatus{{
			Name: "svc",
			Spec: MCPServerSpec{
				Type:  TransportStreamableHTTP,
				URL:   url,
				OAuth: &mcp.OAuthConfig{},
			},
		}},
	})
	rt.SetOAuth(mgr)
	return rt
}

func TestConnectEntryUsesOAuthToken(t *testing.T) {
	srv, sawAuth := newAuthMCPServer(t, "tok")
	rt := newOAuthRuntime(t, srv.URL, true)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st, ok := rt.EntryStatus("svc")
	if !ok || !st.Connected {
		t.Fatalf("entry not connected: %+v (err=%q kind=%q)", st, st.Error, st.ErrorKind)
	}
	if st.ToolCount != 1 {
		t.Fatalf("tool count = %d, want 1", st.ToolCount)
	}
	if !*sawAuth {
		t.Fatal("server never received the OAuth bearer token")
	}
	// 有 token 时状态必须是 authorized（UI 才不会继续渲染"登录"按钮）。
	if st.OAuthState != "authorized" {
		t.Fatalf("oauth state = %q", st.OAuthState)
	}
}

func TestConnectEntryWithoutTokenReportsAuth(t *testing.T) {
	srv, _ := newAuthMCPServer(t, "tok")
	rt := newOAuthRuntime(t, srv.URL, false)
	if err := rt.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st, ok := rt.EntryStatus("svc")
	if !ok || st.Connected {
		t.Fatalf("entry should not connect without a token: %+v", st)
	}
	if st.ErrorKind != mcp.ErrKindAuth.String() {
		t.Fatalf("error kind = %q, want auth (err=%q)", st.ErrorKind, st.Error)
	}
	if st.OAuthAvailable != true || st.OAuthState != "none" {
		t.Fatalf("oauth view = (available=%v state=%q)", st.OAuthAvailable, st.OAuthState)
	}
}
