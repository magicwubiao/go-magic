package mcp

// OAuth 2.1 授权支持（MCP 授权规范，2025-03-26 / 2025-06-18）。
//
// 完整链路：
//
//	1. 发现：从 MCP 端点出发，按 RFC 9728 找 /.well-known/oauth-protected-resource
//	   拿到 authorization_servers，再按 RFC 8414 拉授权服务器元数据。
//	2. 注册：没有预配 client_id 时走动态客户端注册（RFC 7591，DCR）。
//	3. 授权：Authorization Code + PKCE(S256)，本机起一个 127.0.0.1 回调
//	   listener 接浏览器跳转。
//	4. 兑换/刷新：code 换 token；过期后用 refresh_token 静默续期。
//	5. 注入：传输层每次请求自动带 Authorization: Bearer，401 时刷新重试一次。
//
// 安全约束：
//   - token 落盘 0600，单文件 JSON，temp+rename 原子写（项目约定）；
//   - token / client_secret 永远不进日志、不进任何 API 响应；
//   - state 参数防 CSRF，PKCE 防授权码截获；
//   - 回调 listener 只绑 127.0.0.1，授权完成后即关闭，10 分钟 watchdog 兜底。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// OAuthConfig 是连接器级 OAuth 配置（ServerConfig.OAuth）。
//
// 全部字段可选：留空 = 纯自动（发现 + 动态注册 + PKCE 公共客户端）。
// 预注册过 client 的服务（不开放 DCR 的）需要手工填 client_id / client_secret。
type OAuthConfig struct {
	// ClientID 预注册客户端 id。空 = 尝试动态客户端注册。
	ClientID string `json:"client_id,omitempty"`
	// ClientSecret 预注册客户端密钥。机密客户端才需要；API 层按 *** 掩码。
	ClientSecret string `json:"client_secret,omitempty"`
	// Scopes 请求的权限。空 = 采用授权服务器 scopes_supported。
	Scopes []string `json:"scopes,omitempty"`
	// RedirectPort 本机回调端口。0 = 每次挑空闲端口（DCR 场景必须为 0，
	// 因为 redirect_uri 是注册时动态确定的）。
	RedirectPort int `json:"redirect_port,omitempty"`
	// Resource RFC 8707 resource indicator。留空时自动取受保护资源元数据。
	Resource string `json:"resource,omitempty"`
}

// cloneOAuth 深拷贝，避免调用方共享 slice 被并发改写。
func (o *OAuthConfig) cloneOAuth() *OAuthConfig {
	if o == nil {
		return nil
	}
	cp := *o
	if len(o.Scopes) > 0 {
		cp.Scopes = append([]string(nil), o.Scopes...)
	}
	return &cp
}

// ---------------------------------------------------------------------------
// Token 存储
// ---------------------------------------------------------------------------

