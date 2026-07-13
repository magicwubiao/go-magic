package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/approval"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

func (s *Server) handleApprovalHistory(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	offsetStr := r.URL.Query().Get("offset")
	offset := 0
	if offsetStr != "" {
		if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
			offset = n
		}
	}

	records := mgr.GetHistory(limit, offset)
	total := mgr.HistoryLen()

	jsonResponse(w, map[string]interface{}{
		"records": records,
		"total":   total,
	})
}

func (s *Server) handleApprovalWhitelist(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		patterns := mgr.GetWhitelist()
		jsonResponse(w, map[string]interface{}{
			"patterns": patterns,
			"total":    len(patterns),
		})

	case http.MethodPost:
		var req struct {
			Pattern string `json:"pattern"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := mgr.AddToWhitelist(req.Pattern); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]bool{"success": true})

	case http.MethodDelete:
		var req struct {
			Pattern string `json:"pattern"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := mgr.RemoveFromWhitelist(req.Pattern); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResponse(w, map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleApprovalPatternsDenied(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		denied := mgr.GetDeniedCommands()
		jsonResponse(w, map[string]interface{}{
			"patterns": denied,
			"total":    len(denied),
		})

	case http.MethodDelete:
		var req struct {
			Pattern string `json:"pattern"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		// Remove from denied patterns by approving it
		mgr.Approve(&approval.ApprovalRequest{Command: req.Pattern})
		jsonResponse(w, map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleApprovalPatternsTrusted(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		trusted := mgr.GetTrustedCommands()
		jsonResponse(w, map[string]interface{}{
			"patterns": trusted,
			"total":    len(trusted),
		})

	case http.MethodDelete:
		var req struct {
			Pattern string `json:"pattern"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pattern == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		// Remove from trusted patterns by denying it
		mgr.Deny(&approval.ApprovalRequest{Command: req.Pattern})
		jsonResponse(w, map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleApprovalStrategy(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Strategy string `json:"strategy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Strategy == "" {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		switch req.Strategy {
		case "manual":
			mgr.SetStrategy(approval.StrategyManual)
		case "auto":
			mgr.SetStrategy(approval.StrategyAutoApprove)
		case "smart":
			mgr.SetStrategy(approval.StrategySmart)
		case "whitelist":
			mgr.SetStrategy(approval.StrategyWhitelist)
		default:
			http.Error(w, "Invalid strategy. Must be: manual, auto, smart, or whitelist", http.StatusBadRequest)
			return
		}

		jsonResponse(w, map[string]bool{"success": true})
		return
	}

	// GET current strategy
	cfg := mgr.GetConfig()
	jsonResponse(w, map[string]interface{}{
		"strategy": cfg.Strategy,
	})
}

func (s *Server) handleApprovalClearHistory(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	var req struct {
		OlderThanHours int `json:"older_than_hours"`
	}
	// Default body is optional; use default 168 hours (7 days) if not provided
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.OlderThanHours <= 0 {
		req.OlderThanHours = 168
	}

	mgr.ClearHistory(time.Duration(req.OlderThanHours) * time.Hour)
	jsonResponse(w, map[string]bool{"success": true})
}

func (s *Server) getApprovalManager() *approval.Manager {
	// Prefer the independent approval manager (always available after server init)
	if s.approvalMgr != nil {
		return s.approvalMgr
	}

	// Fallback: try to find any agent with an approval hook
	s.agentsMu.Lock()
	defer s.agentsMu.Unlock()
	for _, a := range s.agents {
		if hook := a.GetApprovalHook(); hook != nil {
			return hook.GetManager()
		}
	}

	return nil
}

func (s *Server) handleApprovalPending(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	pending := mgr.GetPendingApprovals()

	type pendingItem struct {
		ID        string `json:"id"`
		Command   string `json:"command"`
		RiskLevel string `json:"risk_level"`
		SessionID string `json:"session_id"`
		CreatedAt string `json:"created_at"`
		ExpiresAt string `json:"expires_at"`
	}

	items := make([]pendingItem, 0, len(pending))
	for _, pa := range pending {
		items = append(items, pendingItem{
			ID:        pa.ID,
			Command:   pa.Request.Command,
			RiskLevel: pa.Request.RiskLevel.String(),
			SessionID: pa.Request.SessionID,
			CreatedAt: pa.CreatedAt.Format(time.RFC3339),
			ExpiresAt: pa.ExpiresAt.Format(time.RFC3339),
		})
	}

	jsonResponse(w, map[string]interface{}{
		"pending": items,
		"total":   len(items),
	})
}

func (s *Server) handleApprovalDenied(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	denied := mgr.GetDeniedCommands()
	jsonResponse(w, map[string]interface{}{
		"patterns": denied,
		"total":    len(denied),
	})
}

func (s *Server) handleApprovalSettings(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		// GET current settings
		cfg := mgr.GetConfig()
		stats := mgr.GetStats()
		jsonResponse(w, map[string]interface{}{
			"strategy":             cfg.Strategy,
			"enableLearning":       cfg.EnableLearning,
			"cliConfirm":           cfg.EnableCLIConfirm,
			"trustThreshold":       cfg.TrustThreshold,
			"whitelist":            mgr.GetWhitelist(),
			"trusted_patterns":     stats.TrustedPatterns,
			"denied_patterns":      stats.DeniedPatterns,
			"total_requests":       stats.TotalRequests,
			"auto_approved":        stats.AutoApproved,
			"user_approved":        stats.UserApproved,
			"user_denied":          stats.UserDenied,
			"avg_response_time_ms": stats.AvgResponseTime,
		})
	case http.MethodPut:
		// PUT update settings
		var req struct {
			Strategy       string `json:"strategy"`
			TrustThreshold int    `json:"trust_threshold"`
			CLIPrompt      bool   `json:"cli_confirm"`
			EnableLearning bool   `json:"enable_learning"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		// Update strategy
		if req.Strategy != "" {
			mgr.SetStrategy(approval.Strategy(req.Strategy))
		}
		// Update other settings
		cfg := mgr.GetConfig()
		if req.TrustThreshold > 0 {
			cfg.TrustThreshold = req.TrustThreshold
		}
		cfg.EnableCLIConfirm = req.CLIPrompt
		cfg.EnableLearning = req.EnableLearning

		// Sync to main config file (the single source of truth)
		s.syncApprovalToMainConfig(mgr)

		jsonResponse(w, map[string]bool{"success": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleApprovalTrusted(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	trusted := mgr.GetTrustedCommands()
	jsonResponse(w, map[string]interface{}{
		"patterns": trusted,
		"total":    len(trusted),
	})
}

func (s *Server) handleApprovalStats(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	stats := mgr.GetStats()

	// Convert by_risk_level keys to strings for JSON serialization
	byRiskLevel := make(map[string]int)
	for k, v := range stats.ByRiskLevel {
		byRiskLevel[k.String()] = v
	}

	jsonResponse(w, map[string]interface{}{
		"total_requests":       stats.TotalRequests,
		"auto_approved":        stats.AutoApproved,
		"user_approved":        stats.UserApproved,
		"user_denied":          stats.UserDenied,
		"timed_out":            stats.TimedOut,
		"trusted_patterns":     stats.TrustedPatterns,
		"denied_patterns":      stats.DeniedPatterns,
		"top_commands":         stats.TopCommands,
		"by_risk_level":        byRiskLevel,
		"by_category":          stats.ByCategory,
		"avg_response_time_ms": stats.AvgResponseTime,
	})
}

func (s *Server) syncApprovalToMainConfig(mgr *approval.Manager) {
	if s.cfg == nil {
		return
	}
	ac := mgr.GetConfig()
	s.cfg.Approval = &appconfig.ApprovalConfig{
		Strategy:         string(ac.Strategy),
		TrustThreshold:   ac.TrustThreshold,
		EnableLearning:   ac.EnableLearning,
		EnableCLIConfirm: ac.EnableCLIConfirm,
		ApprovalTimeout:  ac.ApprovalTimeout,
	}
	_ = s.cfg.Save()
}

func (s *Server) handleApprovalPendingByID(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	// Extract ID from path: /api/approval/pending/{id}/resolve or /api/approval/pending/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/approval/pending/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]

	if r.Method == http.MethodPost && len(parts) > 1 && parts[1] == "resolve" {
		var req struct {
			Approved bool   `json:"approved"`
			Reason   string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		mgr.ResolveWebApproval(id, req.Approved, req.Reason)
		jsonResponse(w, map[string]bool{"success": true})
		return
	}

	// GET single pending approval
	pending := mgr.GetPendingApprovals()
	for _, pa := range pending {
		if pa.ID == id {
			jsonResponse(w, map[string]interface{}{
				"id":         pa.ID,
				"command":    pa.Request.Command,
				"risk_level": pa.Request.RiskLevel.String(),
				"session_id": pa.Request.SessionID,
				"created_at": pa.CreatedAt.Format(time.RFC3339),
				"expires_at": pa.ExpiresAt.Format(time.RFC3339),
			})
			return
		}
	}

	http.Error(w, "Pending approval not found", http.StatusNotFound)
}

func (s *Server) handleApprovalStatus(w http.ResponseWriter, r *http.Request) {
	mgr := s.getApprovalManager()
	if mgr == nil {
		jsonResponse(w, map[string]interface{}{"error": "approval system not available"})
		return
	}

	cfg := mgr.GetConfig()
	stats := mgr.GetStats()

	jsonResponse(w, map[string]interface{}{
		"strategy":             cfg.Strategy,
		"enableLearning":       cfg.EnableLearning,
		"cliConfirm":           cfg.EnableCLIConfirm,
		"trustThreshold":       cfg.TrustThreshold,
		"whitelist":            mgr.GetWhitelist(),
		"trusted_patterns":     stats.TrustedPatterns,
		"denied_patterns":      stats.DeniedPatterns,
		"total_requests":       stats.TotalRequests,
		"auto_approved":        stats.AutoApproved,
		"user_approved":        stats.UserApproved,
		"user_denied":          stats.UserDenied,
		"avg_response_time_ms": stats.AvgResponseTime,
	})
}
