package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
	qrcode "github.com/skip2/go-qrcode"
)

// QRCodeManager handles QR code generation and login for all platforms
type QRCodeManager struct {
	mu      sync.RWMutex
	codes   map[string]*QRCodeSession // platform -> session
	cleanup chan string
	pollers map[string]*QRPollContext // active pollers

	// wecomConfirmFn 由 server 注入：WeCom AI Bot 扫码确认（新 bot_id/secret
	// 已写入 config.json）后回调。gateway 只在启动时读取一次 wecom 凭据，
	// 宿主可借此自动重启 gateway，避免"扫码成功但新 bot 未连接、收不到消息"。
	wecomConfirmMu sync.RWMutex
	wecomConfirmFn func()
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
	Status    string                 `json:"status"`            // pending, scanning, confirmed, expired, error
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

// CreateSession creates a new QR code session for a platform
// qrData is the raw QR data string (used for status polling)
// qrImage is an optional base64 image (if empty, will be generated from qrData)
func (m *QRCodeManager) CreateSession(platform string, qrData string, qrImage string) (*QRCodeSession, error) {
	return m.CreateSessionWithMeta(platform, qrData, qrImage, nil)
}

// CreateSessionWithMeta 同 CreateSession，额外允许附加轮询所需元数据
// （如 WeCom AI Bot 的 scode）。
func (m *QRCodeManager) CreateSessionWithMeta(platform string, qrData string, qrImage string, meta map[string]string) (*QRCodeSession, error) {
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
		ExpiresAt: time.Now().Add(60 * time.Second), // 60 seconds expiry
		Metadata:  make(map[string]interface{}),
	}
	for k, v := range meta {
		session.Metadata[k] = v
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
						// WeCom AI Bot：凭据已落盘 config.json，但 gateway 进程只在
						// 启动时读取一次 wecom 凭据——通知宿主（server）自动重启
						// gateway 使新 bot_id/secret 生效。旧流程要求手动重启，
						// 极易造成"扫码成功却收不到消息"。
						if platform == "wecom" {
							m.fireWeComConfirmed()
						}
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
	case "wecom":
		// WeCom AI Bot 扫码：轮询 query_result，成功后把 bot_id/secret 写入 config
		return pollWeComAIBotStatus(ctx, session)
	default:
		// 其余平台的状态由回调/外部机制直接更新，轮询不改变状态；
		// 新增扫码平台需在此提供对应的 poll 分支。
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

// SetWeComConfirmedHook 注册 WeCom AI Bot 扫码确认后的回调（通常由 server 注入）。
// 回调在 bot_id/secret 已写入 config.json 之后触发，宿主可据此重启 gateway，
// 使新凭据生效。传 nil 可注销。
func (m *QRCodeManager) SetWeComConfirmedHook(fn func()) {
	m.wecomConfirmMu.Lock()
	defer m.wecomConfirmMu.Unlock()
	m.wecomConfirmFn = fn
}

// fireWeComConfirmed 调用已注册的 wecom 确认回调（无回调时为空操作）。
func (m *QRCodeManager) fireWeComConfirmed() {
	m.wecomConfirmMu.RLock()
	fn := m.wecomConfirmFn
	m.wecomConfirmMu.RUnlock()
	if fn != nil {
		fn()
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
	Platform  string                 `json:"platform"`
	Status    string                 `json:"status"` // "not_configured", "waiting_qr", "scanning", "confirmed", "error"
	Message   string                 `json:"message,omitempty"`
	QRCode    string                 `json:"qr_code,omitempty"`
	QRStatus  string                 `json:"qr_status,omitempty"`
	QRExpires int                    `json:"qr_expires_in,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// saveILinkToken saves the iLink bot token to config file
func (m *QRCodeManager) saveILinkToken(token, baseURL string) {
	if token == "" {
		return
	}

	configPath := filepath.Join(config.GetMagicHome(), "config.json")

	// Read existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		// Config doesn't exist, create new
		cfg := map[string]interface{}{
			"gateway": map[string]interface{}{
				"enabled": true,
				"platforms": map[string]interface{}{
					"wechat_ilink": map[string]interface{}{
						"enabled": true,
						"token":   token,
						"api_url": baseURL,
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
