package server

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed dist
var staticFiles embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Session represents a chat session
type Session struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Model         string    `json:"model,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Messages      []Message `json:"messages,omitempty"`
	MessageCount  int       `json:"message_count,omitempty"`
}

// Message represents a chat message
type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"` // user, assistant, system
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Tools     []ToolCall `json:"tools,omitempty"`
}

// ToolCall represents a tool invocation
type ToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	Status    string `json:"status"` // pending, running, done, error
}

// Server represents the web server
type Server struct {
	port            string
	db              *sql.DB
	sessions        map[string]*Session
	sessionsMux     sync.RWMutex
	mux             *http.ServeMux
	httpServer      *http.Server
	startTime       time.Time
	toolsets        []map[string]interface{}
	skills          []map[string]interface{}
	config          map[string]interface{}
	logs            []map[string]interface{}
}

// Storage for in-memory data
type Storage struct {
	sessions   map[string]*Session
	sessionsMux sync.RWMutex
}

func NewStorage() *Storage {
	return &Storage{
		sessions: make(map[string]*Session),
	}
}

func (s *Storage) CreateSession(name, model string) *Session {
	session := &Session{
		ID:        generateID(),
		Name:      name,
		Model:     model,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages:  []Message{},
	}
	s.sessionsMux.Lock()
	s.sessions[session.ID] = session
	s.sessionsMux.Unlock()
	return session
}

func (s *Storage) GetSession(id string) *Session {
	s.sessionsMux.RLock()
	defer s.sessionsMux.RUnlock()
	return s.sessions[id]
}

func (s *Storage) ListSessions() []*Session {
	s.sessionsMux.RLock()
	defer s.sessionsMux.RUnlock()
	result := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		result = append(result, session)
	}
	return result
}

func (s *Storage) DeleteSession(id string) bool {
	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()
	if _, ok := s.sessions[id]; ok {
		delete(s.sessions, id)
		return true
	}
	return false
}

func (s *Storage) UpdateSession(id string, fn func(*Session)) bool {
	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return false
	}
	fn(session)
	session.UpdatedAt = time.Now()
	return true
}

func NewServer(port string, db *sql.DB) *Server {
	s := &Server{
		port:       port,
		db:         db,
		startTime:  time.Now(),
		logs:       []map[string]interface{}{},
	}
	s.initData()
	return s
}

func (s *Server) initData() {
	// Initialize toolsets from actual tool system
	s.toolsets = []map[string]interface{}{
		{
			"name":        "web",
			"description": "Web search and content extraction",
			"enabled":     true,
			"tools": []map[string]interface{}{
				{"name": "web_search", "description": "Search the web for information"},
				{"name": "web_extract", "description": "Extract content from URLs"},
			},
		},
		{
			"name":        "file",
			"description": "File operations",
			"enabled":     true,
			"tools": []map[string]interface{}{
				{"name": "read_file", "description": "Read file contents"},
				{"name": "write_file", "description": "Write content to file"},
				{"name": "edit_file", "description": "Edit existing file"},
				{"name": "list_files", "description": "List directory contents"},
				{"name": "search_in_files", "description": "Search for text in files"},
			},
		},
		{
			"name":        "terminal",
			"description": "Terminal command execution",
			"enabled":     true,
			"tools": []map[string]interface{}{
				{"name": "execute_command", "description": "Execute shell commands"},
				{"name": "terminal", "description": "Interactive terminal"},
			},
		},
		{
			"name":        "browser",
			"description": "Browser automation",
			"enabled":     true,
			"tools": []map[string]interface{}{
				{"name": "browser_navigate", "description": "Navigate to URL"},
				{"name": "browser_snapshot", "description": "Get page snapshot"},
				{"name": "browser_click", "description": "Click element"},
				{"name": "browser_type", "description": "Type text"},
			},
		},
		{
			"name":        "memory",
			"description": "Persistent memory storage",
			"enabled":     true,
			"tools": []map[string]interface{}{
				{"name": "memory_store", "description": "Store information"},
				{"name": "memory_recall", "description": "Recall stored information"},
			},
		},
		{
			"name":        "utility",
			"description": "Utility functions",
			"enabled":     true,
			"tools": []map[string]interface{}{
				{"name": "json", "description": "JSON operations"},
				{"name": "uuid", "description": "Generate UUID"},
				{"name": "random", "description": "Generate random values"},
				{"name": "time", "description": "Time operations"},
			},
		},
	}

	// Initialize skills
	s.skills = []map[string]interface{}{
		{"name": "git-workflow", "category": "development", "description": "Git workflow helpers", "enabled": true},
		{"name": "docker-help", "category": "devops", "description": "Docker commands reference", "enabled": true},
		{"name": "code-review", "category": "development", "description": "Code review best practices", "enabled": true},
		{"name": "api-design", "category": "development", "description": "REST API design patterns", "enabled": false},
	}

	// Initialize config
	s.config = map[string]interface{}{
		"provider":     "openai",
		"model":        "gpt-4",
		"temperature":  0.7,
		"max_tokens":   4096,
		"theme":         "dark",
		"language":      "en",
		"streaming":     true,
		"system_prompt": "You are go-magic, a helpful AI assistant.",
	}
}

