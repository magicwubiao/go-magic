package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/magicwubiao/go-magic/internal/goal"
)

// handleGoals handles /api/goals
func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request) {
	if s.goalMgr == nil {
		http.Error(w, "Goal manager not initialized", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		// List goals
		status := r.URL.Query().Get("status")
		goals, err := s.goalMgr.List(ctx, goal.Status(status))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, goals)

	case http.MethodPost:
		// Create goal
		var req struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		g, err := s.goalMgr.Create(ctx, req.Title, req.Description)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, g)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGoalByID handles /api/goals/{id} and /api/goals/{id}/...
func (s *Server) handleGoalByID(w http.ResponseWriter, r *http.Request) {
	if s.goalMgr == nil {
		http.Error(w, "Goal manager not initialized", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/goals/")

	// Handle link/unlink session
	if strings.HasSuffix(path, "/link") {
		goalID := strings.TrimSuffix(path, "/link")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := s.goalMgr.LinkSession(ctx, goalID, req.SessionID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "linked": true})
		return
	}

	if strings.HasSuffix(path, "/unlink") {
		goalID := strings.TrimSuffix(path, "/unlink")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := s.goalMgr.UnlinkSession(ctx, goalID, req.SessionID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "unlinked": true})
		return
	}

	// Handle goal by ID
	goalID := path

	switch r.Method {
	case http.MethodGet:
		g, err := s.goalMgr.Get(ctx, goalID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		jsonResponse(w, g)

	case http.MethodPut:
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		g, err := s.goalMgr.Update(ctx, goalID, updates)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, g)

	case http.MethodDelete:
		if err := s.goalMgr.Delete(ctx, goalID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "deleted": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
