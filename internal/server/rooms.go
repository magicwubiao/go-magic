package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/magicwubiao/go-magic/internal/bot"
)

// roomToResponse serializes a room config for the dashboard API.
func roomToResponse(r *bot.RoomConfig) map[string]interface{} {
	return map[string]interface{}{
		"id":           r.ID,
		"name":         r.Name,
		"topic":        r.Topic,
		"members":      r.Members,
		"max_rounds":   r.Rounds(),
		"max_messages": r.MessagesCap(),
		"created_at":   r.CreatedAt,
		"updated_at":   r.UpdatedAt,
	}
}

// handleRooms routes /api/rooms (list, create).
func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRoomsList(w, r)
	case http.MethodPost:
		s.handleRoomsCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRoomByID routes /api/rooms/{id}[/...].
func (s *Server) handleRoomByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/rooms/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "room id is required", http.StatusBadRequest)
		return
	}
	id := parts[0]
	if !validNamePattern.MatchString(id) {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}

	switch {
	case len(parts) == 1:
		switch r.Method {
		case http.MethodGet:
			s.handleRoomGet(w, r, id)
		case http.MethodPut, http.MethodPatch:
			s.handleRoomUpdate(w, r, id)
		case http.MethodDelete:
			s.handleRoomDelete(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 2 && parts[1] == "messages":
		if r.Method == http.MethodGet {
			s.handleRoomMessages(w, r, id)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 2 && parts[1] == "send":
		if r.Method == http.MethodPost {
			s.handleRoomSend(w, r, id)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// handleRoomsList GET /api/rooms
func (s *Server) handleRoomsList(w http.ResponseWriter, r *http.Request) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	rooms, err := mgr.ListRooms()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := make([]map[string]interface{}, 0, len(rooms))
	for _, room := range rooms {
		result = append(result, roomToResponse(room))
	}
	jsonResponse(w, result)
}

// handleRoomsCreate POST /api/rooms
func (s *Server) handleRoomsCreate(w http.ResponseWriter, r *http.Request) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	var req struct {
		Name        string   `json:"name"`
		Topic       string   `json:"topic"`
		Members     []string `json:"members"`
		MaxRounds   *int     `json:"max_rounds,omitempty"`
		MaxMessages *int     `json:"max_messages,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	cfg := &bot.RoomConfig{
		Name:    req.Name,
		Topic:   req.Topic,
		Members: req.Members,
	}
	if req.MaxRounds != nil {
		cfg.MaxRounds = *req.MaxRounds
	}
	if req.MaxMessages != nil {
		cfg.MaxMessages = *req.MaxMessages
	}
	if err := mgr.CreateRoom(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonResponse(w, roomToResponse(cfg))
}

// handleRoomGet GET /api/rooms/{id}
func (s *Server) handleRoomGet(w http.ResponseWriter, r *http.Request, id string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	room, err := mgr.GetRoom(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, roomToResponse(room))
}

// handleRoomUpdate PUT/PATCH /api/rooms/{id}
func (s *Server) handleRoomUpdate(w http.ResponseWriter, r *http.Request, id string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	var req struct {
		Name        *string  `json:"name,omitempty"`
		Topic       *string  `json:"topic,omitempty"`
		Members     []string `json:"members,omitempty"`
		MaxRounds   *int     `json:"max_rounds,omitempty"`
		MaxMessages *int     `json:"max_messages,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	room, err := mgr.UpdateRoom(id, func(c *bot.RoomConfig) {
		if req.Name != nil {
			c.Name = *req.Name
		}
		if req.Topic != nil {
			c.Topic = *req.Topic
		}
		if req.Members != nil {
			c.Members = req.Members
		}
		if req.MaxRounds != nil {
			c.MaxRounds = *req.MaxRounds
		}
		if req.MaxMessages != nil {
			c.MaxMessages = *req.MaxMessages
		}
	})
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	jsonResponse(w, roomToResponse(room))
}

// handleRoomDelete DELETE /api/rooms/{id}
func (s *Server) handleRoomDelete(w http.ResponseWriter, r *http.Request, id string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	if err := mgr.DeleteRoom(id); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	jsonResponse(w, map[string]interface{}{"deleted": id})
}

// handleRoomMessages GET /api/rooms/{id}/messages
func (s *Server) handleRoomMessages(w http.ResponseWriter, r *http.Request, id string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	msgs, err := mgr.RoomMessages(id)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	result := make([]map[string]interface{}, 0, len(msgs))
	for _, msg := range msgs {
		result = append(result, map[string]interface{}{
			"id":        msg.ID,
			"from":      msg.From,
			"content":   msg.Content,
			"timestamp": msg.Timestamp * 1000,
		})
	}
	jsonResponse(w, result)
}

// handleRoomSend POST /api/rooms/{id}/send — deliver a user message to the
// room and block until the coordinated multi-bot round completes.
func (s *Server) handleRoomSend(w http.ResponseWriter, r *http.Request, id string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	var req struct {
		Message string `json:"message"`
		Target  string `json:"target,omitempty"` // optional @bot to address first
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	res, err := mgr.SendToRoom(r.Context(), id, req.Message, req.Target)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		if strings.Contains(err.Error(), "unknown bot") {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	result := make([]map[string]interface{}, 0, len(res.Messages))
	for _, msg := range res.Messages {
		result = append(result, map[string]interface{}{
			"id":        msg.ID,
			"from":      msg.From,
			"content":   msg.Content,
			"timestamp": msg.Timestamp * 1000,
		})
	}
	jsonResponse(w, map[string]interface{}{
		"room_id":    res.RoomID,
		"needs_user": res.NeedsUser,
		"messages":   result,
	})
}
