package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/goal"
)

func (s *Server) handleGoalAnalyze(w http.ResponseWriter, r *http.Request) {
	if s.goalMgr == nil {
		http.Error(w, "Goal manager not initialized", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/goals/")
	goalID := strings.TrimSuffix(path, "/analyze")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Conversation string `json:"conversation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get goal info
	g, err := s.goalMgr.Get(ctx, goalID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Simple heuristic analysis based on conversation
	analysis := analyzeProgress(g, req.Conversation)

	jsonResponse(w, map[string]interface{}{
		"goal_id":            goalID,
		"title":              g.Title,
		"current_progress":   g.Progress,
		"suggested_progress": analysis.SuggestedProgress,
		"reason":             analysis.Reason,
		"completed":          analysis.Completed,
	})
}

func analyzeProgress(g *goal.Goal, conversation string) ProgressAnalysis {
	lowerConv := strings.ToLower(conversation)

	// Check for completion indicators
	completionWords := []string{"完成", "搞定", "done", "finished", "completed", "解决了", "成功了"}
	for _, word := range completionWords {
		if strings.Contains(lowerConv, word) {
			return ProgressAnalysis{
				SuggestedProgress: 100,
				Reason:            "检测到完成关键词，目标可能已完成",
				Completed:         true,
			}
		}
	}

	// Check for progress indicators
	progressWords := []string{"进展", "进度", "progress", "advance", "推进", "完成了一半", "50%"}
	for _, word := range progressWords {
		if strings.Contains(lowerConv, word) {
			// If current progress is low, suggest a jump
			if g.Progress < 50 {
				return ProgressAnalysis{
					SuggestedProgress: 50,
					Reason:            "检测到进展关键词，建议更新进度到50%",
					Completed:         false,
				}
			}
		}
	}

	// Check for start indicators
	startWords := []string{"开始", "启动", "start", "begin", "着手", "准备"}
	for _, word := range startWords {
		if strings.Contains(lowerConv, word) && g.Progress == 0 {
			return ProgressAnalysis{
				SuggestedProgress: 10,
				Reason:            "检测到开始关键词，建议设置初始进度10%",
				Completed:         false,
			}
		}
	}

	// Default: no change
	return ProgressAnalysis{
		SuggestedProgress: g.Progress,
		Reason:            "未检测到明显的进度变化",
		Completed:         false,
	}
}

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

func (s *Server) handleGoalSessions(w http.ResponseWriter, r *http.Request) {
	if s.goalMgr == nil {
		http.Error(w, "Goal manager not initialized", http.StatusServiceUnavailable)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	path := strings.TrimPrefix(r.URL.Path, "/api/goals/sessions/")
	goalID := path

	if goalID == "" {
		http.Error(w, "Goal ID required", http.StatusBadRequest)
		return
	}

	// Get goal
	g, err := s.goalMgr.Get(ctx, goalID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Get session details for each linked session
	sessions := []map[string]interface{}{}
	if s.sessionStore != nil && len(g.SessionIDs) > 0 {
		for _, sessionID := range g.SessionIDs {
			sess, err := s.sessionStore.LoadSession(ctx, sessionID)
			if err != nil {
				continue // Skip if session not found
			}

			// Build session summary
			title := sess.Name
			if title == "" {
				// Extract title from first user message
				for _, m := range sess.Messages {
					if m.Role == "user" && m.Content != "" {
						title = strings.TrimSpace(m.Content)
						// Truncate by runes, not bytes, to avoid cutting UTF-8 characters
						runes := []rune(title)
						if len(runes) > 50 {
							title = string(runes[:50]) + "..."
						}
						break
					}
				}
			}
			if title == "" {
				title = "Untitled"
			}

			// Build preview from messages
			preview := ""
			for _, m := range sess.Messages {
				if m.Role == "user" || m.Role == "assistant" {
					preview += m.Content + " "
					runes := []rune(preview)
					if len(runes) > 150 {
						preview = string(runes[:150]) + "..."
						break
					}
				}
			}
			preview = strings.TrimSpace(preview)
			runes := []rune(preview)
			if len(runes) > 150 {
				preview = string(runes[:150]) + "..."
			}

			// Determine if session is active based on goal status
			// Session is not active if goal is completed or abandoned
			isActive := time.Since(sess.UpdatedAt) < 30*time.Minute && g.Status != "completed" && g.Status != "abandoned"

			sessions = append(sessions, map[string]interface{}{
				"id":            sess.ID,
				"title":         title,
				"preview":       preview,
				"model":         sess.Model,
				"message_count": len(sess.Messages),
				"created_at":    sess.CreatedAt.Unix(),
				"updated_at":    sess.UpdatedAt.Unix(),
				"is_active":     isActive,
			})
		}
	}

	// Sort by updated_at desc (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i]["updated_at"].(int64) > sessions[j]["updated_at"].(int64)
	})

	jsonResponse(w, map[string]interface{}{
		"goal_id":  goalID,
		"sessions": sessions,
		"total":    len(sessions),
	})
}

func (s *Server) handleGoalCurrent(w http.ResponseWriter, r *http.Request) {
	if s.goalMgr == nil {
		http.Error(w, "Goal manager not initialized", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		// Get first active goal (most recently updated)
		goals, err := s.goalMgr.List(ctx, goal.StatusActive)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(goals) == 0 {
			jsonResponse(w, nil)
			return
		}
		jsonResponse(w, goals[0])

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

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

	// Handle decompose - auto-break goal into kanban tasks
	if strings.HasSuffix(path, "/decompose") {
		goalID := strings.TrimSuffix(path, "/decompose")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.kanbanMgr == nil {
			http.Error(w, "Kanban manager not initialized", http.StatusServiceUnavailable)
			return
		}
		goal, err := s.goalMgr.Get(ctx, goalID)
		if err != nil {
			http.Error(w, "Goal not found: "+err.Error(), http.StatusNotFound)
			return
		}
		tasks, err := s.kanbanMgr.DecomposeGoal(ctx, goal.ID, goal.Title, goal.Description, s.provider)
		if err != nil {
			http.Error(w, "Decompose failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]interface{}{
			"goal_id": goal.ID,
			"count":   len(tasks),
			"tasks":   tasks,
		})
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
