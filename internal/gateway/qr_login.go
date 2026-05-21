package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/pkg/log"
	qrcode "github.com/skip2/go-qrcode"
)

// QRCodeManager handles QR code generation and login for all platforms
type QRCodeManager struct {
	mu          sync.RWMutex
	codes       map[string]*QRCodeSession // platform -> session
	cleanup     chan string
	pollers     map[string]*QRPollContext // active pollers
}

// QRPollContext holds the context for QR status polling
type QRPollContext struct {
	Cancel  context.CancelFunc
	Timeout time.Duration
}

// QRCodeSession represents an active QR code login session
type QRCodeSession struct {
	ID        string                 `json:"id"`
	Platform  string                 `json:"platform"`
	Status    string                 `json:"status"` // pending, scanning, confirmed, expired, error
	QRCode    string                 `json:"qr_code,omitempty"` // base64 encoded PNG
	QRData    string                 `json:"qr_data,omitempty"` // raw QR data string
	Message   string                 `json:"message,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	ExpiresAt time.Time              `json:"expires_at"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

var globalQRManager *QRCodeManager
var qrManagerOnce sync.Once

// GetQRManager returns the global QR code manager instance
func GetQRManager() *QRCodeManager {
	qrManagerOnce.Do(func() {
		globalQRManager = &QRCodeManager{
			codes:   make(map[string]*QRCodeSession),
			cleanup: make(chan string, 10),
			pollers: make(map[string]*QRPollContext),
		}
		go globalQRManager.cleanupLoop()
	})
	return globalQRManager
}

// cleanupLoop periodically removes expired QR codes
func (m *QRCodeManager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case platform := <-m.cleanup:
			m.mu.Lock()
			if session, ok := m.codes[platform]; ok && session.Status == "expired" {
				delete(m.codes, platform)
				log.Debugf("Cleaned up expired QR code for platform: %s", platform)
			}
			// Cancel any active poller
			if poller, ok := m.pollers[platform]; ok {
				poller.Cancel()
				delete(m.pollers, platform)
			}
			m.mu.Unlock()
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			for platform, session := range m.codes {
				if now.After(session.ExpiresAt) && session.Status != "confirmed" {
					session.Status = "expired"
					session.Message = "QR code expired. Please refresh."
					m.cleanup <- platform
				}
			}
			m.mu.Unlock()
		}
	}
}

// PlatformQRGenerator defines the interface for platforms that can generate QR codes
type PlatformQRGenerator interface {
	GenerateQRCode(ctx context.Context) (qrData string, err error)
	PollQRStatus(ctx context.Context, qrData string) (status string, err error)
}

// CreateSession creates a new QR code session for a platform
// qrData is the raw QR data string (used for status polling)
// qrImage is an optional base64 image (if empty, will be generated from qrData)
func (m *QRCodeManager) CreateSession(platform string, qrData string, qrImage string) (*QRCodeSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel any existing poller for this platform
	if poller, ok := m.pollers[platform]; ok {
		poller.Cancel()
	}

	session := &QRCodeSession{
		ID:        uuid.New().String(),
		Platform:  platform,
		Status:    "pending",
		QRData:    qrData,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(120 * time.Second), // 2 minutes expiry
		Metadata:  make(map[string]interface{}),
	}

	// Use provided image or generate from qrData
	if qrImage != "" {
		session.QRCode = qrImage
	} else if strings.HasPrefix(qrData, "data:image/") {
		session.QRCode = qrData
	} else {
		img, err := GenerateQRCodePNG(qrData)
		if err != nil {
			session.Status = "error"
			session.Message = fmt.Sprintf("Failed to generate QR code: %v", err)
			log.Errorf("Failed to generate QR code for %s: %v", platform, err)
			return session, nil
		}
		session.QRCode = img
	}
	session.Message = "Please scan the QR code with your app"

	m.codes[platform] = session
	log.Infof("Created QR code session for platform: %s, expires in 120s", platform)

	// Start polling for status
	m.startPoller(platform, session)

	return session, nil
}

