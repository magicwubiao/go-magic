package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// QRCodeManager handles QR code generation and login for all platforms
type QRCodeManager struct {
	mu      sync.RWMutex
	codes   map[string]*QRCodeSession // platform -> session
	cleanup chan string
}

type QRCodeSession struct {
	ID        string                 `json:"id"`
	Platform  string                 `json:"platform"`
	Status    string                 `json:"status"` // pending, scanning, confirmed, expired
	QRCode    string                 `json:"qr_code,omitempty"` // base64 encoded PNG
	QRData    string                 `json:"qr_data,omitempty"` // raw QR data string
	CreatedAt time.Time              `json:"created_at"`
	ExpiresAt time.Time              `json:"expires_at"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// QRCodeCallback is called when a new QR code is generated
type QRCodeCallback func(session *QRCodeSession) error

var globalQRManager *QRCodeManager
var qrManagerOnce sync.Once

// GetQRManager returns the global QR code manager instance
func GetQRManager() *QRCodeManager {
	qrManagerOnce.Do(func() {
		globalQRManager = &QRCodeManager{
			codes:   make(map[string]*QRCodeSession),
			cleanup: make(chan string, 10),
		}
		go globalQRManager.cleanupLoop()
	})
	return globalQRManager
}

// cleanupLoop periodically removes expired QR codes
func (m *QRCodeManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case platform := <-m.cleanup:
			m.mu.Lock()
			if session, ok := m.codes[platform]; ok && session.Status == "expired" {
				delete(m.codes, platform)
				log.Debugf("Cleaned up expired QR code for platform: %s", platform)
			}
			m.mu.Unlock()
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			for platform, session := range m.codes {
				if now.After(session.ExpiresAt) {
					session.Status = "expired"
					m.cleanup <- platform
				}
			}
			m.mu.Unlock()
		}
	}
}

// CreateSession creates a new QR code session for a platform
func (m *QRCodeManager) CreateSession(platform string, qrData string) (*QRCodeSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := &QRCodeSession{
		ID:        uuid.New().String(),
		Platform:  platform,
		Status:    "pending",
		QRData:    qrData,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(60 * time.Second), // QR codes expire after 60 seconds
		Metadata:  make(map[string]interface{}),
	}

	// Generate PNG image
	img, err := generateQRCodeImage(qrData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate QR code image: %w", err)
	}
	session.QRCode = img

	m.codes[platform] = session
	log.Infof("Created QR code session for platform: %s, expires in 60s", platform)

	return session, nil
}

// GetSession returns the current QR code session for a platform
func (m *QRCodeManager) GetSession(platform string) *QRCodeSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.codes[platform]
}

// UpdateSessionStatus updates the status of a QR code session
func (m *QRCodeManager) UpdateSessionStatus(platform string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.codes[platform]; ok {
		session.Status = status
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

// QRCodeAPIResponse represents QR code data for API response
type QRCodeAPIResponse struct {
	ID        string                 `json:"id"`
	Platform  string                 `json:"platform"`
	Status    string                 `json:"status"`
	QRCode    string                 `json:"qr_code,omitempty"` // base64 encoded PNG
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
		CreatedAt: s.CreatedAt,
		Metadata:  s.Metadata,
	}

	if s.Status == "pending" || s.Status == "scanning" {
		resp.QRCode = s.QRCode
		resp.ExpiresIn = int(time.Until(s.ExpiresAt).Seconds())
		if resp.ExpiresIn < 0 {
			resp.ExpiresIn = 0
		}
	}

	return resp
}

// RegisterPlatformQR registers a platform's QR callback with the manager
func (m *QRCodeManager) RegisterPlatformQR(platform string, callback QRCodeCallback) {
	// This would be called by each platform handler when they generate a QR code
	log.Infof("Registered QR callback for platform: %s", platform)
}

// generateQRCodeImage generates a QR code PNG image from the given data
func generateQRCodeImage(data string) (string, error) {
	// Use a simple QR code generation approach
	// For production, you might want to use a proper QR library
	// Here we return the data as base64 for the terminal renderer
	
	// The actual image generation will be done by the frontend
	// This returns the data as a placeholder
	return data, nil
}

// GenerateQRCodePNG generates a proper PNG QR code image
// This is a placeholder - in production, use github.com/skip2/go-qrcode
func GenerateQRCodePNG(data string) ([]byte, error) {
	// TODO: Implement actual QR code generation
	// For now, return empty bytes
	return []byte{}, nil
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

// MarshalJSON marshals LoginStatus to JSON
// func (s *LoginStatus) MarshalJSON() ([]byte, error) {
// 	type Alias LoginStatus
// 	return json.Marshal((*Alias)(s))
// }