// OAuthToken 是单个连接器的凭据快照。TokenEndpoint / ClientID / ClientSecret
// 冗余存一份，刷新时不需要重新做发现与注册。
type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scope        string    `json:"scope,omitempty"`

	TokenEndpoint string `json:"token_endpoint,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	ClientSecret  string `json:"client_secret,omitempty"`
	Resource      string `json:"resource,omitempty"`
}

// Valid 报告 access token 是否可直接使用（留 30s 时钟偏移余量；
// 无过期时间的 token 视为长期有效，等 401 触发刷新）。
func (t *OAuthToken) Valid() bool {
	if t == nil || t.AccessToken == "" {
		return false
	}
	return t.ExpiresAt.IsZero() || time.Until(t.ExpiresAt) > 30*time.Second
}

// AuthHeader 按 token 类型拼 Authorization 值。默认 Bearer（RFC 6750）。
func (t *OAuthToken) AuthHeader() string {
	if t == nil || t.AccessToken == "" {
		return ""
	}
	typ := t.TokenType
	if typ == "" {
		typ = "Bearer"
	}
	// token_type 不区分大小写地匹配常见值，其余原样大写沿用。
	if strings.EqualFold(typ, "bearer") {
		typ = "Bearer"
	}
	return typ + " " + t.AccessToken
}

// OAuthTokenStore 把 token 按 server 名持久化到单个 JSON 文件。
//
// 为什么不进 config.json：config.json 是用户手工编辑的文件，混进密钥后
// 备份/分享/截图都会泄漏；独立文件 + 0600 是同类工具的通行做法。
type OAuthTokenStore struct {
	path string
	mu   sync.Mutex
}

// NewOAuthTokenStore 构造存储；目录不存在会在 Save 时创建。
func NewOAuthTokenStore(path string) *OAuthTokenStore {
	return &OAuthTokenStore{path: path}
}

func (s *OAuthTokenStore) readAll() (map[string]OAuthToken, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]OAuthToken{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return map[string]OAuthToken{}, nil
	}
	out := map[string]OAuthToken{}
	if err := json.Unmarshal(data, &out); err != nil {
		// 损坏时宁可从空开始，也不要让整个 MCP 模块挂掉。
		log.Warnf("[MCP] OAuth token file %s is corrupt, starting empty: %v", s.path, err)
		return map[string]OAuthToken{}, nil
	}
	return out, nil
}

// Get 返回指定连接器的 token；不存在返回 (nil, false)。
func (s *OAuthTokenStore) Get(name string) (*OAuthToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return nil, false
	}
	t, ok := all[name]
	if !ok {
		return nil, false
	}
	return &t, true
}

// Save 原子写入（temp + rename），文件权限 0600。落盘失败必须报错——
// 吞掉的话重启后授权会静默丢失，用户被迫反复重新登录。
func (s *OAuthTokenStore) Save(name string, tok OAuthToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	all[name] = tok

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Delete 移除指定连接器的 token（登出）。文件不存在视为成功。
func (s *OAuthTokenStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	if _, ok := all[name]; !ok {
		return nil
	}
	delete(all, name)
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ---------------------------------------------------------------------------
// 元数据发现（RFC 8414 / RFC 9728，路径感知变体）
// ---------------------------------------------------------------------------

// authorizationServerMetadata 是 RFC 8414 的子集（只取用到的字段）。
type authorizationServerMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// protectedResourceMetadata 是 RFC 9728 的子集。
type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

// wellKnownCandidates 生成 well-known 候选 URL。
//
// 按 RFC 8414/9728 的"路径插入"规则：https://h/a/mcp 先试
// https://h/.well-known/<suffix>/a/mcp，再退化到 https://h/.well-known/<suffix>。
// 两家 RFC 对顺序的表述一致（先带路径，再不带），实测两种实现都有，都试。
func wellKnownCandidates(serverURL, suffix string) []string {
	u, err := url.Parse(serverURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil
	}
	base := u.Scheme + "://" + u.Host
	path := strings.TrimSuffix(u.Path, "/")
	var out []string
	if path != "" && path != "/" {
		out = append(out, base+"/.well-known/"+suffix+path)
	}
	out = append(out, base+"/.well-known/"+suffix)
	return out
}

// fetchJSON GET 一个 JSON 元数据文档。非 200 或解析失败返回错误。
func fetchJSON(ctx context.Context, hc *http.Client, url string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// discoverProtectedResource 尝试拉取受保护资源元数据；拿不到不算错误
// （老式 AS 直接就是 MCP 端点本身的场景没有这份文档），返回 nil。
func discoverProtectedResource(ctx context.Context, hc *http.Client, serverURL string) *protectedResourceMetadata {
	for _, u := range wellKnownCandidates(serverURL, "oauth-protected-resource") {
		var meta protectedResourceMetadata
		if err := fetchJSON(ctx, hc, u, &meta); err == nil && len(meta.AuthorizationServers) > 0 {
			return &meta
		}
	}
	return nil
}

// discoverAuthorizationServer 拉授权服务器元数据；这是硬要求，
// 拿不到 authorization_endpoint 就无法发起授权。
func discoverAuthorizationServer(ctx context.Context, hc *http.Client, asURL string) (*authorizationServerMetadata, error) {
	var lastErr error
	for _, u := range wellKnownCandidates(asURL, "oauth-authorization-server") {
		var meta authorizationServerMetadata
		if err := fetchJSON(ctx, hc, u, &meta); err != nil {
			lastErr = err
			continue
		}
		if meta.AuthorizationEndpoint != "" && meta.TokenEndpoint != "" {
			if meta.Issuer == "" {
				meta.Issuer = asURL
			}
			return &meta, nil
		}
		lastErr = fmt.Errorf("authorization server metadata at %s is missing endpoints", u)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no OAuth metadata found for %s", asURL)
	}
	return nil, lastErr
}

// ---------------------------------------------------------------------------
// 动态客户端注册（RFC 7591）
// ---------------------------------------------------------------------------

type dcrRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope,omitempty"`
}

type dcrResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	Error        string `json:"error,omitempty"`
}

// registerClient 执行 DCR。公共客户端（无 secret）用 "none" 认证方法。
func registerClient(ctx context.Context, hc *http.Client, endpoint, redirectURI, scope string) (*dcrResponse, error) {
	body, err := json.Marshal(dcrRequest{
		ClientName:              "go-magic MCP connector",
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
		Scope:                   scope,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("dynamic client registration failed: HTTP %d: %s", resp.StatusCode, truncateForLog(string(data)))
	}
	var out dcrResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("invalid DCR response: %w", err)
	}
	if out.ClientID == "" {
		return nil, fmt.Errorf("dynamic client registration returned no client_id (server may not support RFC 7591; configure client_id manually)")
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// PKCE
// ---------------------------------------------------------------------------

// randomToken 生成 n 字节随机数的 base64url 编码（无 padding）。
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败基本意味着系统熵源坏了，直接 panic 是合理的。
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// pkcePair 生成 (verifier, S256 challenge)。
func pkcePair() (verifier, challenge string) {
	verifier = randomToken(48) // 64 chars，落在 43-128 的合法区间
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

// ---------------------------------------------------------------------------
// 授权流程
// ---------------------------------------------------------------------------

// OAuthFlowStatus 是 OAuth 流程对外的状态视图（API 返回它，绝不含 token）。
type OAuthFlowStatus struct {
	// State: authorized / awaiting_authorization / failed / expired / none
	State       string `json:"state"`
	Error       string `json:"error,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
}

const (
	OAuthStateAuthorized              = "authorized"
	OAuthStateAwaitingAuthorization   = "awaiting_authorization"
	OAuthStateFailed                  = "failed"
	oauthFlowTimeout                  = 10 * time.Minute
	oauthCallbackPath                 = "/callback"
)

// oauthFlow 记录一次进行中的授权。浏览器点完"允许"后回调会消费它。
type oauthFlow struct {
	serverName   string
	serverURL    string
	authorizeURL string
	redirectURI  string
	state        string
	verifier     string
	clientID     string
	clientSecret string
	tokenURL     string
	resource     string

	listener net.Listener
	server   *http.Server
	done     chan struct{}
	flowErr  error
}

// OAuthManager 管理所有连接器的 OAuth 状态：进行中的流程 + token 存储。
//
// 并发模型：flows map 由 mu 保护；token 读写经 OAuthTokenStore 自带锁；
// 回调 handler 与 UI 轮询通过 done channel 同步结果。
type OAuthManager struct {
	mu    sync.Mutex
	store *OAuthTokenStore
	flows map[string]*oauthFlow
	// onAuthorized 在 token 落盘后被调用（新 goroutine），上层用它触发重连。
	onAuthorized func(serverName string)
	hc           *http.Client
}

// NewOAuthManager 构造管理器；store 为 nil 时所有流程启动都会报错。
func NewOAuthManager(store *OAuthTokenStore) *OAuthManager {
	return &OAuthManager{
		store: store,
		flows: make(map[string]*oauthFlow),
		hc:    &http.Client{Timeout: 15 * time.Second},
	}
}

// SetOnAuthorized 注册授权成功回调（重连触发器）。
func (m *OAuthManager) SetOnAuthorized(fn func(serverName string)) {
	m.mu.Lock()
	m.onAuthorized = fn
	m.mu.Unlock()
}

// Status 返回连接器的 OAuth 状态，供 UI 轮询。
func (m *OAuthManager) Status(name string) OAuthFlowStatus {
	if tok, ok := m.store.Get(name); ok && tok.Valid() {
		return OAuthFlowStatus{State: OAuthStateAuthorized}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	flow, ok := m.flows[name]
	if !ok {
		// 有 token 但已过期：明确告诉用户"该重新授权"而不是笼统的 none。
		if tok, ok := m.store.Get(name); ok && tok.AccessToken != "" {
			return OAuthFlowStatus{State: "expired"}
		}
		return OAuthFlowStatus{State: "none"}
	}
	select {
	case <-flow.done:
		if flow.flowErr != nil {
			return OAuthFlowStatus{State: OAuthStateFailed, Error: flow.flowErr.Error(), RedirectURI: flow.redirectURI}
		}
		// done 但没有 token 也没有错误：极端竞态，按失败处理。
		return OAuthFlowStatus{State: OAuthStateFailed, Error: "authorization flow ended without a token", RedirectURI: flow.redirectURI}
	default:
		return OAuthFlowStatus{State: OAuthStateAwaitingAuthorization, RedirectURI: flow.redirectURI}
	}
}

// StartOAuth 发起一次完整授权，返回浏览器要打开的 authorize URL。
//
// 返回 ("", nil) 表示已经有有效 token，无需再授权（幂等，UI 可据此直接刷新）。
func (m *OAuthManager) StartOAuth(serverName string, cfg ServerConfig) (string, error) {
	if m.store == nil {
		return "", WrapKind(ErrKindAuth, fmt.Errorf("OAuth token store is not configured"))
	}
	if cfg.Transport != TransportHTTP && cfg.Transport != TransportSSE {
		return "", fmt.Errorf("OAuth only applies to remote (http/sse) connectors")
	}
	if u, err := url.Parse(cfg.URL); err != nil || u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid connector URL %q", cfg.URL)
	}
	if tok, ok := m.store.Get(serverName); ok && tok.Valid() {
		return "", nil
	}

	m.mu.Lock()
	if flow, ok := m.flows[serverName]; ok {
		select {
		case <-flow.done:
			// 上一次流程已结束（成功或失败），清理后重新发起。
			m.closeFlowLocked(flow)
			delete(m.flows, serverName)
		default:
			url := flow.authorizeURL
			m.mu.Unlock()
			return url, nil
		}
	}
	m.mu.Unlock()

	flow, err := m.buildFlow(serverName, cfg)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	m.flows[serverName] = flow
	m.mu.Unlock()

	go flow.server.Serve(flow.listener)
	go m.watchdog(serverName, flow)

	log.Infof("[MCP] OAuth flow started for %q (redirect %s)", serverName, flow.redirectURI)
	return flow.authorizeURL, nil
}

// buildFlow 完成 发现 → 注册 → 起 listener → 拼 authorize URL。
// 不持有 m.mu（网络往返可能很慢，不能堵住 Status 轮询）。
func (m *OAuthManager) buildFlow(serverName string, cfg ServerConfig) (*oauthFlow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ocfg := cfg.OAuth.cloneOAuth()
	if ocfg == nil {
		ocfg = &OAuthConfig{}
	}

	// 1) 发现：优先按受保护资源元数据找 AS（MCP 规范路径），
	//    找不到就把 MCP 端点本身当 AS（自托管一体化部署的常见形态）。
	asURL := ""
	resource := ocfg.Resource
	var scopeCandidates []string
	if meta := discoverProtectedResource(ctx, m.hc, cfg.URL); meta != nil {
		asURL = meta.AuthorizationServers[0]
		if resource == "" {
			resource = meta.Resource
		}
		scopeCandidates = meta.ScopesSupported
	}
	if asURL == "" {
		if u, err := url.Parse(cfg.URL); err == nil {
			asURL = u.Scheme + "://" + u.Host
		}
	}
	asMeta, err := discoverAuthorizationServer(ctx, m.hc, asURL)
	if err != nil {
		return nil, WrapKind(ErrKindAuth, fmt.Errorf("OAuth discovery failed for %s: %w", asURL, err))
	}

	// 2) scope：用户配置 > 资源元数据 > AS 元数据。
	scopes := ocfg.Scopes
	if len(scopes) == 0 {
		scopes = scopeCandidates
	}
	if len(scopes) == 0 {
		scopes = asMeta.ScopesSupported
	}
	scope := strings.Join(scopes, " ")

	// 3) 本机回调 listener。端口必须先定下来，DCR 和 authorize 都要用。
	port := ocfg.RedirectPort
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("cannot bind OAuth callback listener on port %d: %w", port, err)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d%s", listener.Addr().(*net.TCPAddr).Port, oauthCallbackPath)

	// 4) 客户端身份：预配 client_id 优先，否则 DCR。
	clientID, clientSecret := ocfg.ClientID, ocfg.ClientSecret
	if clientID == "" {
		if asMeta.RegistrationEndpoint == "" {
			listener.Close()
			return nil, WrapKind(ErrKindAuth, fmt.Errorf(
				"server %s does not advertise dynamic client registration; configure client_id (and client_secret) on this connector", asURL))
		}
		dcr, err := registerClient(ctx, m.hc, asMeta.RegistrationEndpoint, redirectURI, scope)
		if err != nil {
			listener.Close()
			return nil, WrapKind(ErrKindAuth, err)
		}
		clientID, clientSecret = dcr.ClientID, dcr.ClientSecret
	}

	// 5) state + PKCE。
	state := randomToken(24)
	verifier, challenge := pkcePair()

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if scope != "" {
		q.Set("scope", scope)
	}
	if resource != "" {
		// RFC 8707：把"我要访问的是哪个 API"显式告诉 AS，防止 token 被挪用。
		q.Set("resource", resource)
	}
	authorizeURL := asMeta.AuthorizationEndpoint + "?" + q.Encode()

	flow := &oauthFlow{
		serverName:   serverName,
		serverURL:    cfg.URL,
		authorizeURL: authorizeURL,
		redirectURI:  redirectURI,
		state:        state,
		verifier:     verifier,
		clientID:     clientID,
		clientSecret: clientSecret,
		tokenURL:     asMeta.TokenEndpoint,
		resource:     resource,
		listener:     listener,
		done:         make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(oauthCallbackPath, m.handleCallback(flow))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "go-magic OAuth callback endpoint", http.StatusNotFound)
	})
	flow.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return flow, nil
}

// watchdog 兜底：用户一直不完成授权时关掉 listener，避免端口和 goroutine 泄漏。
func (m *OAuthManager) watchdog(serverName string, flow *oauthFlow) {
	timer := time.NewTimer(oauthFlowTimeout)
	defer timer.Stop()
	select {
	case <-flow.done:
		return // 流程已正常/失败收尾，没有超时这回事
	case <-timer.C:
	}

	m.mu.Lock()
	flow.flowErr = fmt.Errorf("authorization timed out after %s", oauthFlowTimeout)
	m.closeFlowLocked(flow)
	select {
	case <-flow.done:
	default:
		close(flow.done)
	}
	delete(m.flows, serverName)
	m.mu.Unlock()
}

// closeFlowLocked 关闭 listener 与 http.Server。调用方必须持有 m.mu。
func (m *OAuthManager) closeFlowLocked(flow *oauthFlow) {
	if flow.listener != nil {
		flow.listener.Close()
		flow.listener = nil
	}
	if flow.server != nil {
		flow.server.Close()
		flow.server = nil
	}
}

// handleCallback 返回浏览器跳转的处理器：校验 state → code 换 token →
// 落盘 → 通知上层重连。
//
// 顺序敏感：必须先把 HTTP 响应写给浏览器、**再**收尾关 listener/server。
// 反过来 server.Close() 会掐断这条正在写的连接，浏览器看到的是一面
// "连接被重置"，授权明明成功了却像失败。
func (m *OAuthManager) handleCallback(flow *oauthFlow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			desc := q.Get("error_description")
			http.Error(w, "Authorization failed: "+errParam, http.StatusBadRequest)
			m.finishFlow(flow, fmt.Errorf("authorization server rejected the request: %s (%s)", errParam, desc))
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			m.finishFlow(flow, fmt.Errorf("authorization callback missing code parameter"))
			return
		}
		if q.Get("state") != flow.state {
			// state 不匹配最可能是 CSRF 或者另一个浏览器标签页的旧流程。
			http.Error(w, "state mismatch", http.StatusBadRequest)
			m.finishFlow(flow, fmt.Errorf("state mismatch in authorization callback"))
			return
		}

		tok, err := m.exchangeCode(flow, code)
		if err != nil {
			http.Error(w, "Token exchange failed: "+err.Error(), http.StatusBadGateway)
			m.finishFlow(flow, err)
			return
		}
		if err := m.store.Save(flow.serverName, *tok); err != nil {
			http.Error(w, "failed to persist token", http.StatusInternalServerError)
			m.finishFlow(flow, fmt.Errorf("failed to persist OAuth token: %w", err))
			return
		}

		log.Infof("[MCP] OAuth authorization completed for %q", flow.serverName)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><body style="font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
<p>Authorization complete. You can close this tab and go back to go-magic.</p></body></html>`)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// 响应已经写完，现在才能安全地收尾。
		m.finishFlow(flow, nil)

		// 触发上层重连（异步：handler 要先返回，浏览器别等握手）。
		m.mu.Lock()
		fn := m.onAuthorized
		m.mu.Unlock()
		if fn != nil {
			name := flow.serverName
			go fn(name)
		}
	}
}

// finishFlow 收尾一次流程：关闭 listener、清掉 map（失败/成功都收尾）。
// 多次调用安全（handleCallback 与 watchdog 竞争）。
func (m *OAuthManager) finishFlow(flow *oauthFlow, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		flow.flowErr = err
	}
	m.closeFlowLocked(flow)
	select {
	case <-flow.done: // 已关闭
	default:
		close(flow.done)
	}
	if cur, ok := m.flows[flow.serverName]; ok && cur == flow {
		delete(m.flows, flow.serverName)
	}
}

