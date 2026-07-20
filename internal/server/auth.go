package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
)

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	s.authMu.RLock()
	token := s.authToken
	s.authMu.RUnlock()

	if token == "" {
		http.Error(w, `{"error":"auth not configured"}`, http.StatusNotFound)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// First try bcrypt verification (new format)
	if err := bcrypt.CompareHashAndPassword([]byte(token), []byte(req.Password)); err == nil {
		jsonResponse(w, map[string]interface{}{
			"ok":    true,
			"token": token,
		})
		return
	}

	// Fallback to old SHA-256 format for backward compatibility
	hash := sha256.Sum256([]byte(req.Password))
	inputToken := hex.EncodeToString(hash[:])

	if subtle.ConstantTimeCompare([]byte(inputToken), []byte(token)) == 1 {
		jsonResponse(w, map[string]interface{}{
			"ok":    true,
			"token": token,
		})
		return
	}

	http.Error(w, `{"error":"invalid password"}`, http.StatusUnauthorized)
}

func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
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

	jsonResponse(w, map[string]interface{}{
		"ok": true,
	})
}

func (s *Server) handleAuthReset(w http.ResponseWriter, r *http.Request) {
	authTokenPath := filepath.Join(s.magicHome, ".auth_token")
	os.Remove(authTokenPath)

	s.authMu.Lock()
	s.authToken = ""
	s.authMu.Unlock()

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