// startPoller starts a background poller for QR status
func (m *QRCodeManager) startPoller(platform string, session *QRCodeSession) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	m.pollers[platform] = &QRPollContext{Cancel: cancel, Timeout: 3 * time.Minute}

	go func() {
		defer cancel()

		// Initial delay before first poll
		time.Sleep(2 * time.Second)

		pollInterval := time.NewTicker(2 * time.Second)
		defer pollInterval.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-pollInterval.C:
				status, err := m.pollPlatformStatus(ctx, platform)
				if err != nil {
					log.Debugf("Poll error for %s: %v", platform, err)
					continue
				}

				m.mu.Lock()
				currentSession := m.codes[platform]
				if currentSession == nil || currentSession.ID != session.ID {
					m.mu.Unlock()
					return
				}

				if status != currentSession.Status {
					currentSession.Status = status
					switch status {
					case "scanning":
						currentSession.Message = "QR code scanned! Please confirm on your device"
						log.Infof("QR code scanned for %s", platform)
					case "confirmed":
						currentSession.Message = "Login successful!"
						currentSession.ExpiresAt = time.Now().Add(24 * time.Hour)
						log.Infof("QR login confirmed for %s", platform)
						m.mu.Unlock()
						return
					case "expired":
						currentSession.Message = "QR code expired. Please refresh."
						m.mu.Unlock()
						m.cleanup <- platform
						return
					}
				}
				m.mu.Unlock()
			}
		}
	}()
}

// pollPlatformStatus polls the platform for QR status
func (m *QRCodeManager) pollPlatformStatus(ctx context.Context, platform string) (string, error) {
	m.mu.RLock()
	session := m.codes[platform]
	m.mu.RUnlock()

	if session == nil {
		return "error", fmt.Errorf("no session found")
	}

	// Check expiry
	if time.Now().After(session.ExpiresAt) {
		return "expired", nil
	}

	// Platform-specific status polling
	switch platform {
	case "wechat_ilink":
		status, resp, err := m.pollILinkStatus(ctx, session.QRData)
		if err != nil {
			return "", err
		}
		// Save token when confirmed
		if status == "confirmed" && resp != nil && resp.BotToken != "" {
			m.saveILinkToken(resp.BotToken, resp.Baseurl)
		}
		return status, nil
	case "whatsapp":
		return m.pollGatewayStatus(ctx, "whatsapp")
	case "wechat":
		return m.pollGatewayStatus(ctx, "wechat")
	case "wecom":
		return m.pollGatewayStatus(ctx, "wecom")
	case "dingtalk":
		// DingTalk uses OAuth redirect, status changes via callback
		return session.Status, nil
	case "feishu":
		// Feishu uses OAuth redirect, status changes via callback
		return session.Status, nil
	default:
		return session.Status, nil
	}
}

// pollILinkStatus polls iLink API for QR status
// Returns status and optional token info when confirmed
func (m *QRCodeManager) pollILinkStatus(ctx context.Context, qrcode string) (string, *ILinkStatusResponse, error) {
	// Create a temporary API client
	api, err := NewILinkAPIClient(ilinkDefaultBaseURL, "", "")
	if err != nil {
		return "", nil, err
	}

	resp, err := api.GetQRCodeStatus(ctx, qrcode)
	if err != nil {
		return "", nil, err
	}

	// Map iLink status to our status
	switch resp.Status {
	case "wait":
		return "pending", resp, nil
	case "scaned", "scaned_but_redirect":
		return "scanning", resp, nil
	case "confirmed":
		return "confirmed", resp, nil
	case "expired":
		return "expired", resp, nil
	default:
		return "pending", resp, nil
	}
}

// pollWhatsAppStatus polls WhatsApp for login status
func (m *QRCodeManager) pollWhatsAppStatus(ctx context.Context) (string, error) {
	return m.pollGatewayStatus(ctx, "whatsapp")
}

// pollWeChatStatus polls WeChat for QR status
func (m *QRCodeManager) pollWeChatStatus(ctx context.Context, qrcode string) (string, error) {
	return m.pollGatewayStatus(ctx, "wechat")
}