func (s *Server) addLog(level, message string) {
	s.logs = append(s.logs, map[string]interface{}{
		"id":        generateID(),
		"time":      time.Now().Format("15:04:05"),
		"timestamp": time.Now().Unix(),
		"level":     level,
		"message":   message,
	})
	// Keep only last 100 logs
	if len(s.logs) > 100 {
		s.logs = s.logs[len(s.logs)-100:]
	}
}

func (s *Server) Start() error {
	s.mux = http.NewServeMux()
	s.addLog("info", "Server starting on port "+s.port)

	// API routes
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/stats", s.handleStats)
	
	// Sessions
	s.mux.HandleFunc("/api/sessions", s.handleSessions)
	s.mux.HandleFunc("/api/sessions/", s.handleSession)
	
	// Chat
	s.mux.HandleFunc("/api/chat", s.handleChat)
	s.mux.HandleFunc("/api/chat/stream", s.handleChatStream)
	
	// Tools
	s.mux.HandleFunc("/api/toolsets", s.handleToolsets)
	s.mux.HandleFunc("/api/toolsets/", s.handleToolsetAction)
	s.mux.HandleFunc("/api/tools", s.handleTools)
	
	// Skills
	s.mux.HandleFunc("/api/skills", s.handleSkills)
	s.mux.HandleFunc("/api/skills/", s.handleSkillAction)
	
	// Config
	s.mux.HandleFunc("/api/config", s.handleConfig)
	
	// Logs
	s.mux.HandleFunc("/api/logs", s.handleLogs)
	
	// Platforms
	s.mux.HandleFunc("/api/platforms", s.handlePlatforms)
	
	// WebSocket
	s.mux.HandleFunc("/ws", s.handleWebSocket)

	// Serve static files
	s.mux.HandleFunc("/", s.handleStatic)

	addr := fmt.Sprintf(":%s", s.port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	s.addLog("info", fmt.Sprintf("Server listening on http://0.0.0.0%s", addr))
	log.Printf("Starting web server on http://0.0.0.0%s", addr)
	return s.httpServer.ListenAndServe()
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	s.addLog("info", "Server shutting down")
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// Health check
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"status":  "healthy",
		"version": "1.0.0",
		"time":    time.Now().Unix(),
	})
}

// Stats
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.sessionsMux.RLock()
	sessionCount := len(s.sessions)
	var messageCount int
	for _, session := range s.sessions {
		messageCount += len(session.Messages)
	}
	s.sessionsMux.RUnlock()
	
	jsonResponse(w, map[string]interface{}{
		"total_sessions":  sessionCount,
		"total_messages":  messageCount,
		"uptime_seconds":  int(time.Since(s.startTime).Seconds()),
		"toolset_count":   len(s.toolsets),
		"skill_count":     len(s.skills),
	})
}

