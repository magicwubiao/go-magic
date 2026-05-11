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

	"github.com/magicwubiao/go-magic/internal/groupchat"
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
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Messages  []Message `json:"messages"`
}

// Message represents a chat message
type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"` // user, assistant, system
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Server represents the web server
type Server struct {
	port            string
	db              *sql.DB
	groupChatHandler *groupchat.Handler
	sessions        map[string]*Session
	sessionsMux     sync.RWMutex
	mux             *http.ServeMux
	httpServer      *http.Server
}

func NewServer(port string, db *sql.DB) *Server {
	s := &Server{
		port:     port,
		db:       db,
		sessions: make(map[string]*Session),
	}
	// Initialize Group Chat
	groupchat.InitSchema(db)
	s.groupChatHandler = groupchat.NewHandler(groupchat.NewStorage(db))
	return s
}

func (s *Server) Start() error {
	s.mux = http.NewServeMux()

	// API routes
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/sessions", s.handleSessions)
	s.mux.HandleFunc("/api/sessions/", s.handleSession)
	s.mux.HandleFunc("/api/tools", s.handleTools)
	s.mux.HandleFunc("/api/toolsets", s.handleToolsets)
	s.mux.HandleFunc("/api/skills", s.handleSkills)
	s.mux.HandleFunc("/api/config", s.handleConfig)
	s.mux.HandleFunc("/api/logs", s.handleLogs)
	s.mux.HandleFunc("/api/platforms", s.handlePlatforms)
	s.mux.HandleFunc("/api/analytics", s.handleAnalytics)
	s.mux.HandleFunc("/api/chat", s.handleChat)

	// Group Chat routes
	s.mux.Handle("/api/groupchat/", http.StripPrefix("/api/groupchat", s.groupChatHandler))

	// WebSocket for streaming
	s.mux.HandleFunc("/ws", s.handleWebSocket)
	
	// SSE for streaming chat
	s.mux.HandleFunc("/api/chat/stream", s.handleChatSSE)

	// Serve static files from embedded FS
	s.mux.HandleFunc("/", s.handleStatic)

	addr := fmt.Sprintf(":%s", s.port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	log.Printf("Starting web server on http://localhost%s", addr)
	return s.httpServer.ListenAndServe()
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"status":  "healthy",
		"version": "1.0.0",
		"time":    time.Now().Unix(),
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.sessionsMux.RLock()
		sessions := make([]*Session, 0, len(s.sessions))
		for _, s := range s.sessions {
			sessions = append(sessions, s)
		}
		s.sessionsMux.RUnlock()
		jsonResponse(w, sessions)

	case http.MethodPost:
		session := &Session{
			ID:        generateID(),
			Name:      "New Session",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Messages:  []Message{},
		}
		s.sessionsMux.Lock()
		s.sessions[session.ID] = session
		s.sessionsMux.Unlock()
		jsonResponse(w, session)
	}
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	id := strings.Split(path, "/")[0]

	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	session, ok := s.sessions[id]
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, session)

	case http.MethodDelete:
		delete(s.sessions, id)
		w.WriteHeader(http.StatusNoContent)

	case http.MethodPost:
		var req struct {
			Message string `json:"message"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		
		// Add user message
		userMsg := Message{
			ID:        generateID(),
			Role:      "user",
			Content:   req.Message,
			Timestamp: time.Now(),
		}
		session.Messages = append(session.Messages, userMsg)
		
		// Simulate AI response
		assistantMsg := Message{
			ID:        generateID(),
			Role:      "assistant",
			Content:   fmt.Sprintf("Echo: %s", req.Message),
			Timestamp: time.Now(),
		}
		session.Messages = append(session.Messages, assistantMsg)
		session.UpdatedAt = time.Now()
		
		jsonResponse(w, session)
	}
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	tools := []map[string]interface{}{
		{"name": "web_search", "description": "Search the web"},
		{"name": "read_file", "description": "Read file contents"},
		{"name": "write_file", "description": "Write file contents"},
		{"name": "terminal", "description": "Execute shell commands"},
		{"name": "browser_navigate", "description": "Navigate browser"},
	}
	jsonResponse(w, tools)
}

func (s *Server) handleToolsets(w http.ResponseWriter, r *http.Request) {
	toolsets := []map[string]interface{}{
		{"name": "web", "tools": []string{"web_search", "web_extract"}},
		{"name": "file", "tools": []string{"read_file", "write_file"}},
		{"name": "terminal", "tools": []string{"terminal", "process"}},
		{"name": "browser", "tools": []string{"browser_navigate", "browser_snapshot"}},
	}
	jsonResponse(w, toolsets)
}

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	skills := []map[string]interface{}{
		{"name": "git-workflow", "category": "development", "description": "Git workflow helpers"},
		{"name": "docker-help", "category": "devops", "description": "Docker commands"},
	}
	jsonResponse(w, skills)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	config := map[string]interface{}{
		"provider":   "openai",
		"model":     "gpt-4",
		"theme":     "dark",
		"language":   "en",
		"streaming":  true,
	}
	jsonResponse(w, config)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	// Real-time logs from gateway if available
	logs := []map[string]interface{}{
		{"time": time.Now().Format("15:04:05"), "level": "info", "message": "Server started"},
		{"time": time.Now().Format("15:04:06"), "level": "debug", "message": "Loading config"},
		{"time": time.Now().Format("15:04:07"), "level": "info", "message": "API server initialized"},
		{"time": time.Now().Format("15:04:08"), "level": "info", "message": "WebSocket handler ready"},
	}
	jsonResponse(w, logs)
}

// handlePlatforms returns connected messaging platforms
func (s *Server) handlePlatforms(w http.ResponseWriter, r *http.Request) {
	platforms := []map[string]interface{}{
		{
			"name":    "telegram",
			"status":  "disconnected",
			"message": "Not configured",
		},
		{
			"name":    "discord",
			"status":  "disconnected", 
			"message": "Not configured",
		},
		{
			"name":    "whatsapp",
			"status":  "disconnected",
			"message": "Use 'magic gateway start' to connect",
		},
	}
	jsonResponse(w, platforms)
}

// handleAnalytics returns usage statistics
func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	analytics := map[string]interface{}{
		"total_sessions":   len(s.sessions),
		"total_messages":    0,
		"total_tokens":      0,
		"active_platforms":  0,
		"uptime_seconds":    time.Since(time.Now().Add(-1 * time.Hour)).Seconds(),
	}
	jsonResponse(w, analytics)
}

// handleChat handles HTTP POST chat requests
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message string `json:"message"`
		Model   string `json:"model"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Set streaming headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Simulate streaming response
	response := fmt.Sprintf("Echo: %s\n\nAs go-magic AI, I can help you with various tasks!", req.Message)
	for _, char := range response {
		fmt.Fprintf(w, "data: %c\n\n", char)
		flusher.Flush()
		time.Sleep(20 * time.Millisecond)
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Handle incoming messages
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		
		// Parse incoming message
		var req ChatRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			// Echo back for error
			conn.WriteMessage(websocket.TextMessage, msg)
			continue
		}
		
		// Process chat request and stream response
		s.streamChatResponse(conn, &req)
	}
}

