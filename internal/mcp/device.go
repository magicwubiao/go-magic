package mcp

// OAuth 2.0 设备授权流程（RFC 8628）。
//
// 为什么 CLI 连接器需要单独的流程：授权码 + 本机回调要求"能开浏览器、能监听
// 端口"，而 CLI 工具的 token 往往只需要一枚字符串（GH_TOKEN / VERCEL_TOKEN）。
// 设备码里用户只做两件事——打开一个 URL、输入一段短码，其余全由本程序完成，
// 因此没有 TTY、没有浏览器、跑在远端机器上都成立。
//
// 与 MCP 的授权码流程共用同一份 OAuthTokenStore（键 `cliconn:<connector>`），
// 因此刷新、登出、状态查询都复用 OAuthManager 的实现，不新增第二套凭据文件。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// DeviceAuthorization 是设备授权端点的响应。
type DeviceAuthorization struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	Error                   string `json:"error,omitempty"`
	ErrorDescription        string `json:"error_description,omitempty"`
}

// DeviceFlowConfig 是一次设备码流程的输入。
type DeviceFlowConfig struct {
	DeviceAuthorizationEndpoint string
	TokenEndpoint               string
	ClientID                    string
	ClientSecret                string
	Scopes                      []string
	Audience                    string
}

// 设备码轮询的错误码（RFC 8628 §3.5）。
const (
	deviceErrPending  = "authorization_pending"
	deviceErrSlowDown = "slow_down"
	deviceErrExpired  = "expired_token"
	deviceErrDenied   = "access_denied"
)

const (
	// deviceDefaultTTL 是设备码有效期兜底（服务端不给 expires_in 时）。
	deviceDefaultTTL = 10 * time.Minute
)

// devicePollFloor 是轮询间隔下限：服务端给 0/负数时也不能打成忙等。
// 变量而非常量，测试要把它调到毫秒级。
var devicePollFloor = 5 * time.Second