// Sessions handlers
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.sessionsMux.RLock()
		sessions := make([]*Session, 0, len(s.sessions))
		for _, session := range s.sessions {
			// Return sessions without full messages for list view
			sessions = append(sessions, &Session{
				ID:        session.ID,
				Name:      session.Name,
				Model:     session.Model,
				CreatedAt: session.CreatedAt,
				UpdatedAt: session.UpdatedAt,
				MessageCount: len(session.Messages),
			})
		}
		s.sessionsMux.RUnlock()
		jsonResponse(w, sessions)

	case http.MethodPost:
		var req struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		
		if req.Name == "" {
			req.Name = fmt.Sprintf("Chat %d", time.Now().Unix())
		}
		if req.Model == "" {
			req.Model = s.config["model"].(string)
		}
		
		session := &Session{
			ID:        generateID(),
			Name:      req.Name,
			Model:     req.Model,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Messages:  []Message{},
		}
		
		s.sessionsMux.Lock()
		s.sessions[session.ID] = session
		s.sessionsMux.Unlock()
		
		s.addLog("info", fmt.Sprintf("Created session: %s", session.Name))
		jsonResponse(w, session)
	}
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	s.sessionsMux.RLock()
	session, ok := s.sessions[id]
	s.sessionsMux.RUnlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if action == "messages" {
			jsonResponse(w, session.Messages)
		} else {
			jsonResponse(w, session)
		}

	case http.MethodPut:
		var req struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Name != "" {
			s.sessionsMux.Lock()
			session.Name = req.Name
			session.UpdatedAt = time.Now()
			s.sessionsMux.Unlock()
			s.addLog("info", fmt.Sprintf("Renamed session to: %s", req.Name))
		}
		jsonResponse(w, session)

	case http.MethodDelete:
		s.sessionsMux.Lock()
		delete(s.sessions, id)
		s.sessionsMux.Unlock()
		s.addLog("info", fmt.Sprintf("Deleted session: %s", id))
		w.WriteHeader(http.StatusNoContent)

	case http.MethodPost:
		if action == "messages" {
			var req struct {
				Content string `json:"content"`
				Role    string `json:"role"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			
			msg := Message{
				ID:        generateID(),
				Role:      req.Role,
				Content:   req.Content,
				Timestamp: time.Now(),
			}
			
			s.sessionsMux.Lock()
			session.Messages = append(session.Messages, msg)
			session.UpdatedAt = time.Now()
			s.sessionsMux.Unlock()
			
			jsonResponse(w, msg)
		}
	}
}

// Chat handlers
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
		Model     string `json:"model"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Create or get session
	s.sessionsMux.Lock()
	var session *Session
	if req.SessionID != "" {
		session = s.sessions[req.SessionID]
	}
	if session == nil {
		session = &Session{
			ID:        generateID(),
			Name:      fmt.Sprintf("Chat %d", time.Now().Unix()),
			Model:     req.Model,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Messages:  []Message{},
		}
		s.sessions[session.ID] = session
	}
	s.sessionsMux.Unlock()

	// Add user message
	userMsg := Message{
		ID:        generateID(),
		Role:      "user",
		Content:   req.Message,
		Timestamp: time.Now(),
	}

	s.sessionsMux.Lock()
	session.Messages = append(session.Messages, userMsg)
	session.UpdatedAt = time.Now()
	s.sessionsMux.Unlock()

	// Generate response
	response := s.generateResponse(req.Message, session)

	// Add assistant message
	assistantMsg := Message{
		ID:        generateID(),
		Role:      "assistant",
		Content:   response,
		Timestamp: time.Now(),
	}

	s.sessionsMux.Lock()
	session.Messages = append(session.Messages, assistantMsg)
	session.UpdatedAt = time.Now()
	s.sessionsMux.Unlock()

	s.addLog("info", fmt.Sprintf("Chat message in session %s", session.ID))

	jsonResponse(w, map[string]interface{}{
		"session":  session,
		"response": assistantMsg,
	})
}

// SSE streaming chat
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
		Model     string `json:"model"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Create or get session
	s.sessionsMux.Lock()
	var session *Session
	if req.SessionID != "" {
		session = s.sessions[req.SessionID]
	}
	if session == nil {
		session = &Session{
			ID:        generateID(),
			Name:      fmt.Sprintf("Chat %d", time.Now().Unix()),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Messages:  []Message{},
		}
		s.sessions[session.ID] = session
	}

	// Add user message
	userMsg := Message{
		ID:        generateID(),
		Role:      "user",
		Content:   req.Message,
		Timestamp: time.Now(),
	}
	session.Messages = append(session.Messages, userMsg)
	s.sessionsMux.Unlock()

	// Send session info
	sessionJSON, _ := json.Marshal(map[string]interface{}{
		"type": "session",
		"data": session,
	})
	fmt.Fprintf(w, "data: %s\n\n", sessionJSON)
	flusher.Flush()

	// Generate and stream response
	response := s.generateResponse(req.Message, session)
	
	// Stream character by character
	assistantMsg := Message{
		ID:        generateID(),
		Role:      "assistant",
		Content:   "",
		Timestamp: time.Now(),
	}

	for _, char := range response {
		event := map[string]interface{}{
			"type": "chunk",
			"data": map[string]interface{}{
				"content": string(char),
			},
		}
		eventJSON, _ := json.Marshal(event)
		fmt.Fprintf(w, "data: %s\n\n", eventJSON)
		flusher.Flush()
		assistantMsg.Content += string(char)
		time.Sleep(5 * time.Millisecond)
	}

	// Add complete message to session
	s.sessionsMux.Lock()
	session.Messages = append(session.Messages, assistantMsg)
	session.UpdatedAt = time.Now()
	s.sessionsMux.Unlock()

	// Send done event
	doneEvent := map[string]interface{}{
		"type": "done",
		"data": assistantMsg,
	}
	doneJSON, _ := json.Marshal(doneEvent)
	fmt.Fprintf(w, "data: %s\n\n", doneJSON)
	flusher.Flush()

	s.addLog("info", fmt.Sprintf("Streamed response for session %s", session.ID))
}

// Generate AI response (simplified - would connect to real LLM in production)
func (s *Server) generateResponse(message string, session *Session) string {
	// Simple pattern-based responses for demo
	lower := strings.ToLower(message)
	
	// Build context from recent messages
	var context string
	s.sessionsMux.RLock()
	if len(session.Messages) > 1 {
		recent := session.Messages[len(session.Messages)-5 : len(session.Messages)-1]
		for _, msg := range recent {
			if msg.Role == "user" {
				context += "User: " + msg.Content + "\n"
			}
		}
	}
	s.sessionsMux.RUnlock()

	switch {
	case strings.Contains(lower, "hello") || strings.Contains(lower, "hi"):
		return "Hello! I'm go-magic, your AI assistant. How can I help you today?"
	
	case strings.Contains(lower, "help"):
		return "I can help you with:\n\n- **Chat**: Have conversations and get answers\n- **Code**: Write, review, and debug code\n- **Files**: Read, write, and manage files\n- **Terminal**: Execute shell commands\n- **Web**: Search the web and extract content\n\nWhat would you like to do?"
	
	case strings.Contains(lower, "who are you"):
		return "I'm **go-magic**, a high-performance AI Agent built with Go. I'm inspired by Nous Research's hermes-agent and support multiple providers (OpenAI, DeepSeek, etc.) and various messaging platforms."
	
	case strings.Contains(lower, "feature"):
		return "**go-magic Features:**\n\n- Multi-provider LLM support\n- Tool system (web, file, terminal, browser)\n- Skills system for extending capabilities\n- Message gateways (Telegram, Discord, WhatsApp)\n- Session management with memory\n- MCP protocol support\n\nWhat would you like to explore?"
	
	case strings.Contains(lower, "thanks") || strings.Contains(lower, "thank"):
		return "You're welcome! Is there anything else I can help you with?"
	
	default:
		return fmt.Sprintf("I received your message: \"%s\"\n\nAs an AI assistant, I can help you with various tasks. Try asking about:\n- \"What can you do?\"\n- \"Help me with coding\"\n- \"Show me your features\"", message)
	}
}

// Tools handlers
func (s *Server) handleToolsets(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		jsonResponse(w, s.toolsets)
	}
}

func (s *Server) handleToolsetAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/toolsets/")
	parts := strings.SplitN(path, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	// Find toolset
	var ts map[string]interface{}
	for _, t := range s.toolsets {
		if t["name"] == name {
			ts = t
			break
		}
	}

	if ts == nil {
		http.Error(w, "Toolset not found", http.StatusNotFound)
		return
	}

	if action == "toggle" && r.Method == http.MethodPost {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		ts["enabled"] = req.Enabled
		jsonResponse(w, ts)
		return
	}

	if r.Method == http.MethodGet {
		jsonResponse(w, ts)
	}
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	var tools []map[string]interface{}
	for _, ts := range s.toolsets {
		for _, t := range ts["tools"].([]map[string]interface{}) {
			t["toolset"] = ts["name"]
			tools = append(tools, t)
		}
	}
	jsonResponse(w, tools)
}

// Skills handlers
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		jsonResponse(w, s.skills)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		
		skill := map[string]interface{}{
			"name":        req.Name,
			"description": req.Description,
			"category":    req.Category,
			"enabled":     true,
		}
		s.skills = append(s.skills, skill)
		s.addLog("info", fmt.Sprintf("Added skill: %s", req.Name))
		jsonResponse(w, skill)
	}
}

func (s *Server) handleSkillAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/skills/")
	parts := strings.SplitN(path, "/", 2)
	name := parts[0]

	var skillData map[string]interface{}
	for i, sk := range s.skills {
		if sk["name"] == name {
			skillData = sk
			if r.Method == http.MethodDelete {
				s.skills = append(s.skills[:i], s.skills[i+1:]...)
				s.addLog("info", fmt.Sprintf("Deleted skill: %s", name))
				w.WriteHeader(http.StatusNoContent)
				return
			}
			break
		}
	}

	if skillData == nil {
		http.Error(w, "Skill not found", http.StatusNotFound)
		return
	}

	if r.Method == http.MethodGet {
		jsonResponse(w, skillData)
	}
}

// Config handlers
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, s.config)
	case http.MethodPut:
		var updates map[string]interface{}
		json.NewDecoder(r.Body).Decode(&updates)
		for k, v := range updates {
			s.config[k] = v
		}
		s.addLog("info", "Config updated")
		jsonResponse(w, s.config)
	}
}

// Logs handler
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.logs = []map[string]interface{}{}
	}
	
	// Add current request as log
	query := r.URL.Query()
	level := query.Get("level")
	if level == "" {
		level = "debug"
	}
	s.addLog(level, fmt.Sprintf("%s %s", r.Method, r.URL.Path))
	
	jsonResponse(w, s.logs)
}

// Platforms handler
func (s *Server) handlePlatforms(w http.ResponseWriter, r *http.Request) {
	platforms := []map[string]interface{}{
		{
			"name":    "telegram",
			"status":  "disconnected",
			"message": "Configure with: magic gateway setup telegram",
		},
		{
			"name":    "discord",
			"status":  "disconnected",
			"message": "Configure with: magic gateway setup discord",
		},
		{
			"name":    "whatsapp",
			"status":  "disconnected",
			"message": "Use 'magic gateway start' to connect",
		},
		{
			"name":    "slack",
			"status":  "disconnected",
			"message": "Not configured",
		},
	}
	jsonResponse(w, platforms)
}

// WebSocket handler
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var req struct {
			Type      string `json:"type"`
			Message   string `json:"message"`
			SessionID string `json:"session_id"`
		}
		json.Unmarshal(msg, &req)

		switch req.Type {
		case "chat":
			response := s.generateResponse(req.Message, nil)
			conn.WriteJSON(map[string]interface{}{
				"type":    "response",
				"content": response,
			})
		}
	}
}

// Static file handler
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	// Try embedded files
	data, err := staticFiles.ReadFile(filepath.Join("dist", path))
	if err == nil {
		contentType := getContentType(path)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
		return
	}

	// Try disk files
	diskPath := filepath.Join("web", "dist", path)
	data, err = os.ReadFile(diskPath)
	if err == nil {
		contentType := getContentType(path)
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
		return
	}

	// SPA fallback
	indexData, _ := staticFiles.ReadFile("/index.html")
	w.Header().Set("Content-Type", "text/html")
	w.Write(indexData)
}

// Helper functions
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func getContentType(path string) string {
	ext := filepath.Ext(path)
	types := map[string]string{
		".html": "text/html",
		".js":   "application/javascript",
		".css":  "text/css",
		".json": "application/json",
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".svg":  "image/svg+xml",
		".ico":  "image/x-icon",
		".woff": "font/woff",
		".woff2": "font/woff2",
	}
	if t, ok := types[ext]; ok {
		return t
	}
	return "application/octet-stream"
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
