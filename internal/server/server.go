package server

import (
	"context"
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
	port        string
	sessions    map[string]*Session
	sessionsMux sync.RWMutex
	mux         *http.ServeMux
	httpServer  *http.Server
}

func NewServer(port string) *Server {
	return &Server{
		port:     port,
		sessions: make(map[string]*Session),
	}
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
	
	// WebSocket for streaming
	s.mux.HandleFunc("/ws", s.handleWebSocket)
	
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
	logs := []map[string]interface{}{
		{"time": time.Now().Format("15:04:05"), "level": "info", "message": "Server started"},
		{"time": time.Now().Format("15:04:06"), "level": "debug", "message": "Loading config"},
	}
	jsonResponse(w, logs)
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
		
		// Echo back (in production, this would call the AI)
		conn.WriteMessage(websocket.TextMessage, msg)
	}
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	// Try disk files first
	diskPath := filepath.Join("web", "dist", path)
	data, err := os.ReadFile(diskPath)
	if err == nil {
		contentType := getContentType(path)
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
		return
	}

	// Return index.html for SPA routing
	indexPath := filepath.Join("web", "dist", "index.html")
	indexData, err := os.ReadFile(indexPath)
	if err == nil {
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexData)
		return
	}

	// No files found
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("go-magic Web Dashboard\n\nPlease run: make web-build\n"))
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

	server := NewServer(port)
	log.Printf("Starting go-magic server on http://localhost:%s", port)
	log.Printf("Dashboard: http://localhost:%s/", port)
	log.Fatal(server.Start())
}
