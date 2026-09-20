package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// loginRateLimiter bounds how often a client may attempt authentication, which
// mitigates online brute-force attacks against /api/auth/login and /api/auth/setup.
type loginRateLimiter struct {
	mu      sync.Mutex
	perIP   map[string]*ipCounter
	window  time.Duration
	maxHits int
}

type ipCounter struct {
	windowStart  time.Time
	attempts     int
	blockedUntil time.Time
}

func newLoginRateLimiter(window time.Duration, maxHits int) *loginRateLimiter {
	return &loginRateLimiter{
		perIP:   make(map[string]*ipCounter),
		window:  window,
		maxHits: maxHits,
	}
}

// allow records an attempt and reports whether the caller may proceed.
func (l *loginRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	c, ok := l.perIP[ip]
	if !ok {
		c = &ipCounter{windowStart: now}
		l.perIP[ip] = c
	}

	// Reset window if expired.
	if now.Sub(c.windowStart) >= l.window {
		c.windowStart = now
		c.attempts = 0
		c.blockedUntil = time.Time{}
	}

	// Hard block if previously blocked and block hasn't elapsed.
	if now.Before(c.blockedUntil) {
		return false
	}

	// Count the attempt; block when exceeding the threshold.
	c.attempts++
	if c.attempts > l.maxHits {
		c.blockedUntil = now.Add(l.window)
		return false
	}
	return true
}

// reset clears rate-limit state for an IP (e.g. after a successful login).
func (l *loginRateLimiter) reset(ip string) {
	l.mu.Lock()
	delete(l.perIP, ip)
	l.mu.Unlock()
}

// bearerToken extracts the session token from the standard auth headers.
func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if h := strings.TrimSpace(r.Header.Get("X-Magic-Session-Token")); h != "" {
		return h
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// handleAuthLogin verifies the password and, on success, issues a fresh
// random session token instead of returning the stored hash.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.loginLimiter.allow(ip) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		jsonResponse(w, map[string]interface{}{
			"ok":    false,
			"error": "too many login attempts, please try again later",
		})
		return
	}

	s.authMu.RLock()
	token := s.authToken
	s.authMu.RUnlock()

	if token == "" {
		http.Error(w, `{"error":"auth not configured"}`, http.StatusNotFound)
		return
	}

	var req struct {
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	valid := false
	// First try bcrypt verification (new format)
	if err := bcrypt.CompareHashAndPassword([]byte(token), []byte(req.Password)); err == nil {
		valid = true
	} else {
		// Fallback to old SHA-256 format for backward compatibility
		hash := sha256.Sum256([]byte(req.Password))
		inputToken := hex.EncodeToString(hash[:])
		if subtle.ConstantTimeCompare([]byte(inputToken), []byte(token)) == 1 {
			valid = true
		}
	}

	if !valid {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		jsonResponse(w, map[string]interface{}{
			"ok":    false,
			"error": "invalid password",
		})
		return
	}

	// Success — clear rate limit and issue a session token.
	s.loginLimiter.reset(ip)
	sessionToken := s.sessions.create(req.Remember)
	if sessionToken == "" {
		http.Error(w, `{"error":"failed to create session"}`, http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"ok":    true,
		"token": sessionToken,
	})
}

// handleAuthSetup creates the initial password and immediately logs the caller in.
func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.loginLimiter.allow(ip) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		jsonResponse(w, map[string]interface{}{
			"ok":    false,
			"error": "too many attempts, please try again later",
		})
		return
	}

	s.authMu.RLock()
	token := s.authToken
	s.authMu.RUnlock()

	if token != "" {
		http.Error(w, `{"error":"auth already configured"}`, http.StatusConflict)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Password) < 8 {
		http.Error(w, `{"error":"password must be at least 8 characters"}`, http.StatusBadRequest)
		return
	}

	// Generate secure bcrypt hash with default cost
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, `{"error":"failed to hash password"}`, http.StatusInternalServerError)
		return
	}

	authTokenPath := filepath.Join(s.magicHome, ".auth_token")
	if err := os.WriteFile(authTokenPath, hash, 0600); err != nil {
		http.Error(w, `{"error":"failed to save token"}`, http.StatusInternalServerError)
		return
	}

	s.authMu.Lock()
	s.authToken = string(hash)
	s.authMu.Unlock()

	// Issue a session so the first-time setup flows straight into the app.
	sessionToken := s.sessions.create(true)
	jsonResponse(w, map[string]interface{}{
		"ok":    true,
		"token": sessionToken,
	})
}

// handleAuthReset removes the configured password and invalidates all sessions.
func (s *Server) handleAuthReset(w http.ResponseWriter, r *http.Request) {
	authTokenPath := filepath.Join(s.magicHome, ".auth_token")
	os.Remove(authTokenPath)

	s.authMu.Lock()
	s.authToken = ""
	s.authMu.Unlock()

	// Invalidate every live session.
	s.sessions.revokeAll()

	jsonResponse(w, map[string]bool{"ok": true})
}

// handleAuthLogout invalidates the caller's session server-side.
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	// The session token is carried the same way as auth: prefer the Bearer header.
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		tok = strings.TrimSpace(r.Header.Get("X-Magic-Session-Token"))
	}
	s.sessions.revoke(tok)
	jsonResponse(w, map[string]bool{"ok": true})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	s.authMu.RLock()
	token := s.authToken
	s.authMu.RUnlock()

	jsonResponse(w, map[string]interface{}{
		"configured": token != "",
	})
}
