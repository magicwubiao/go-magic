package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/magicwubiao/go-magic/internal/bot"
	"github.com/magicwubiao/go-magic/pkg/types"
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
	// Reclaim the room's uploads bucket (mirrors bot deletion).
	s.cleanupSessionUploads(roomUploadBucket(id))
	jsonResponse(w, map[string]interface{}{"deleted": id})
}

// roomUploadBucket is the uploads bucket for a room's shared attachments.
// The "room_" prefix (plus the id itself, which also starts with "room_" for
// bot.NewRoomID ids) makes the GC recognize these buckets unambiguously; the
// sanitized lowercase id keeps the directory filesystem-safe.
func roomUploadBucket(roomID string) string {
	return "room_" + fileNameSafeRe.ReplaceAllString(strings.ToLower(roomID), "_")
}

// roomMessageToWire serializes a room log entry for the dashboard API,
// including attachment refs when present.
func roomMessageToWire(msg bot.RoomMessage) map[string]interface{} {
	out := map[string]interface{}{
		"id":        msg.ID,
		"from":      msg.From,
		"content":   msg.Content,
		"timestamp": msg.Timestamp * 1000,
	}
	if len(msg.Attachments) > 0 {
		atts := make([]map[string]interface{}, 0, len(msg.Attachments))
		for _, a := range msg.Attachments {
			atts = append(atts, map[string]interface{}{
				"name": a.Name,
				"url":  a.URL,
				"mime": a.Mime,
			})
		}
		out["attachments"] = atts
	}
	return out
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
		result = append(result, roomMessageToWire(msg))
	}
	jsonResponse(w, result)
}

// parseRoomSendPayload parses the room send body through the shared
// parseChatPayload (same attachment contract as bot chat / sessions):
// inline images and upload refs are stripped into persisted ref parts, and
// the canonical uploads are returned so each member bot gets a private copy
// materialized into its workdir. Back-compat: the original
// {"message","target"} body keeps working.
func (s *Server) parseRoomSendPayload(r *http.Request, roomID string) (text, target string, persisted []types.ContentPart, items []bot.RoomUploadItem, errMsg string) {
	const maxRoomSendBodyBytes = 16 << 20
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxRoomSendBodyBytes))
	if err != nil {
		return "", "", nil, nil, "failed to read request body: " + err.Error()
	}

	var legacy struct {
		Message string `json:"message"`
		Target  string `json:"target"`
	}
	_ = json.Unmarshal(raw, &legacy)
	target = strings.TrimSpace(legacy.Target)

	// parseChatPayload re-reads the body; hand the saved bytes back.
	r.Body = io.NopCloser(bytes.NewReader(raw))

	parsed, perr := s.parseChatPayload(r, roomUploadBucket(roomID))
	if parsed == nil {
		return "", "", nil, nil, perr
	}

	text = strings.TrimSpace(parsed.content)
	if text == "" {
		text = strings.TrimSpace(legacy.Message)
	}
	if text == "" && len(parsed.contentParts) == 0 {
		return "", "", nil, nil, "message is required"
	}

	persisted = persistedContentParts(parsed.contentParts, parsed.imageURLRefs, parsed.imageNames, s.uploadDisplayName)
	for _, it := range parsed.pendingMaterialize {
		items = append(items, bot.RoomUploadItem{Name: it.Name, Src: it.Src})
	}
	return text, target, persisted, items, ""
}

// handleRoomSend POST /api/rooms/{id}/send — deliver a user message to the
// room and block until the coordinated multi-bot round completes.
func (s *Server) handleRoomSend(w http.ResponseWriter, r *http.Request, id string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	text, target, persisted, items, errMsg := s.parseRoomSendPayload(r, id)
	if errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	res, err := mgr.SendToRoomWithMedia(r.Context(), id, text, persisted, items, target)
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
		result = append(result, roomMessageToWire(msg))
	}
	jsonResponse(w, map[string]interface{}{
		"room_id":    res.RoomID,
		"needs_user": res.NeedsUser,
		"messages":   result,
	})
}