// pollGatewayStatus checks platform login status via the gateway API
func (m *QRCodeManager) pollGatewayStatus(ctx context.Context, platform string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:8080/api/login/qr/status/%s", platform))
	if err != nil {
		// Gateway not reachable, keep current status
		return "", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	var result struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil
	}

	switch result.Status {
	case "connected", "confirmed":
		return "confirmed", nil
	case "scanning", "scaned":
		return "scanning", nil
	case "expired":
		return "expired", nil
	default:
		return "", nil
	}
}

// pollWeComStatus polls WeCom for QR status
func (m *QRCodeManager) pollWeComStatus(ctx context.Context, session *QRCodeSession) (string, error) {
	// Get WeCom credentials from metadata
	corpID, _ := session.Metadata["corp_id"].(string)
	agentID, _ := session.Metadata["agent_id"].(string)

	if corpID == "" || agentID == "" {
		return "error", fmt.Errorf("WeCom credentials not configured")
	}

	// WeCom QR login uses a ticket-based system
	// The ticket is embedded in the QR code URL
	ticket := ""
	if url, ok := session.Metadata["login_url"].(string); ok {
		// Extract ticket from URL if needed
		ticket = extractWeComTicket(url)
	}

	if ticket == "" {
		return session.Status, nil
	}

	// Try to get the scan status via WeCom API
	// WeCom login status is typically checked via callback
	return session.Status, nil
}

// extractWeComTicket extracts the ticket parameter from WeCom login URL
func extractWeComTicket(loginURL string) string {
	// WeCom QR login URL format: https://open.work.weixin.qq.com/wwopen/sso/qrConnect?appid=CORPID&agentid=AGENTID&redirect_uri=URI&state=STATE
	// Or with ticket: contains "ticket=" parameter
	return ""
}

// GenerateWeComQR generates a WeCom QR code for login
func (m *QRCodeManager) GenerateWeComQR(ctx context.Context, corpID, agentID, redirectURI string) (*QRCodeSession, error) {
	// WeCom QR login flow:
	// 1. Generate a login URL with appid, agentid, redirect_uri, state
	// 2. Create a QR code from this URL
	// 3. Poll for login status via callback or direct API

	state := uuid.New().String()

	// Build WeCom QR login URL
	loginURL := fmt.Sprintf(
		"https://open.work.weixin.qq.com/wwopen/sso/qrConnect?appid=%s&agentid=%s&redirect_uri=%s&state=%s",
		corpID, agentID, redirectURI, state,
	)

	session := &QRCodeSession{
		ID:        uuid.New().String(),
		Platform:  "wecom",
		Status:    "pending",
		QRData:    loginURL,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(120 * time.Second),
		Metadata: map[string]interface{}{
			"corp_id":    corpID,
			"agent_id":   agentID,
			"redirect":   redirectURI,
			"state":      state,
			"login_url":  loginURL,
		},
		Message: "Please scan the QR code with WeCom",
	}

	// Generate QR code image
	img, err := GenerateQRCodePNG(loginURL)
	if err != nil {
		session.Status = "error"
		session.Message = fmt.Sprintf("Failed to generate QR code: %v", err)
		return session, err
	}
	session.QRCode = img

	m.mu.Lock()
	m.codes["wecom"] = session
	m.mu.Unlock()

	log.Infof("Created WeCom QR code session, expires in 120s")

	// Start polling
	go m.pollWeComLogin(context.Background(), session)

	return session, nil
}

// pollWeComLogin polls WeCom login status
func (m *QRCodeManager) pollWeComLogin(ctx context.Context, session *QRCodeSession) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeout := time.After(3 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return
		case <-timeout:
			m.mu.Lock()
			session.Status = "expired"
			session.Message = "Login timeout. Please try again."
			m.mu.Unlock()
			return
		case <-ticker.C:
			m.mu.Lock()
			if time.Now().After(session.ExpiresAt) {
				session.Status = "expired"
				session.Message = "QR code expired. Please refresh."
				m.mu.Unlock()
				return
			}
			// WeCom login status would be updated via callback or polling
			// For now, keep in pending state until externally updated
			m.mu.Unlock()
		}
	}
}

