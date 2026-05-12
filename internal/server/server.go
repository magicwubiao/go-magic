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
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Model     string    `json:"model"`
	Platform  string    `json:"platform"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Message represents a chat message
type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
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

// HealthResponse represents health check response
type HealthResponse struct {
	Status string `json:"status"`
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
	messages    []*Message
}

// NewServer creates a new server instance
func NewServer(dbPath string) *Server {
	return &Server{
		mu:         sync.RWMutex{},
		startTime:  time.Now(),
		sessions:   make(map[string]*Session),
		sessionList: []*Session{},
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
	jsonResponse(w, HealthResponse{Status: "healthy"})
}

// handleStatus handles status check
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
		"version":   "1.0.0",
	})
}

// handleSessions handles session list and creation
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		jsonResponse(w, s.sessionList)
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
		name := req.Name
		if name == "" {
			name = req.Title
		}
		if name == "" {
			name = fmt.Sprintf("Chat %d", time.Now().Unix())
		}
		model := req.Model
		if model == "" {
			model = "gpt-4"
		}
		platform := req.Platform
		if platform == "" {
			platform = "web"
		}
		session := &Session{
			ID:        fmt.Sprintf("s_%d", time.Now().UnixNano()),
			Name:      name,
			Model:     model,
			Platform:  platform,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
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
	id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
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
		w.WriteHeader(204)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// handleChat handles chat messages
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
		Model     string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	// Return a mock response for now
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
	id = strings.TrimPrefix(r.URL.Path, "/api/toolsets/")
	
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
	messageCount := len(s.messages)
	s.mu.RUnlock()
	
	jsonResponse(w, map[string]interface{}{
		"sessions":     sessionCount,
		"messages":     messageCount,
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
	jsonResponse(w, []map[string]interface{}{
		{"model": "gpt-4", "requests": 100, "tokens": 50000},
		{"model": "gpt-3.5-turbo", "requests": 200, "tokens": 80000},
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
	jsonResponse(w, logs)
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
	themes := []map[string]interface{}{
		{"id": "dark", "name": "Dark", "preview": "#1a1a2e"},
		{"id": "light", "name": "Light", "preview": "#ffffff"},
		{"id": "cyber", "name": "Cyber", "preview": "#00ff41"},
	}
	jsonResponse(w, themes)
}

// handleSettings handles settings
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"general": map[string]interface{}{
			"language": "en",
			"timezone": "UTC",
		},
		"appearance": map[string]interface{}{
			"theme": "dark",
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
	data, err := staticFiles.ReadFile(path[1:])
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
