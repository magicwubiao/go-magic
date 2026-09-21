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

// authorized reports whether the request carries a credential that requireAuth
// would accept: a live web session, or one of the legacy static-token forms
// (Bearer header / X-Magic-Session-Token / token query param).
//
// Both the auth middleware and /api/auth/status go through here so the two can
// never disagree: the web router guard treats "authenticated" as "the protected
// middleware would let me in".
func (s *Server) authorized(r *http.Request) bool {
	s.authMu.RLock()
	token := s.authToken
	s.authMu.RUnlock()

	if token == "" {
		return false
	}

	// 1) Live session token (modern, supports logout/expiry).
	if st := bearerToken(r); s.sessions.validate(st) {
		return true
	}

	// 2) Legacy static token (the bcrypt hash itself) for backward
	//    compatibility with clients that logged in before sessions.
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) == 1 {
		return true
	}

	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Magic-Session-Token")), []byte(token)) == 1 {
		return true
	}

	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(token)) == 1 {
		return true
	}

	return false
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
	if sessionToken == "" {
		// 密码已落盘但会话没建起来：明确报错，别回一个 token:"" 让前端存空串
		// （空串在 localStorage 里是 falsy，会被当成"没登录"，用户刚设完密码
		// 又被弹回登录页，还不明白为什么）。
		http.Error(w, `{"error":"failed to create session"}`, http.StatusInternalServerError)
		return
	}
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

// handleAuthStatus reports whether a password is configured, plus whether the
// credential the caller presented is still valid.
//
// `authenticated` exists so the web router guard can tell a real login from a
// token left over in localStorage (expired session, revoked sessions after a
// password reset, or a server that switched magic home). 只凭"本地有 token"
// 放行，会让主界面带着死 token 并发发出十几个请求全部 401，把错误提示弹在
// 认证页上——那正是用户看到的"有时候出现弹窗错误"。
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	s.authMu.RLock()
	token := s.authToken
	s.authMu.RUnlock()

	jsonResponse(w, map[string]interface{}{
		"configured":    token != "",
		"authenticated": s.authorized(r),
	})
}