// GenerateWhatsAppQR generates a WhatsApp QR code for login
func (m *QRCodeManager) GenerateWhatsAppQR(ctx context.Context, mode string) (*QRCodeSession, error) {
	// WhatsApp QR login requires the WhatsAppGateway to be running
	// For personal mode, it uses the WhatsApp Web-style QR code

	session := &QRCodeSession{
		ID:        uuid.New().String(),
		Platform:  "whatsapp",
		Status:    "pending",
		QRData:    fmt.Sprintf("whatsapp://login?mode=%s&ref=%s", mode, uuid.New().String()),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(120 * time.Second),
		Metadata: map[string]interface{}{
			"mode": mode,
		},
		Message: "Please scan the QR code with WhatsApp",
	}

	// Generate QR code image
	img, err := GenerateQRCodePNG(session.QRData)
	if err != nil {
		session.Status = "error"
		session.Message = fmt.Sprintf("Failed to generate QR code: %v", err)
		return session, err
	}
	session.QRCode = img

	m.mu.Lock()
	m.codes["whatsapp"] = session
	m.mu.Unlock()

	log.Infof("Created WhatsApp QR code session (mode: %s), expires in 120s", mode)

	return session, nil
}

// DingTalk QR Login
// Docs: https://open.dingtalk.com/document/orgapp/scan-qr-code-to-logon-to-an-third-party-website

// GenerateDingTalkQR generates a DingTalk QR code for login
func (m *QRCodeManager) GenerateDingTalkQR(ctx context.Context, appKey, appSecret, redirectURI string) (*QRCodeSession, error) {
	// DingTalk QR login flow:
	// 1. Get access token via OAuth2
	// 2. Create QR code scene
	// 3. Poll for login status

	// For now, create a simulated QR code with DingTalk mini-app login URL
	// In production, this would call DingTalk's QR code generation API
	state := uuid.New().String()

	// DingTalk QR login URL format
	loginURL := fmt.Sprintf(
		"https://oapi.dingtalk.com/connect/qrconnect?appid=%s&response_type=code&scope=snsapi_login&state=%s&redirect_uri=%s",
		appKey, state, redirectURI,
	)

	session := &QRCodeSession{
		ID:        uuid.New().String(),
		Platform:  "dingtalk",
		Status:    "pending",
		QRData:    loginURL,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(120 * time.Second),
		Metadata: map[string]interface{}{
			"app_key":     appKey,
			"redirect":    redirectURI,
			"state":       state,
			"login_url":   loginURL,
		},
		Message: "Please scan the QR code with DingTalk",
	}

	// Generate QR code image
	img, err := GenerateQRCodePNG(loginURL)
	if err != nil {
		session.Status = "error"
		session.Message = fmt.Sprintf("Failed to generate QR code: %v", err)
		return session, err
	}
	session.QRCode = img

	m.mu.Lock()
	m.codes["dingtalk"] = session
	m.mu.Unlock()

	log.Infof("Created DingTalk QR code session, expires in 120s")

	// Start polling for DingTalk login status
	go m.pollDingTalkLogin(appKey, state)

	return session, nil
}

// pollDingTalkLogin polls for DingTalk QR login status
func (m *QRCodeManager) pollDingTalkLogin(appKey, state string) {
	// DingTalk QR login typically completes via callback URL
	// This polling mechanism is a fallback for checking login status
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.UpdateSessionStatus("dingtalk", "expired", "QR code expired, please try again")
			return
		case <-ticker.C:
			// In production, this would check DingTalk's API for login confirmation
			// For now, simulate pending -> scanning -> expired flow
			m.mu.RLock()
			session := m.codes["dingtalk"]
			m.mu.RUnlock()

			if session == nil {
				return
			}
		}
	}
}

// Feishu QR Login
// Docs: https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/authen-v1/sns/scene