// exchangeCode 用授权码 + PKCE verifier 换 token。
func (m *OAuthManager) exchangeCode(flow *oauthFlow, code string) (*OAuthToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", flow.redirectURI)
	form.Set("client_id", flow.clientID)
	form.Set("code_verifier", flow.verifier)
	if flow.resource != "" {
		form.Set("resource", flow.resource)
	}
	tok, err := doTokenRequest(flow.tokenURL, form, flow.clientID, flow.clientSecret, flow.resource)
	if err != nil {
		return nil, err
	}
	// 冗余进 token 记录：刷新时不需要重新发现/注册。
	tok.TokenEndpoint = flow.tokenURL
	tok.ClientID = flow.clientID
	tok.ClientSecret = flow.clientSecret
	return tok, nil
}

// refreshAccessToken 用 refresh_token 续期（grant_type=refresh_token）。
func refreshAccessToken(tok *OAuthToken, resource string) (*OAuthToken, error) {
	if tok == nil || tok.RefreshToken == "" || tok.TokenEndpoint == "" {
		return nil, WrapKind(ErrKindAuth, fmt.Errorf("no refresh token available; re-authorization is required"))
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", tok.RefreshToken)
	form.Set("client_id", tok.ClientID)
	if tok.Scope != "" {
		form.Set("scope", tok.Scope)
	}
	if resource != "" {
		form.Set("resource", resource)
	}
	return doTokenRequest(tok.TokenEndpoint, form, tok.ClientID, tok.ClientSecret, resource)
}

// doTokenRequest POST 表单到 token endpoint。机密客户端用 Basic 认证。
func doTokenRequest(tokenURL string, form url.Values, clientID, clientSecret, resource string) (*OAuthToken, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if clientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
	}

	hc := &http.Client{Timeout: 15 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, WrapKind(ErrKindNetwork, classifyHTTPError(err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		// RFC 6749 §5.2：错误在 JSON body 的 error 字段里。
		var terr struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		msg := string(data)
		if json.Unmarshal(data, &terr) == nil && terr.Error != "" {
			msg = terr.Error
			if terr.ErrorDescription != "" {
				msg += ": " + terr.ErrorDescription
			}
		}
		return nil, WrapKind(ErrKindAuth, fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, truncateForLog(msg)))
	}
	var payload struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		TokenType        string `json:"token_type"`
		ExpiresIn        int64  `json:"expires_in"`
		Scope            string `json:"scope"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, WrapKind(ErrKindProtocol, fmt.Errorf("invalid token response: %w", err))
	}
	if payload.Error != "" || payload.AccessToken == "" {
		msg := payload.Error
		if msg == "" {
			msg = "no access_token in response"
		}
		if payload.ErrorDescription != "" {
			msg += ": " + payload.ErrorDescription
		}
		return nil, WrapKind(ErrKindAuth, fmt.Errorf("%s", msg))
	}
	out := &OAuthToken{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		TokenType:    payload.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second),
		Scope:        payload.Scope,
		Resource:     resource,
	}
	return out, nil
}

// truncateForLog 限制错误消息长度，防止超长响应刷爆日志。
func truncateForLog(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// ---------------------------------------------------------------------------
// 连接期 token 获取（EnsureToken）+ 传输层 provider
// ---------------------------------------------------------------------------

// EnsureToken 返回当前可用的 access token：有效直接用；过期走刷新；
// 什么都没有就报 auth 类错误（UI 据此亮"去授权"按钮）。
func (m *OAuthManager) EnsureToken(serverName string) (string, error) {
	if m == nil || m.store == nil {
		return "", WrapKind(ErrKindAuth, fmt.Errorf("OAuth token store is not configured"))
	}
	tok, ok := m.store.Get(serverName)
	if ok && tok.Valid() {
		return tok.AuthHeader(), nil
	}
	if ok && tok.RefreshToken != "" {
		newTok, err := refreshAccessToken(tok, tok.Resource)
		if err == nil {
			// 刷新成功：保留服务端可能"旋转"过的新 refresh_token。
			if newTok.RefreshToken == "" {
				newTok.RefreshToken = tok.RefreshToken
			}
			// 冗余字段沿用旧值，刷新响应里没有。
			newTok.TokenEndpoint = tok.TokenEndpoint
			newTok.ClientID = tok.ClientID
			newTok.ClientSecret = tok.ClientSecret
			if err := m.store.Save(serverName, *newTok); err != nil {
				log.Warnf("[MCP] failed to persist refreshed token for %q: %v", serverName, err)
			}
			log.Infof("[MCP] OAuth token refreshed for %q", serverName)
			return newTok.AuthHeader(), nil
		}
		log.Warnf("[MCP] token refresh failed for %q: %v", serverName, err)
		return "", err
	}
	return "", WrapKind(ErrKindAuth, fmt.Errorf(
		"no OAuth credentials for %q — complete the authorization flow in the connectors page", serverName))
}

// SignOut 删除 token 并终止进行中的流程（登出）。
func (m *OAuthManager) SignOut(serverName string) error {
	m.mu.Lock()
	if flow, ok := m.flows[serverName]; ok {
		flow.flowErr = fmt.Errorf("signed out")
		m.closeFlowLocked(flow)
		select {
		case <-flow.done:
		default:
			close(flow.done)
		}
		delete(m.flows, serverName)
	}
	m.mu.Unlock()
	if m.store == nil {
		return nil
	}
	return m.store.Delete(serverName)
}

// oauthProvider 是 AuthTokenProvider 的实现，绑定单个连接器。
type oauthProvider struct {
	mgr  *OAuthManager
	name string
}

func (p *oauthProvider) CurrentToken(ctx context.Context) (string, error) {
	return p.mgr.EnsureToken(p.name)
}

// RefreshToken 强制续期：401 重试路径专用。没有 refresh token 时返回
// 原样错误，让上层把 401 透传给用户（而不是吞掉变成神秘失败）。
func (p *oauthProvider) RefreshToken(ctx context.Context) (string, error) {
	tok, ok := p.mgr.store.Get(p.name)
	if !ok || tok.RefreshToken == "" {
		return "", WrapKind(ErrKindAuth, fmt.Errorf(
			"server rejected the token and no refresh token is available — re-authorization required"))
	}
	newTok, err := refreshAccessToken(tok, tok.Resource)
	if err != nil {
		return "", err
	}
	if newTok.RefreshToken == "" {
		newTok.RefreshToken = tok.RefreshToken
	}
	newTok.TokenEndpoint = tok.TokenEndpoint
	newTok.ClientID = tok.ClientID
	newTok.ClientSecret = tok.ClientSecret
	if err := p.mgr.store.Save(p.name, *newTok); err != nil {
		log.Warnf("[MCP] failed to persist refreshed token for %q: %v", p.name, err)
	}
	return newTok.AuthHeader(), nil
}

// Provider 返回绑定指定连接器的传输层 token 提供者。
func (m *OAuthManager) Provider(serverName string) AuthTokenProvider {
	return &oauthProvider{mgr: m, name: serverName}
}
