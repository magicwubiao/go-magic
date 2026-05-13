package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed dist
var staticFiles embed.FS

// Session represents a chat session
type Session struct {
	ID              string    `json:"id"`
	Source          string    `json:"source"`
	Model           string    `json:"model"`
	Title           string    `json:"title"`
	StartedAt       int64     `json:"started_at"`
	EndedAt         *int64    `json:"ended_at"`
	LastActive      int64     `json:"last_active"`
	IsActive        bool      `json:"is_active"`
	MessageCount    int       `json:"message_count"`
	ToolCallCount   int       `json:"tool_call_count"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	Preview         string    `json:"preview"`
	ParentSessionID *string   `json:"parent_session_id"`
}

// Message represents a chat message
type Message struct {
	ID          string                 `json:"id"`
	SessionID   string                 `json:"session_id"`
	Role        string                 `json:"role"`
	Content     string                 `json:"content"`
	Timestamp   int64                  `json:"timestamp"`
	ToolCalls   []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolName    string                 `json:"tool_name,omitempty"`
	ToolCallID  string                 `json:"tool_call_id,omitempty"`
}

// Toolset represents a group of tools
type Toolset struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Tools   []string `json:"tools"`
	Enabled bool     `json:"enabled"`
}

// Skill represents a skill
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
}

// LogEntry represents a log entry
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
}

// Provider represents a model provider
type Provider struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	BaseURL string   `json:"base_url"`
	Models  []string `json:"models"`
	APIKey  string   `json:"api_key,omitempty"`
}

// PlatformStatus represents platform status
type PlatformStatus struct {
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	State        string `json:"state"`
	UpdatedAt    int64  `json:"updated_at"`
}

// Server represents the HTTP server
type Server struct {
	mu          sync.RWMutex
	startTime   time.Time
	sessions    map[string]*Session
	sessionList []*Session
	toolsets    []Toolset
	skills      []Skill
	providers   []Provider
	messages    map[string][]*Message
}

// NewServer creates a new server instance
func NewServer(dbPath string) *Server {
	return &Server{
		mu:         sync.RWMutex{},
		startTime:  time.Now(),
		sessions:   make(map[string]*Session),
		sessionList: []*Session{},
		messages:   make(map[string][]*Message),
		toolsets: []Toolset{
			{ID: "web", Name: "Web", Tools: []string{"web_search", "web_extract"}, Enabled: true},
			{ID: "terminal", Name: "Terminal", Tools: []string{"execute_command", "terminal"}, Enabled: true},
			{ID: "file", Name: "File", Tools: []string{"read_file", "write_file", "edit_file"}, Enabled: true},
			{ID: "browser", Name: "Browser", Tools: []string{"browser_navigate", "browser_snapshot"}, Enabled: true},
			{ID: "memory", Name: "Memory", Tools: []string{"memory_store", "memory_recall"}, Enabled: true},
			{ID: "code_execution", Name: "Code Execution", Tools: []string{"execute_code"}, Enabled: true},
			{ID: "delegation", Name: "Delegation", Tools: []string{"delegate_task", "poll_task"}, Enabled: true},
		},
		skills: []Skill{
			{ID: "git-workflow", Name: "Git Workflow", Description: "Git operations", Category: "development", Tags: []string{"git", "version-control"}},
			{ID: "code-review", Name: "Code Review", Description: "Code review assistance", Category: "development", Tags: []string{"code", "review"}},
			{ID: "web-research", Name: "Web Research", Description: "Research and analysis", Category: "research", Tags: []string{"web", "research"}},
			{ID: "data-analysis", Name: "Data Analysis", Description: "Data processing and analysis", Category: "analytics", Tags: []string{"data", "analysis"}},
		},
		providers: []Provider{
			{Name: "openai", Label: "OpenAI", BaseURL: "https://api.openai.com/v1", Models: []string{"gpt-4", "gpt-4-turbo", "gpt-3.5-turbo"}},
			{Name: "deepseek", Label: "DeepSeek", BaseURL: "https://api.deepseek.com", Models: []string{"deepseek-chat", "deepseek-coder"}},
			{Name: "anthropic", Label: "Anthropic", BaseURL: "https://api.anthropic.com", Models: []string{"claude-3-5-sonnet", "claude-3-opus"}},
		},
	}
}

// Start starts the HTTP server
func (s *Server) Start(port int) error {
	mux := http.NewServeMux()

	// CORS middleware wrapper
	withCORS := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == "OPTIONS" {
				w.WriteHeader(204)
				return
			}
			h(w, r)
		}
	}

	// Base API handler for CORS preflight
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		http.NotFound(w, r)
	})

	// Health
	mux.HandleFunc("/api/health", withCORS(s.handleHealth))
	mux.HandleFunc("/api/status", withCORS(s.handleStatus))

	// Sessions
	mux.HandleFunc("/api/sessions", withCORS(s.handleSessions))
	mux.HandleFunc("/api/sessions/", withCORS(s.handleSessionByID))
	mux.HandleFunc("/api/sessions/search", withCORS(s.handleSessionSearch))

	// Chat
	mux.HandleFunc("/api/chat", withCORS(s.handleChat))

	// Tools
	mux.HandleFunc("/api/toolsets", withCORS(s.handleToolsets))
	mux.HandleFunc("/api/tools/toolsets", withCORS(s.handleToolsets))
	mux.HandleFunc("/api/tools/toolsets/", withCORS(s.handleToolsetByID))
	mux.HandleFunc("/api/tools/categories", withCORS(s.handleToolCategories))
	mux.HandleFunc("/api/tools/", withCORS(s.handleToolByID))

	// Skills
	mux.HandleFunc("/api/skills", withCORS(s.handleSkills))
	mux.HandleFunc("/api/skills/categories", withCORS(s.handleSkillCategories))
	mux.HandleFunc("/api/skills/", withCORS(s.handleSkillByID))
	mux.HandleFunc("/api/dashboard/skills", withCORS(s.handleDashboardSkills))
	mux.HandleFunc("/api/dashboard/skills/search", withCORS(s.handleSkillsSearch))

	// Plugins
	mux.HandleFunc("/api/plugins", withCORS(s.handlePlugins))
	mux.HandleFunc("/api/dashboard/plugins", withCORS(s.handleDashboardPlugins))
	mux.HandleFunc("/api/dashboard/plugins/rescan", withCORS(s.handleDashboardPluginsRescan))

	// Models
	mux.HandleFunc("/api/models", withCORS(s.handleModels))
	mux.HandleFunc("/api/models/", withCORS(s.handleModelByID))
	mux.HandleFunc("/api/model/auxiliary", withCORS(s.handleModelAuxiliary))
	mux.HandleFunc("/api/providers", withCORS(s.handleProviders))

	// Config
	mux.HandleFunc("/api/config", withCORS(s.handleConfig))
	mux.HandleFunc("/api/config/", withCORS(s.handleConfigByID))

	// Analytics
	mux.HandleFunc("/api/analytics/models", withCORS(s.handleAnalyticsModels))
	mux.HandleFunc("/api/analytics/usage", withCORS(s.handleAnalyticsUsage))
	mux.HandleFunc("/api/analytics/cost", withCORS(s.handleAnalyticsCost))
	mux.HandleFunc("/api/analytics/tokens", withCORS(s.handleAnalyticsTokens))

	// System
	mux.HandleFunc("/api/system/info", withCORS(s.handleSystemInfo))
	mux.HandleFunc("/api/system/stats", withCORS(s.handleSystemStats))
	mux.HandleFunc("/api/system/health", withCORS(s.handleSystemHealth))

	// Logs
	mux.HandleFunc("/api/logs", withCORS(s.handleLogs))
	mux.HandleFunc("/api/dashboard/logs", withCORS(s.handleDashboardLogs))

	// Themes
	mux.HandleFunc("/api/dashboard/themes", withCORS(s.handleDashboardThemes))

	// Settings
	mux.HandleFunc("/api/settings", withCORS(s.handleSettings))
	mux.HandleFunc("/api/settings/", withCORS(s.handleSettingByID))

	// WebSocket for streaming
	mux.HandleFunc("/api/chat/stream", withCORS(s.handleChatStream))

	// Static files
	mux.HandleFunc("/", s.handleStatic)

	addr := fmt.Sprintf(":%d", port)
	return http.ListenAndServe(addr, mux)
}

// handleHealth handles health check
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]string{"status": "healthy"})
}

// handleStatus handles status check
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"status":              "ok",
		"timestamp":           time.Now().Unix(),
		"version":             "1.0.0",
		"active_sessions":     len(s.sessions),
		"config_path":         "~/.magic/config.json",
		"config_version":      1,
		"env_path":            "~/.magic/.env",
		"gateway_exit_reason": nil,
		"gateway_health_url":  nil,
		"gateway_pid":         nil,
		"gateway_platforms":   map[string]PlatformStatus{},
		"gateway_running":     false,
		"gateway_state":       nil,
		"magic_home":          "~/.magic",
		"session_count":       len(s.sessions),
	})
}

// handleSessions handles session list and creation
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")
		
		limit := 20
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}
		
		offset := 0
		if offsetStr != "" {
			if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
				offset = o
			}
		}
		
		end := offset + limit
		if end > len(s.sessionList) {
			end = len(s.sessionList)
		}
		
		jsonResponse(w, map[string]interface{}{
			"sessions": s.sessionList[offset:end],
			"total":    len(s.sessionList),
			"limit":    limit,
		})
	case "POST":
		var req struct {
			Name     string `json:"name"`
			Title    string `json:"title"`
			Model    string `json:"model"`
			Platform string `json:"platform"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		
		now := time.Now().Unix()
		name := req.Name
		if name == "" {
			name = req.Title
		}
		if name == "" {
			name = fmt.Sprintf("Chat %d", now)
		}
		
		model := req.Model
		if model == "" {
			model = "gpt-4"
		}
		
		session := &Session{
			ID:             fmt.Sprintf("s_%d", time.Now().UnixNano()),
			Source:         "cli",
			Model:          model,
			Title:          name,
			StartedAt:      now,
			EndedAt:        nil,
			LastActive:     now,
			IsActive:       false,
			MessageCount:   0,
			ToolCallCount:  0,
			InputTokens:    0,
			OutputTokens:   0,
			Preview:        "",
			ParentSessionID: nil,
		}
		s.sessions[session.ID] = session
		s.sessionList = append(s.sessionList, session)
		jsonResponse(w, session)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// handleSessionByID handles single session operations
func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	
	// Check for messages endpoint
	if strings.HasSuffix(path, "/messages") {
		sessionID := strings.TrimSuffix(path, "/messages")
		s.handleSessionMessages(w, r, sessionID)
		return
	}
	
	id := path
	if id == "" {
		http.Error(w, "not found", 404)
		return
	}

	session, ok := s.sessions[id]
	if !ok {
		http.Error(w, "not found", 404)
		return
	}

	switch r.Method {
	case "GET":
		jsonResponse(w, session)
	case "DELETE":
		delete(s.sessions, id)
		for i, item := range s.sessionList {
			if item.ID == id {
				s.sessionList = append(s.sessionList[:i], s.sessionList[i+1:]...)
				break
			}
		}
		delete(s.messages, id)
		w.WriteHeader(204)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// handleSessionMessages handles session messages
func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	
	msgs := s.messages[sessionID]
	if msgs == nil {
		msgs = []*Message{}
	}
	
	messages := make([]map[string]interface{}, len(msgs))
	for i, msg := range msgs {
		messages[i] = map[string]interface{}{
			"id":         msg.ID,
			"role":       msg.Role,
			"content":    msg.Content,
			"timestamp":  msg.Timestamp,
			"tool_calls": msg.ToolCalls,
			"tool_name":  msg.ToolName,
			"tool_call_id": msg.ToolCallID,
		}
	}
	
	jsonResponse(w, map[string]interface{}{
		"session_id": sessionID,
		"messages":   messages,
	})
}

// handleSessionSearch handles session search
func (s *Server) handleSessionSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	
	query := r.URL.Query().Get("q")
	results := []map[string]interface{}{}
	
	for _, session := range s.sessionList {
		if strings.Contains(strings.ToLower(session.Title), strings.ToLower(query)) ||
		   strings.Contains(strings.ToLower(session.Model), strings.ToLower(query)) {
			results = append(results, map[string]interface{}{
				"session_id":       session.ID,
				"snippet":          session.Preview,
				"role":             nil,
				"source":           session.Source,
				"model":            session.Model,
				"session_started":  session.StartedAt,
			})
		}
	}
	
	jsonResponse(w, map[string]interface{}{
		"results": results,
	})
}

