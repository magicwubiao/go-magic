package server

import (
	"context"
	"net/http"
	"path/filepath"
	"time"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]string{"status": "healthy"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	providerStatus := "not_configured"
	if s.provider != nil {
		providerStatus = "connected"
	}

	sessions := 0
	if s.sessionStore != nil {
		if list, err := s.sessionStore.ListSessions(context.Background(), ""); err == nil {
			sessions = len(list)
		}
	}

	jsonResponse(w, map[string]interface{}{
		"status":                "ok",
		"timestamp":             time.Now().Unix(),
		"version":               s.version,
		"active_sessions":       sessions,
		"config_path":           filepath.Join(s.magicHome, "config.json"),
		"config_version":        1,
		"latest_config_version": 1,
		"env_path":              filepath.Join(s.magicHome, ".env"),
		"gateway_exit_reason":   nil,
		"gateway_health_url":    nil,
		"gateway_pid":           nil,
		"gateway_platforms":     map[string]PlatformStatus{},
		"gateway_running":       false,
		"gateway_state":         nil,
		"gateway_updated_at":    nil,
		"magic_home":            s.magicHome,
		"session_count":         sessions,
		"provider_status":       providerStatus,
		"release_date":          time.Now().Format("2006-01-02"),
	})
}