// GenerateFeishuQR generates a Feishu (Lark) QR code for login
func (m *QRCodeManager) GenerateFeishuQR(ctx context.Context, appID, appSecret, redirectURI string) (*QRCodeSession, error) {
	// Feishu QR login flow:
	// 1. Get tenant access token
	// 2. Create QR code scene
	// 3. Poll for login status

	state := uuid.New().String()

	// Feishu QR login URL format
	loginURL := fmt.Sprintf(
		"https://open.feishu.cn/open-apis/authen/v1/authorize?redirect_uri=%s&app_id=%s&state=%s",
		redirectURI, appID, state,
	)

	session := &QRCodeSession{
		ID:        uuid.New().String(),
		Platform:  "feishu",
		Status:    "pending",
		QRData:    loginURL,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(120 * time.Second),
		Metadata: map[string]interface{}{
			"app_id":     appID,
			"redirect":    redirectURI,
			"state":       state,
			"login_url":   loginURL,
		},
		Message: "Please scan the QR code with Feishu",
	}

	// Generate QR code image
	img, err := GenerateQRCodePNG(loginURL)
	if err != nil {
		session.Status = "error"
		session.Message = fmt.Sprintf("Failed to generate QR code: %v", err)
		return session, err
	}
	session.QRCode = img

	m.mu.Lock()
	m.codes["feishu"] = session
	m.mu.Unlock()

	log.Infof("Created Feishu QR code session, expires in 120s")

	// Start polling for Feishu login status
	go m.pollFeishuLogin(appID, state)

	return session, nil
}

// pollFeishuLogin polls for Feishu QR login status
func (m *QRCodeManager) pollFeishuLogin(appID, state string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.UpdateSessionStatus("feishu", "expired", "QR code expired, please try again")
			return
		case <-ticker.C:
			m.mu.RLock()
			session := m.codes["feishu"]
			m.mu.RUnlock()

			if session == nil {
				return
			}
		}
	}
}

// GetSession returns the current QR code session for a platform
func (m *QRCodeManager) GetSession(platform string) *QRCodeSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.codes[platform]
}

// UpdateSessionStatus updates the status of a QR code session
func (m *QRCodeManager) UpdateSessionStatus(platform string, status string, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.codes[platform]; ok {
		session.Status = status
		if message != "" {
			session.Message = message
		}
		if status == "confirmed" {
			session.ExpiresAt = time.Now().Add(24 * time.Hour) // Long expiry for confirmed sessions
		}
		log.Infof("Updated QR code session status for %s: %s", platform, status)
	}
}