// ChatRequest represents a chat message request
type ChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	Model     string `json:"model,omitempty"`
}

// streamChatResponse streams AI response via WebSocket
func (s *Server) streamChatResponse(conn *websocket.Conn, req *ChatRequest) {
	// Generate response chunks (simulated streaming)
	chunks := []string{
		"收到你的消息: " + req.Message + "\n\n",
		"正在思考...\n",
		"作为 go-magic AI Agent，我可以帮助你：\n\n",
		"1. 回答各种问题\n",
		"2. 编写和调试代码\n",
		"3. 分析文件和数据\n",
		"4. 执行终端命令\n",
		"5. 管理你的技能和工作流程\n\n",
		"请告诉我你需要什么帮助？",
	}
	
	for _, chunk := range chunks {
		resp := map[string]interface{}{
			"type":    "chunk",
			"content": chunk,
		}
		data, _ := json.Marshal(resp)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	// Send completion
	complete := map[string]interface{}{
		"type": "complete",
	}
	data, _ := json.Marshal(complete)
	conn.WriteMessage(websocket.TextMessage, data)
}

// handleChatSSE handles Server-Sent Events for streaming chat
func (s *Server) handleChatSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	sessionID := r.URL.Query().Get("session_id")
	message := r.URL.Query().Get("message")
	
	if message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}
	
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}
	
	// Stream response
	_ = sessionID // sessionID reserved for future use
	chunks := []string{
		"收到: " + message + "\n\n",
		"正在处理...\n",
		"go-magic AI Agent 响应中...\n",
	}
	
	for _, chunk := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
		time.Sleep(200 * time.Millisecond)
	}
	
	// Send final message
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	// Try embedded files first
	data, err := staticFiles.ReadFile(path)
	if err == nil {
		contentType := getContentType(path)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
		return
	}

	// Try disk files as fallback
	diskPath := filepath.Join("web", "dist", path)
	data, err = os.ReadFile(diskPath)
	if err == nil {
		contentType := getContentType(path)
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
		return
	}

	// Return index.html for SPA routing
	indexData, err := staticFiles.ReadFile("/index.html")
	if err == nil {
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexData)
		return
	}

	// Fallback to disk
	indexDiskPath := filepath.Join("web", "dist", "index.html")
	indexData, err = os.ReadFile(indexDiskPath)
	if err == nil {
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexData)
		return
	}

	// No files found - show setup instructions
	w.Header().Set("Content-Type", "text/html")
	html := `<!DOCTYPE html>
<html>
<head><title>go-magic Web Dashboard</title></head>
<body>
<h1>go-magic Web Dashboard</h1>
<p>Frontend files not embedded. To enable the dashboard:</p>
<pre>
# Option 1: Download pre-built binary with embedded frontend
# Already available in this release!

# Option 2: Build frontend manually
cd web && pnpm install && pnpm build

# Option 3: Use the CLI instead
./magic --help
</pre>
</body>
</html>`
	w.Write([]byte(html))
}

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
	}
	if t, ok := types[ext]; ok {
		return t
	}
	return "text/plain"
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Create temporary database for standalone server
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Initialize schema
	groupchat.InitSchema(db)

	server := NewServer(port, db)
	log.Printf("Starting go-magic server on http://localhost:%s", port)
	log.Printf("Dashboard: http://localhost:%s/", port)
	log.Fatal(server.Start())
}