// StartDeviceAuthorization 发起一次设备码授权，返回用户需要看到的短码与 URL。
//
// 返回 (nil, nil) 表示已经有可用 token，无需再授权（幂等，UI 可直接刷新）。
// 轮询在后台进行，结果通过 Status(name) 暴露（authorized / failed）。
func (m *OAuthManager) StartDeviceAuthorization(name string, cfg DeviceFlowConfig) (*DeviceAuthorization, error) {
	if m == nil || m.store == nil {
		return nil, WrapKind(ErrKindAuth, fmt.Errorf("OAuth token store is not configured"))
	}
	if tok, ok := m.store.Get(name); ok && tok.Valid() {
		return nil, nil
	}

	m.mu.Lock()
	if flow, ok := m.flows[name]; ok {
		select {
		case <-flow.done:
			m.closeFlowLocked(flow)
			delete(m.flows, name)
		default:
			// 流程还在等用户输入短码：把同一份短码回给前端，别让用户
			// 因为刷新页面就拿到一个作废的新码。
			dc := flow.deviceAuth
			m.mu.Unlock()
			return dc, nil
		}
	}
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dc, err := requestDeviceCode(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if dc.ExpiresIn <= 0 {
		dc.ExpiresIn = int(deviceDefaultTTL / time.Second)
	}
	if dc.Interval <= 0 {
		dc.Interval = int(devicePollFloor / time.Second)
	}

	flow := &oauthFlow{
		serverName:      name,
		redirectURI:     dc.VerificationURI,
		userCode:        dc.UserCode,
		verificationURI: dc.VerificationURIComplete,
		deviceAuth:      dc,
		tokenURL:        cfg.TokenEndpoint,
		clientID:        cfg.ClientID,
		clientSecret:    cfg.ClientSecret,
		done:            make(chan struct{}),
	}
	if flow.verificationURI == "" {
		flow.verificationURI = dc.VerificationURI
	}

	m.mu.Lock()
	delete(m.lastResult, name) // 新一轮授权，旧的失败原因作废
	m.flows[name] = flow
	m.mu.Unlock()

	go m.pollDeviceToken(flow, cfg, dc)

	log.Infof("[CLI] device authorization started for %q (code %s)", name, dc.UserCode)
	return dc, nil
}

// requestDeviceCode POST 设备授权端点。
func requestDeviceCode(ctx context.Context, cfg DeviceFlowConfig) (*DeviceAuthorization, error) {
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	if len(cfg.Scopes) > 0 {
		form.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	if cfg.Audience != "" {
		form.Set("audience", cfg.Audience)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.DeviceAuthorizationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	hc := &http.Client{Timeout: 15 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, WrapKind(ErrKindNetwork, classifyHTTPError(err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	var dc DeviceAuthorization
	if err := json.Unmarshal(data, &dc); err != nil {
		return nil, WrapKind(ErrKindProtocol, fmt.Errorf("invalid device authorization response (HTTP %d)", resp.StatusCode))
	}
	if resp.StatusCode != http.StatusOK || dc.Error != "" || dc.DeviceCode == "" {
		msg := dc.Error
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		if dc.ErrorDescription != "" {
			msg += ": " + dc.ErrorDescription
		}
		return nil, WrapKind(ErrKindAuth, fmt.Errorf("device authorization failed: %s", truncateForLog(msg)))
	}
	if dc.VerificationURI == "" && dc.VerificationURIComplete == "" {
		return nil, WrapKind(ErrKindProtocol, fmt.Errorf("device authorization response has no verification_uri; cannot tell the user where to approve"))
	}
	return &dc, nil
}

// pollDeviceToken 按服务端给的 interval 轮询，直到拿到 token / 用户拒绝 / 过期。
//
// 失败只记录在 flow.flowErr 上，不 panic、不重试到天荒地老：用户点开页面
// 大概率就在旁边等着，超过 TTL 还没批就是"确实没人批"。
func (m *OAuthManager) pollDeviceToken(flow *oauthFlow, cfg DeviceFlowConfig, dc *DeviceAuthorization) {
	interval := time.Duration(dc.Interval) * time.Second
	if interval < devicePollFloor {
		interval = devicePollFloor
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)

	for {
		select {
		case <-flow.done:
			return // 被登出 / 被新流程替换 / 已完成
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			m.finishFlow(flow, fmt.Errorf("device code expired before it was approved"))
			return
		}

		tok, retry, err := m.pollDeviceOnce(flow, cfg, dc.DeviceCode)
		switch {
		case err == nil:
			if saveErr := m.store.Save(flow.serverName, *tok); saveErr != nil {
				m.finishFlow(flow, fmt.Errorf("failed to persist OAuth token: %w", saveErr))
				return
			}
			log.Infof("[CLI] device authorization completed for %q", flow.serverName)
			m.finishFlow(flow, nil)
			m.mu.Lock()
			fn := m.onAuthorized
			m.mu.Unlock()
			if fn != nil {
				name := flow.serverName
				go fn(name)
			}
			return
		case errors.Is(err, errDevicePending):
			// 用户还没点"允许"，继续等。
			continue
		case errors.Is(err, errDeviceSlowDown):
			// 服务端要求降速：RFC 8628 规定每次 +5s。
			interval += 5 * time.Second
			continue
		default:
			_ = retry
			m.finishFlow(flow, err)
			return
		}
	}
}

// errDevicePending / errDeviceSlowDown 是轮询里的"继续等"信号，
// 不能被当成失败收尾。
var (
	errDevicePending  = errors.New("authorization_pending")
	errDeviceSlowDown = errors.New("slow_down")
)

// pollDeviceOnce 发一次 token 请求。
func (m *OAuthManager) pollDeviceOnce(flow *oauthFlow, cfg DeviceFlowConfig, deviceCode string) (*OAuthToken, bool, error) {
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	form.Set("device_code", deviceCode)
	form.Set("client_id", cfg.ClientID)

	tok, err := doTokenRequest(flow.tokenURL, form, cfg.ClientID, cfg.ClientSecret, cfg.Audience)
	if err == nil {
		tok.TokenEndpoint = cfg.TokenEndpoint
		tok.ClientID = cfg.ClientID
		tok.ClientSecret = cfg.ClientSecret
		return tok, false, nil
	}

	// doTokenRequest 把错误码拼进了消息里（RFC 6749 §5.2 的 error 字段）。
	// 设备码流程要区分"继续等"和"真的失败"，只能看这个字符串。
	msg := err.Error()
	switch {
	case strings.Contains(msg, deviceErrPending):
		return nil, true, errDevicePending
	case strings.Contains(msg, deviceErrSlowDown):
		return nil, true, errDeviceSlowDown
	case strings.Contains(msg, deviceErrExpired):
		return nil, false, fmt.Errorf("device code expired before it was approved")
	case strings.Contains(msg, deviceErrDenied):
		return nil, false, fmt.Errorf("authorization was denied by the user")
	default:
		return nil, false, err
	}
}

// EnsureAccessToken 返回可用的原始 access token（不带 "Bearer " 前缀）。
//
// CLI 的 token 环境变量要的是裸串（GH_TOKEN=ghp_xxx），而 MCP 传输层要的是
// 完整 Authorization 头。两者共用同一份存储，只在出口处拆包。
func (m *OAuthManager) EnsureAccessToken(name string) (string, error) {
	header, err := m.EnsureToken(name)
	if err != nil {
		return "", err
	}
	if i := strings.IndexByte(header, ' '); i >= 0 {
		return header[i+1:], nil
	}
	return header, nil
}
