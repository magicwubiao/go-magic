package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// webSession represents a single authenticated web session.
type webSession struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	// remember controls the session lifetime: Remember-me sessions last longer.
	Remember bool `json:"remember"`
}

// webSessionManager holds web sessions. Sessions are persisted to a JSON file
// under the magic home directory so that a server restart does not log users
// out. Logout / password-reset still invalidate sessions server-side (the
// persisted file is rewritten on every mutation).
type webSessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*webSession
	// file is the optional path to persist sessions to. When empty, the manager
	// behaves exactly like the old in-memory-only implementation.
	file string
}

// Session lifetimes.
const (
	sessionTTLShort = 12 * time.Hour      // default (no "remember me")
	sessionTTLLong  = 30 * 24 * time.Hour // "remember me"
)

func newWebSessionManager() *webSessionManager {
	return &webSessionManager{
		sessions: make(map[string]*webSession),
	}
}

// newWebSessionManagerFile returns a manager that persists sessions to the
// given file path. Existing sessions are loaded from disk if present.
func newWebSessionManagerFile(file string) *webSessionManager {
	m := newWebSessionManager()
	m.file = file
	m.load()
	return m
}

// load reads persisted sessions from disk into memory. It is safe to call
// multiple times (e.g. after a file was written by another process).
func (m *webSessionManager) load() {
	if m.file == "" {
		return
	}
	data, err := os.ReadFile(m.file)
	if err != nil {
		// No file yet (first run) or unreadable — start empty.
		return
	}
	var persisted []*webSession
	if err := json.Unmarshal(data, &persisted); err != nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	for _, s := range persisted {
		if s == nil || s.Token == "" || now.After(s.ExpiresAt) {
			// Skip empty or already-expired sessions.
			continue
		}
		m.sessions[s.Token] = s
	}
	m.mu.Unlock()
	// Rewrite the file to drop any expired entries we just filtered out.
	m.persist()
}

// persist atomically writes the current in-memory sessions to disk.
func (m *webSessionManager) persist() {
	if m.file == "" {
		return
	}
	m.mu.RLock()
	list := make([]*webSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.RUnlock()

	data, err := json.Marshal(list)
	if err != nil {
		return
	}
	// Write to a temp file then rename for atomicity, and ensure the parent
	// directory exists.
	dir := filepath.Dir(m.file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	tmp := m.file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, m.file)
}

// create issues a new cryptographically-random session token.
func (m *webSessionManager) create(remember bool) string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)

	now := time.Now()
	ttl := sessionTTLShort
	if remember {
		ttl = sessionTTLLong
	}

	m.mu.Lock()
	m.sessions[token] = &webSession{
		Token:     token,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
		Remember:  remember,
	}
	m.mu.Unlock()
	m.persist()
	return token
}

// validate returns true if the token is a live, unexpired session.
func (m *webSessionManager) validate(token string) bool {
	if token == "" {
		return false
	}
	m.mu.RLock()
	s, ok := m.sessions[token]
	if !ok {
		m.mu.RUnlock()
		return false
	}
	expired := time.Now().After(s.ExpiresAt)
	m.mu.RUnlock()

	if expired {
		m.revoke(token)
		return false
	}
	return true
}

// revoke invalidates a session (logout).
func (m *webSessionManager) revoke(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
	m.persist()
}

// revokeAll invalidates every session (e.g. password reset).
func (m *webSessionManager) revokeAll() {
	m.mu.Lock()
	m.sessions = make(map[string]*webSession)
	m.mu.Unlock()
	m.persist()
}

// purgeExpired removes expired sessions to bound memory usage.
func (m *webSessionManager) purgeExpired() {
	now := time.Now()
	m.mu.Lock()
	changed := false
	for _, s := range m.sessions {
		if now.After(s.ExpiresAt) {
			delete(m.sessions, s.Token)
			changed = true
		}
	}
	m.mu.Unlock()
	if changed {
		m.persist()
	}
}