// handleChat handles chat messages
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	var req struct {
		Message    string                   `json:"message"`
		SessionID string                   `json:"session_id"`
		Model     string                   `json:"model"`
		Messages  []map[string]interface{} `json:"messages"`
		Tools     []map[string]interface{} `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	// Check if last message has tool_calls
	var hasToolCalls bool
	var toolCalls []interface{}
	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		if tc, ok := lastMsg["tool_calls"].([]interface{}); ok && len(tc) > 0 {
			hasToolCalls = true
			toolCalls = tc
		}
	}

	// If there are tool_calls in the message, return tool results
	if hasToolCalls {
		toolMessages := make([]map[string]interface{}, 0)
		for i, tc := range toolCalls {
			if tcMap, ok := tc.(map[string]interface{}); ok {
				toolMessages = append(toolMessages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": tcMap["id"],
					"content":      fmt.Sprintf("Tool executed successfully (mock result %d)", i+1),
				})
			}
		}
		// Return tool results
		response := map[string]interface{}{
			"id":            fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			"content":       "Tool executed successfully",
			"model":         req.Model,
			"tool_messages": toolMessages,
			"tool_call_id":  "mock_tool_call_id",
		}
		jsonResponse(w, response)
		return
	}

	// Return a mock response
	response := map[string]interface{}{
		"id":      fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		"content": fmt.Sprintf("Echo: %s", req.Message),
		"model":   req.Model,
	}
	jsonResponse(w, response)
}

// handleToolsets handles toolset list
func (s *Server) handleToolsets(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, s.toolsets)
}

// handleToolsetByID handles single toolset
func (s *Server) handleToolsetByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tools/toolsets/")
	id = strings.TrimPrefix(id, "/api/toolsets/")
	
	for _, ts := range s.toolsets {
		if ts.ID == id {
			jsonResponse(w, ts)
			return
		}
	}
	http.Error(w, "not found", 404)
}

// handleToolCategories handles tool categories
func (s *Server) handleToolCategories(w http.ResponseWriter, r *http.Request) {
	cats := map[string][]Toolset{
		"browser": {{ID: "browser", Name: "Browser", Tools: []string{"browser_navigate", "browser_snapshot", "browser_click", "browser_type"}, Enabled: true}},
	}
	jsonResponse(w, cats)
}

// handleToolByID handles single tool
func (s *Server) handleToolByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/tools/")
	tool := map[string]interface{}{
		"id":          id,
		"name":        id,
		"description": fmt.Sprintf("Tool: %s", id),
		"parameters":  map[string]interface{}{},
	}
	jsonResponse(w, tool)
}

// handleSkills handles skill list
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, s.skills)
}

// handleSkillCategories handles skill categories
func (s *Server) handleSkillCategories(w http.ResponseWriter, r *http.Request) {
	cats := []string{"development", "research", "analytics"}
	jsonResponse(w, cats)
}

// handleSkillByID handles single skill
func (s *Server) handleSkillByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/skills/")
	for _, skill := range s.skills {
		if skill.ID == id {
			jsonResponse(w, skill)
			return
		}
	}
	http.Error(w, "not found", 404)
}

// handlePlugins handles plugin list
func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	plugins := []map[string]interface{}{
		{
			"id":          "welcome",
			"name":        "Welcome",
			"description": "Welcome plugin",
			"enabled":     true,
			"version":     "1.0.0",
		},
	}
	jsonResponse(w, plugins)
}

// handleDashboardSkills handles dashboard skill view
func (s *Server) handleDashboardSkills(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"installed": s.skills,
		"available": []Skill{},
		"categories": []string{"development", "research", "analytics"},
	})
}

// handleSkillsSearch handles skill search
func (s *Server) handleSkillsSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	var results []Skill
	for _, skill := range s.skills {
		if strings.Contains(strings.ToLower(skill.Name), strings.ToLower(query)) {
			results = append(results, skill)
		}
	}
	jsonResponse(w, results)
}

// handleDashboardPlugins handles dashboard plugins
func (s *Server) handleDashboardPlugins(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, []map[string]interface{}{
		{
			"id":   "theme-default",
			"name": "Default Theme",
			"type": "theme",
		},
		{
			"id":   "theme-dark",
			"name": "Dark Theme",
			"type": "theme",
		},
	})
}

// handleDashboardPluginsRescan handles plugin rescan
func (s *Server) handleDashboardPluginsRescan(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"scanned": 0,
		"found":   0,
	})
}

// handleModels handles model list
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	var models []map[string]interface{}
	for _, p := range s.providers {
		for _, m := range p.Models {
			models = append(models, map[string]interface{}{
				"id":         fmt.Sprintf("%s/%s", p.Name, m),
				"name":       m,
				"provider":   p.Name,
				"contextLen": 128000,
			})
		}
	}
	jsonResponse(w, models)
}

// handleModelByID handles single model
func (s *Server) handleModelByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/models/")
	model := map[string]interface{}{
		"id":         id,
		"name":       id,
		"contextLen": 128000,
	}
	jsonResponse(w, model)
}

// handleModelAuxiliary handles auxiliary model
func (s *Server) handleModelAuxiliary(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"id":         "auto",
		"name":       "Auto",
		"contextLen": 128000,
	})
}

// handleProviders handles provider list
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, s.providers)
}

// handleConfig handles configuration
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		jsonResponse(w, map[string]interface{}{
			"language": "en",
			"theme":    "dark",
			"model":    "openai/gpt-4",
		})
	case "PUT":
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		jsonResponse(w, req)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// handleConfigByID handles single config
func (s *Server) handleConfigByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/config/")
	jsonResponse(w, map[string]interface{}{"id": id, "value": ""})
}

// handleSystemInfo handles system information
func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"name":    "go-magic",
		"version": "1.0.0",
		"status":  "running",
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
		"go":      runtime.Version(),
	})
}

// handleSystemStats handles system statistics
func (s *Server) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	sessionCount := len(s.sessions)
	s.mu.RUnlock()
	
	jsonResponse(w, map[string]interface{}{
		"sessions":     sessionCount,
		"messages":     0,
		"uptime":       time.Since(s.startTime).String(),
		"memory_usage": "N/A",
	})
}

// handleSystemHealth handles system health
func (s *Server) handleSystemHealth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"status": "healthy",
		"checks": map[string]string{
			"server":   "ok",
			"database": "ok",
			"llm":      "not_configured",
		},
	})
}

// handleAnalyticsModels handles model analytics
func (s *Server) handleAnalyticsModels(w http.ResponseWriter, r *http.Request) {
	days := r.URL.Query().Get("days")
	if days == "" {
		days = "30"
	}
	jsonResponse(w, map[string]interface{}{
		"models": []map[string]interface{}{
			{"model": "gpt-4", "requests": 100, "tokens": 50000},
			{"model": "gpt-3.5-turbo", "requests": 200, "tokens": 80000},
		},
		"totals": map[string]interface{}{
			"distinct_models":      2,
			"total_input":          100000,
			"total_output":         50000,
			"total_cache_read":     0,
			"total_reasoning":      0,
			"total_estimated_cost": 25.50,
			"total_actual_cost":    25.50,
			"total_sessions":       50,
		},
	})
}

// handleAnalyticsUsage handles usage analytics
func (s *Server) handleAnalyticsUsage(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"daily":   []map[string]interface{}{{"date": time.Now().Format("2006-01-02"), "requests": 10}},
		"monthly": []map[string]interface{}{{"month": time.Now().Format("2006-01"), "requests": 300}},
	})
}

// handleAnalyticsCost handles cost analytics
func (s *Server) handleAnalyticsCost(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"total":   25.50,
		"currency": "USD",
		"breakdown": []map[string]interface{}{
			{"model": "gpt-4", "cost": 15.00},
			{"model": "gpt-3.5-turbo", "cost": 10.50},
		},
	})
}

// handleAnalyticsTokens handles token analytics
func (s *Server) handleAnalyticsTokens(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"input":  100000,
		"output": 50000,
		"total":  150000,
	})
}

// handleLogs handles log list
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "100"
	}
	n, _ := strconv.Atoi(limit)
	
	var logs []LogEntry
	for i := 0; i < n && i < 100; i++ {
		logs = append(logs, LogEntry{
			Timestamp: time.Now().Add(-time.Duration(i) * time.Minute),
			Level:     "info",
			Message:   fmt.Sprintf("Log entry %d", i),
			Source:    "system",
		})
	}
	jsonResponse(w, map[string]interface{}{
		"file":  "server.log",
		"lines": logs,
	})
}

// handleDashboardLogs handles dashboard logs
func (s *Server) handleDashboardLogs(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"logs": []LogEntry{
			{Timestamp: time.Now(), Level: "info", Message: "Server started", Source: "system"},
		},
		"stats": map[string]interface{}{
			"total":   100,
			"errors":   5,
			"warnings": 10,
		},
	})
}

// handleDashboardThemes handles dashboard themes
func (s *Server) handleDashboardThemes(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"themes": []map[string]interface{}{
			{
				"name":        "default",
				"label":       "Magic Teal",
				"description": "Classic dark teal — the canonical Magic look",
				"palette": map[string]interface{}{
					"primary":      "#0d9488",
					"primaryLight": "#14b8a6",
					"primaryDark":  "#0f766e",
					"accent":       "#8b5cf6",
					"accentLight":  "#a78bfa",
					"accentDark":   "#7c3aed",
					"background":   "#0f172a",
					"surface":      "#1e293b",
					"surfaceLight": "#334155",
					"text":         "#f8fafc",
					"textMuted":    "#94a3b8",
					"textDim":      "#64748b",
					"border":       "#334155",
					"success":      "#22c55e",
					"warning":      "#f59e0b",
					"error":        "#ef4444",
					"info":         "#3b82f6",
				},
				"typography": map[string]interface{}{
					"fontFamily":    "Inter, system-ui, sans-serif",
					"fontFamilyMono": "JetBrains Mono, monospace",
					"fontSizeSm":     "0.875rem",
					"fontSize":       "1rem",
					"fontSizeLg":     "1.125rem",
					"fontSizeXl":     "1.25rem",
					"fontSize2Xl":    "1.5rem",
					"fontWeightNormal": "400",
					"fontWeightMedium": "500",
					"fontWeightBold":   "600",
				},
				"layout": map[string]interface{}{
					"sidebarWidth":         "256px",
					"sidebarWidthCollapsed": "64px",
					"headerHeight":         "64px",
					"footerHeight":         "48px",
					"contentPadding":       "1rem",
					"radius":               "0.5rem",
					"radiusLg":             "0.75rem",
					"radiusSm":             "0.25rem",
				},
				"layoutVariant": "standard",
				"colorOverrides": map[string]interface{}{
					"card":                 "#1e293b",
					"cardForeground":       "#f8fafc",
					"popover":              "#1e293b",
					"popoverForeground":    "#f8fafc",
					"primary":              "#0d9488",
					"primaryForeground":    "#ffffff",
					"secondary":            "#334155",
					"secondaryForeground":  "#f8fafc",
					"muted":                "#334155",
					"mutedForeground":      "#94a3b8",
					"accent":               "#8b5cf6",
					"accentForeground":     "#ffffff",
					"destructive":          "#ef4444",
					"destructiveForeground": "#ffffff",
					"success":              "#22c55e",
					"warning":              "#f59e0b",
					"border":               "#334155",
					"input":                "#334155",
					"ring":                 "#0d9488",
				},
			},
			{
				"name":        "midnight",
				"label":       "Midnight",
				"description": "Deep blue-violet with cool accents",
				"palette": map[string]interface{}{
					"primary":      "#6366f1",
					"primaryLight": "#818cf8",
					"primaryDark":  "#4f46e5",
					"accent":       "#c084fc",
					"accentLight":  "#d8b4fe",
					"accentDark":   "#a855f7",
					"background":   "#0a0a1a",
					"surface":      "#17172a",
					"surfaceLight": "#272744",
					"text":         "#fafafa",
					"textMuted":    "#a1a1aa",
					"textDim":      "#71717a",
					"border":       "#272744",
					"success":      "#22c55e",
					"warning":      "#f59e0b",
					"error":        "#ef4444",
					"info":         "#3b82f6",
				},
				"typography": map[string]interface{}{
					"fontFamily":    "Inter, system-ui, sans-serif",
					"fontFamilyMono": "JetBrains Mono, monospace",
					"fontSizeSm":     "0.875rem",
					"fontSize":       "1rem",
					"fontSizeLg":     "1.125rem",
					"fontSizeXl":     "1.25rem",
					"fontSize2Xl":    "1.5rem",
					"fontWeightNormal": "400",
					"fontWeightMedium": "500",
					"fontWeightBold":   "600",
				},
				"layout": map[string]interface{}{
					"sidebarWidth":         "256px",
					"sidebarWidthCollapsed": "64px",
					"headerHeight":         "64px",
					"footerHeight":         "48px",
					"contentPadding":       "1rem",
					"radius":               "0.5rem",
					"radiusLg":             "0.75rem",
					"radiusSm":             "0.25rem",
				},
				"layoutVariant": "standard",
				"colorOverrides": map[string]interface{}{
					"card":                 "#17172a",
					"cardForeground":       "#fafafa",
					"popover":              "#17172a",
					"popoverForeground":    "#fafafa",
					"primary":              "#6366f1",
					"primaryForeground":    "#ffffff",
					"secondary":            "#272744",
					"secondaryForeground":  "#fafafa",
					"muted":                "#272744",
					"mutedForeground":      "#a1a1aa",
					"accent":               "#c084fc",
					"accentForeground":     "#ffffff",
					"destructive":          "#ef4444",
					"destructiveForeground": "#ffffff",
					"success":              "#22c55e",
					"warning":              "#f59e0b",
					"border":               "#272744",
					"input":                "#272744",
					"ring":                 "#6366f1",
				},
			},
			{
				"name":        "ember",
				"label":       "Ember",
				"description": "Warm crimson and bronze — forge vibes",
				"palette": map[string]interface{}{
					"primary":      "#dc2626",
					"primaryLight": "#ef4444",
					"primaryDark":  "#b91c1c",
					"accent":       "#d97706",
					"accentLight":  "#f59e0b",
					"accentDark":   "#b45309",
					"background":   "#1a0a0a",
					"surface":      "#2d1f1f",
					"surfaceLight": "#4a3030",
					"text":         "#fef3c7",
					"textMuted":    "#d97706",
					"textDim":      "#92400e",
					"border":       "#4a3030",
					"success":      "#22c55e",
					"warning":      "#f59e0b",
					"error":        "#dc2626",
					"info":         "#3b82f6",
				},
				"typography": map[string]interface{}{
					"fontFamily":    "Inter, system-ui, sans-serif",
					"fontFamilyMono": "JetBrains Mono, monospace",
					"fontSizeSm":     "0.875rem",
					"fontSize":       "1rem",
					"fontSizeLg":     "1.125rem",
					"fontSizeXl":     "1.25rem",
					"fontSize2Xl":    "1.5rem",
					"fontWeightNormal": "400",
					"fontWeightMedium": "500",
					"fontWeightBold":   "600",
				},
				"layout": map[string]interface{}{
					"sidebarWidth":         "256px",
					"sidebarWidthCollapsed": "64px",
					"headerHeight":         "64px",
					"footerHeight":         "48px",
					"contentPadding":       "1rem",
					"radius":               "0.5rem",
					"radiusLg":             "0.75rem",
					"radiusSm":             "0.25rem",
				},
				"layoutVariant": "standard",
				"colorOverrides": map[string]interface{}{
					"card":                 "#2d1f1f",
					"cardForeground":       "#fef3c7",
					"popover":              "#2d1f1f",
					"popoverForeground":    "#fef3c7",
					"primary":              "#dc2626",
					"primaryForeground":    "#ffffff",
					"secondary":            "#4a3030",
					"secondaryForeground":  "#fef3c7",
					"muted":                "#4a3030",
					"mutedForeground":      "#d97706",
					"accent":               "#d97706",
					"accentForeground":     "#1a0a0a",
					"destructive":          "#dc2626",
					"destructiveForeground": "#ffffff",
					"success":              "#22c55e",
					"warning":              "#f59e0b",
					"border":               "#4a3030",
					"input":                "#4a3030",
					"ring":                 "#dc2626",
				},
			},
			{
				"name":        "mono",
				"label":       "Mono",
				"description": "Clean grayscale — minimal and focused",
				"palette": map[string]interface{}{
					"primary":      "#6b7280",
					"primaryLight": "#9ca3af",
					"primaryDark":  "#4b5563",
					"accent":       "#374151",
					"accentLight":  "#4b5563",
					"accentDark":   "#1f2937",
					"background":   "#0f172a",
					"surface":      "#1e293b",
					"surfaceLight": "#334155",
					"text":         "#f1f5f9",
					"textMuted":    "#94a3b8",
					"textDim":      "#64748b",
					"border":       "#334155",
					"success":      "#22c55e",
					"warning":      "#f59e0b",
					"error":        "#ef4444",
					"info":         "#3b82f6",
				},
				"typography": map[string]interface{}{
					"fontFamily":    "Inter, system-ui, sans-serif",
					"fontFamilyMono": "JetBrains Mono, monospace",
					"fontSizeSm":     "0.875rem",
					"fontSize":       "1rem",
					"fontSizeLg":     "1.125rem",
					"fontSizeXl":     "1.25rem",
					"fontSize2Xl":    "1.5rem",
					"fontWeightNormal": "400",
					"fontWeightMedium": "500",
					"fontWeightBold":   "600",
				},
				"layout": map[string]interface{}{
					"sidebarWidth":         "256px",
					"sidebarWidthCollapsed": "64px",
					"headerHeight":         "64px",
					"footerHeight":         "48px",
					"contentPadding":       "1rem",
					"radius":               "0.5rem",
					"radiusLg":             "0.75rem",
					"radiusSm":             "0.25rem",
				},
				"layoutVariant": "standard",
				"colorOverrides": map[string]interface{}{
					"card":                 "#1e293b",
					"cardForeground":       "#f1f5f9",
					"popover":              "#1e293b",
					"popoverForeground":    "#f1f5f9",
					"primary":              "#6b7280",
					"primaryForeground":    "#ffffff",
					"secondary":            "#334155",
					"secondaryForeground":  "#f1f5f9",
					"muted":                "#334155",
					"mutedForeground":      "#94a3b8",
					"accent":               "#374151",
					"accentForeground":     "#f1f5f9",
					"destructive":          "#ef4444",
					"destructiveForeground": "#ffffff",
					"success":              "#22c55e",
					"warning":              "#f59e0b",
					"border":               "#334155",
					"input":                "#334155",
					"ring":                 "#6b7280",
				},
			},
			{
				"name":        "cyberpunk",
				"label":       "Cyberpunk",
				"description": "Neon green on black — matrix terminal",
				"palette": map[string]interface{}{
					"primary":      "#22c55e",
					"primaryLight": "#4ade80",
					"primaryDark":  "#16a34a",
					"accent":       "#8b5cf6",
					"accentLight":  "#a78bfa",
					"accentDark":   "#7c3aed",
					"background":   "#020617",
					"surface":      "#0f172a",
					"surfaceLight": "#1e293b",
					"text":         "#22c55e",
					"textMuted":    "#4ade80",
					"textDim":      "#16a34a",
					"border":       "#1e293b",
					"success":      "#22c55e",
					"warning":      "#f59e0b",
					"error":        "#ef4444",
					"info":         "#3b82f6",
				},
				"typography": map[string]interface{}{
					"fontFamily":    "Inter, system-ui, sans-serif",
					"fontFamilyMono": "JetBrains Mono, monospace",
					"fontSizeSm":     "0.875rem",
					"fontSize":       "1rem",
					"fontSizeLg":     "1.125rem",
					"fontSizeXl":     "1.25rem",
					"fontSize2Xl":    "1.5rem",
					"fontWeightNormal": "400",
					"fontWeightMedium": "500",
					"fontWeightBold":   "600",
				},
				"layout": map[string]interface{}{
					"sidebarWidth":         "256px",
					"sidebarWidthCollapsed": "64px",
					"headerHeight":         "64px",
					"footerHeight":         "48px",
					"contentPadding":       "1rem",
					"radius":               "0.5rem",
					"radiusLg":             "0.75rem",
					"radiusSm":             "0.25rem",
				},
				"layoutVariant": "standard",
				"colorOverrides": map[string]interface{}{
					"card":                 "#0f172a",
					"cardForeground":       "#22c55e",
					"popover":              "#0f172a",
					"popoverForeground":    "#22c55e",
					"primary":              "#22c55e",
					"primaryForeground":    "#020617",
					"secondary":            "#1e293b",
					"secondaryForeground":  "#22c55e",
					"muted":                "#1e293b",
					"mutedForeground":      "#4ade80",
					"accent":               "#8b5cf6",
					"accentForeground":     "#020617",
					"destructive":          "#ef4444",
					"destructiveForeground": "#020617",
					"success":              "#22c55e",
					"warning":              "#f59e0b",
					"border":               "#1e293b",
					"input":                "#1e293b",
					"ring":                 "#22c55e",
				},
			},
			{
				"name":        "rose",
				"label":       "Rosé",
				"description": "Soft pink and warm ivory — easy on the eyes",
				"palette": map[string]interface{}{
					"primary":      "#f472b6",
					"primaryLight": "#f9a8d4",
					"primaryDark":  "#ec4899",
					"accent":       "#fb923c",
					"accentLight":  "#fdba74",
					"accentDark":   "#f97316",
					"background":   "#1c1917",
					"surface":      "#292524",
					"surfaceLight": "#44403c",
					"text":         "#fef3c7",
					"textMuted":    "#d6d3d1",
					"textDim":      "#a8a29e",
					"border":       "#44403c",
					"success":      "#22c55e",
					"warning":      "#f59e0b",
					"error":        "#ef4444",
					"info":         "#3b82f6",
				},
				"typography": map[string]interface{}{
					"fontFamily":    "Inter, system-ui, sans-serif",
					"fontFamilyMono": "JetBrains Mono, monospace",
					"fontSizeSm":     "0.875rem",
					"fontSize":       "1rem",
					"fontSizeLg":     "1.125rem",
					"fontSizeXl":     "1.25rem",
					"fontSize2Xl":    "1.5rem",
					"fontWeightNormal": "400",
					"fontWeightMedium": "500",
					"fontWeightBold":   "600",
				},
				"layout": map[string]interface{}{
					"sidebarWidth":         "256px",
					"sidebarWidthCollapsed": "64px",
					"headerHeight":         "64px",
					"footerHeight":         "48px",
					"contentPadding":       "1rem",
					"radius":               "0.5rem",
					"radiusLg":             "0.75rem",
					"radiusSm":             "0.25rem",
				},
				"layoutVariant": "standard",
				"colorOverrides": map[string]interface{}{
					"card":                 "#292524",
					"cardForeground":       "#fef3c7",
					"popover":              "#292524",
					"popoverForeground":    "#fef3c7",
					"primary":              "#f472b6",
					"primaryForeground":    "#1c1917",
					"secondary":            "#44403c",
					"secondaryForeground":  "#fef3c7",
					"muted":                "#44403c",
					"mutedForeground":      "#d6d3d1",
					"accent":               "#fb923c",
					"accentForeground":     "#1c1917",
					"destructive":          "#ef4444",
					"destructiveForeground": "#1c1917",
					"success":              "#22c55e",
					"warning":              "#f59e0b",
					"border":               "#44403c",
					"input":                "#44403c",
					"ring":                 "#f472b6",
				},
			},
		},
		"active": "default",
	})
}

// handleSettings handles settings
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"general": map[string]interface{}{
			"language": "en",
			"timezone": "UTC",
		},
		"appearance": map[string]interface{}{
			"theme":    "dark",
			"fontSize": 14,
		},
	})
}

// handleSettingByID handles single setting
func (s *Server) handleSettingByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/settings/")
	jsonResponse(w, map[string]interface{}{"id": id, "value": ""})
}

// handleChatStream handles streaming chat
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Send streaming response
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	message := fmt.Sprintf("Echo: %s", req.Message)
	for i := 0; i < len(message); i++ {
		fmt.Fprintf(w, "data: %s\n\n", string(message[i]))
		flusher.Flush()
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// handleStatic serves static files with SPA fallback
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Skip API routes
	if strings.HasPrefix(path, "/api/") {
		http.Error(w, "not found", 404)
		return
	}

	// Root SPA fallback
	if path == "/" || path == "" {
		data, err := staticFiles.ReadFile("dist/index.html")
		if err != nil {
			http.Error(w, "internal server error", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
		return
	}

	// Try embedded files first
	data, err := staticFiles.ReadFile("dist" + path)
	if err == nil {
		contentType := getContentType(path)
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
		return
	}

	// Serve index.html for SPA routes (remove trailing slash issues)
	spaPaths := []string{"/sessions", "/logs", "/skills", "/tools", "/config", "/settings"}
	for _, spa := range spaPaths {
		if strings.HasPrefix(path, spa) {
			data, err := staticFiles.ReadFile("dist/index.html")
			if err == nil {
				w.Header().Set("Content-Type", "text/html")
				w.Write(data)
				return
			}
		}
	}

	http.Error(w, "not found", 404)
}

func getContentType(path string) string {
	ext := strings.ToLower(path[strings.LastIndex(path, ".")+1:])
	switch ext {
	case "js":
		return "application/javascript"
	case "css":
		return "text/css"
	case "html":
		return "text/html"
	case "json":
		return "application/json"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "svg":
		return "image/svg+xml"
	case "woff", "woff2":
		return "font/woff2"
	case "ttf":
		return "font/ttf"
	case "eot":
		return "application/vnd.ms-fontobject"
	case "ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}