// ListSessions returns all active QR code sessions
func (m *QRCodeManager) ListSessions() []*QRCodeSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*QRCodeSession, 0, len(m.codes))
	now := time.Now()
	for _, session := range m.codes {
		if now.Before(session.ExpiresAt) || session.Status == "confirmed" {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

// GenerateQRCodePNG generates a proper PNG QR code image
func GenerateQRCodePNG(data string) (string, error) {
	if data == "" {
		return "", fmt.Errorf("QR data is empty")
	}

	pngBytes, err := qrcode.Encode(data, qrcode.Medium, 256)
	if err != nil {
		return "", fmt.Errorf("failed to encode QR code: %w", err)
	}

	// Convert to base64 data URL
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes), nil
}

// GenerateQRCodePNGBytes generates QR code as raw PNG bytes
func GenerateQRCodePNGBytes(data string) ([]byte, error) {
	if data == "" {
		return nil, fmt.Errorf("QR data is empty")
	}

	return qrcode.Encode(data, qrcode.Medium, 256)
}

// QRCodeAPIResponse represents QR code data for API response
type QRCodeAPIResponse struct {
	ID        string                 `json:"id"`
	Platform  string                 `json:"platform"`
	Status    string                 `json:"status"`
	QRCode    string                 `json:"qr_code,omitempty"` // base64 encoded PNG
	Message   string                 `json:"message,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	ExpiresIn int                    `json:"expires_in"` // seconds remaining
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ToAPIResponse converts a QRCodeSession to API response format
func (s *QRCodeSession) ToAPIResponse() *QRCodeAPIResponse {
	resp := &QRCodeAPIResponse{
		ID:        s.ID,
		Platform:  s.Platform,
		Status:    s.Status,
		Message:   s.Message,
		CreatedAt: s.CreatedAt,
		Metadata:  s.Metadata,
	}

	// Only include QR code for active states
	if s.Status == "pending" || s.Status == "scanning" {
		resp.QRCode = s.QRCode
		resp.ExpiresIn = int(time.Until(s.ExpiresAt).Seconds())
		if resp.ExpiresIn < 0 {
			resp.ExpiresIn = 0
		}
	} else if s.Status == "confirmed" {
		// Include QR code for confirmed status too (for display)
		resp.QRCode = s.QRCode
		resp.ExpiresIn = int(time.Until(s.ExpiresAt).Seconds())
	}

	return resp
}

// ToJSON converts session to JSON
func (s *QRCodeSession) ToJSON() string {
	data, _ := json.Marshal(s.ToAPIResponse())
	return string(data)
}

// QRCodeHandler defines the interface for platforms that support QR login
type QRCodeHandler interface {
	// StartQRLogin initiates QR code login and returns the QR data
	StartQRLogin(ctx context.Context) (string, error)
	// GetLoginStatus returns the current login status
	GetLoginStatus() string
	// IsLoggedIn returns true if successfully logged in
	IsLoggedIn() bool
}

// GetAllLoginStatuses returns login status for all platforms
func (g *Gateway) GetAllLoginStatuses() []*LoginStatus {
	statuses := make([]*LoginStatus, 0)

	g.mu.RLock()
	for platform, handler := range g.platforms {
		status := &LoginStatus{
			Platform: platform,
			Status:   "unknown",
		}

		// Check if it's a QR handler
		if qrHandler, ok := handler.(QRCodeHandler); ok {
			if qrHandler.IsLoggedIn() {
				status.Status = "confirmed"
				status.Message = "Logged in"
			} else {
				status.Status = "waiting_qr"
				status.Message = "Scan QR code to login"
			}
		} else {
			// Traditional token-based login
			if handler.IsConnected() {
				status.Status = "confirmed"
				status.Message = "Connected via token"
			} else {
				status.Status = "not_configured"
				status.Message = "Configure token to login"
			}
		}

		statuses = append(statuses, status)
	}
	g.mu.RUnlock()

	return statuses
}

// LoginStatus represents the current login status
type LoginStatus struct {
	Platform   string                 `json:"platform"`
	Status     string                 `json:"status"` // "not_configured", "waiting_qr", "scanning", "confirmed", "error"
	Message    string                 `json:"message,omitempty"`
	QRCode     string                 `json:"qr_code,omitempty"`
	QRStatus   string                 `json:"qr_status,omitempty"`
	QRExpires  int                    `json:"qr_expires_in,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// saveILinkToken saves the iLink bot token to config file
func (m *QRCodeManager) saveILinkToken(token, baseURL string) {
	if token == "" {
		return
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Errorf("Failed to get home dir for saving token: %v", err)
		return
	}

	configPath := filepath.Join(homeDir, ".magic", "config.json")

	// Read existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		// Config doesn't exist, create new
		cfg := map[string]interface{}{
			"gateway": map[string]interface{}{
				"enabled": true,
				"platforms": map[string]interface{}{
					"wechat_ilink": map[string]interface{}{
						"enabled":   true,
						"token":     token,
						"api_url":   baseURL,
					},
				},
			},
		}
		newData, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(configPath, newData, 0644); err != nil {
			log.Errorf("Failed to create config with token: %v", err)
		} else {
			log.Infof("Saved iLink token to new config file")
		}
		return
	}

	// Parse existing config
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Errorf("Failed to parse config: %v", err)
		return
	}

	// Navigate/initialize gateway.platforms.wechat_ilink
	gatewaySection := ensureMapQR(cfg, "gateway")
	platforms := ensureMapQR(gatewaySection, "platforms")
	ilinkSection := ensureMapQR(platforms, "wechat_ilink")
	ilinkSection["enabled"] = true
	ilinkSection["token"] = token
	ilinkSection["api_url"] = baseURL

	newData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Errorf("Failed to marshal config: %v", err)
		return
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		log.Errorf("Failed to save config with token: %v", err)
	} else {
		log.Infof("Saved iLink token to config file")
	}
}

// ensureMapQR gets or creates a nested map[string]interface{} at the given key
func ensureMapQR(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key]; ok {
		if sub, ok := v.(map[string]interface{}); ok {
			return sub
		}
	}
	sub := make(map[string]interface{})
	m[key] = sub
	return sub
}
