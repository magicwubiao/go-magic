package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// ============================================================================
// HTTP Client
// ============================================================================

// httpClient wraps net/http.Client with common functionality.
type httpClient struct {
	client *http.Client
}

// newHTTPClient creates an HTTP client, optionally with a proxy.
func newHTTPClient(proxy string) *httpClient {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &httpClient{
		client: &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second,
		},
	}
}

// ============================================================================
// iLink API Client
// ============================================================================

// ILinkAPIClient communicates with the Tencent iLink Bot REST API.
type ILinkAPIClient struct {
	BaseURL    string
	Token      string
	HttpClient *http.Client
}

// NewILinkAPIClient creates a new iLink API client.
func NewILinkAPIClient(baseURL, token, proxy string) (*ILinkAPIClient, error) {
	if baseURL == "" {
		baseURL = ilinkDefaultBaseURL
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", proxy, err)
		}
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			transport := defaultTransport.Clone()
			transport.Proxy = http.ProxyURL(proxyURL)
			client.Transport = transport
		}
	}

	return &ILinkAPIClient{
		BaseURL:    baseURL,
		Token:      token,
		HttpClient: client,
	}, nil
}

// doPost sends a POST request to the iLink API.
func (c *ILinkAPIClient) doPost(ctx context.Context, endpoint string, body interface{}, responseObj interface{}) error {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	u.Path = path.Join(u.Path, endpoint)

	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header["iLink-App-Id"] = []string{ilinkAppID}
	req.Header["iLink-App-ClientVersion"] = []string{strconv.Itoa(ilinkClientVersion)}

	// Skip auth headers for QR code endpoints
	if endpoint != "ilink/bot/get_bot_qrcode" && endpoint != "ilink/bot/get_qrcode_status" {
		req.Header["AuthorizationType"] = []string{"ilink_bot_token"}
		req.Header["X-WECHAT-UIN"] = []string{randomWechatUIN()}
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
	}

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http POST %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d %s: %s", resp.StatusCode, resp.Status, string(respBody))
	}

	if responseObj != nil {
		if err := json.Unmarshal(respBody, responseObj); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w, body: %s", err, string(respBody))
		}
	}

	return nil
}

// doGet sends a GET request to the iLink API.
func (c *ILinkAPIClient) doGet(ctx context.Context, endpoint string, query map[string]string, responseObj interface{}) error {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return err
	}
	u.Path = path.Join(u.Path, endpoint)

	q := u.Query()
	for key, value := range query {
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}

	req.Header["iLink-App-Id"] = []string{ilinkAppID}
	req.Header["iLink-App-ClientVersion"] = []string{strconv.Itoa(ilinkClientVersion)}

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s failed: %d %s", endpoint, resp.StatusCode, string(respBody))
	}

	if responseObj != nil {
		if err := json.Unmarshal(respBody, responseObj); err != nil {
			return err
		}
	}

	return nil
}

// ============================================================================
// API Methods
// ============================================================================

// GetUpdates long-polls for new messages.
func (c *ILinkAPIClient) GetUpdates(ctx context.Context, req ILinkGetUpdatesReq) (*ILinkGetUpdatesResp, error) {
	req.BaseInfo = ILinkBaseInfo{ChannelVersion: ilinkChannelVersion}
	var resp ILinkGetUpdatesResp
	if err := c.doPost(ctx, "ilink/bot/getupdates", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendMessage sends a message to a WeChat user.
func (c *ILinkAPIClient) SendMessage(ctx context.Context, req ILinkSendMessageReq) error {
	req.BaseInfo = ILinkBaseInfo{ChannelVersion: ilinkChannelVersion}
	var resp ILinkSendMessageResp
	if err := c.doPost(ctx, "ilink/bot/sendmessage", req, &resp); err != nil {
		return err
	}
	if resp.Ret != 0 || resp.Errcode != 0 {
		return fmt.Errorf("sendmessage failed: ret=%d errcode=%d errmsg=%s",
			resp.Ret, resp.Errcode, resp.Errmsg)
	}
	return nil
}

// GetConfig retrieves bot configuration (typing ticket, etc.).
func (c *ILinkAPIClient) GetConfig(ctx context.Context, req ILinkGetConfigReq) (*ILinkGetConfigResp, error) {
	req.BaseInfo = ILinkBaseInfo{ChannelVersion: ilinkChannelVersion}
	var resp ILinkGetConfigResp
	if err := c.doPost(ctx, "ilink/bot/getconfig", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendTyping sends a typing indicator.
func (c *ILinkAPIClient) SendTyping(ctx context.Context, req ILinkSendTypingReq) error {
	req.BaseInfo = ILinkBaseInfo{ChannelVersion: ilinkChannelVersion}
	var resp ILinkSendTypingResp
	if err := c.doPost(ctx, "ilink/bot/sendtyping", req, &resp); err != nil {
		return err
	}
	if resp.Ret != 0 || resp.Errcode != 0 {
		return fmt.Errorf("sendtyping failed: ret=%d errcode=%d errmsg=%s",
			resp.Ret, resp.Errcode, resp.Errmsg)
	}
	return nil
}

// GetUploadURL gets a CDN upload URL for media.
func (c *ILinkAPIClient) GetUploadURL(ctx context.Context, req ILinkGetUploadURLReq) (*ILinkGetUploadURLResp, error) {
	req.BaseInfo = ILinkBaseInfo{ChannelVersion: ilinkChannelVersion}
	var resp ILinkGetUploadURLResp
	if err := c.doPost(ctx, "ilink/bot/getuploadurl", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetQRCode gets a QR code for bot login.
func (c *ILinkAPIClient) GetQRCode(ctx context.Context, botType string) (*ILinkQRCodeResponse, error) {
	var qrcodeResp ILinkQRCodeResponse
	if err := c.doGet(ctx, "ilink/bot/get_bot_qrcode", map[string]string{
		"bot_type": botType,
	}, &qrcodeResp); err != nil {
		return nil, err
	}
	return &qrcodeResp, nil
}

// GetQRCodeStatus polls the QR code scanning status.
func (c *ILinkAPIClient) GetQRCodeStatus(ctx context.Context, qrcode string) (*ILinkStatusResponse, error) {
	var statusResp ILinkStatusResponse
	if err := c.doGet(ctx, "ilink/bot/get_qrcode_status", map[string]string{
		"qrcode": qrcode,
	}, &statusResp); err != nil {
		return nil, err
	}
	return &statusResp, nil
}

// ============================================================================
// Helpers
// ============================================================================

// randomWechatUIN generates a random WX UIN for header.
func randomWechatUIN() string {
	return strconv.FormatInt(time.Now().UnixNano()%100000000, 10)
}

// CDN helpers

// BuildCDNDownloadURL constructs the CDN download URL from base URL and encrypted query param.
func BuildCDNDownloadURL(base, encryptedQueryParam string) string {
	return strings.TrimRight(base, "/") +
		"/download?encrypted_query_param=" + url.QueryEscape(encryptedQueryParam)
}

// BuildCDNUploadURL constructs the CDN upload URL.
func BuildCDNUploadURL(base, uploadParam, filekey string) string {
	return strings.TrimRight(base, "/") +
		"/upload?encrypted_query_param=" + url.QueryEscape(uploadParam) +
		"&filekey=" + url.QueryEscape(filekey)
}
