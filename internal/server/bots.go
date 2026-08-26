package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/magicwubiao/go-magic/internal/bot"
)

// botToResponse serializes a bot config for the dashboard API.
func botToResponse(cfg *bot.Config, state *bot.RuntimeState) map[string]interface{} {
	resp := map[string]interface{}{
		"name":          cfg.Name,
		"mention_tag":   cfg.MentionTag(),
		"title":         cfg.Title,
		"description":   cfg.Description,
		"system_prompt": cfg.SystemPrompt,
		"model":         cfg.Model,
		"provider":      cfg.Provider,
		"created_at":    cfg.CreatedAt,
		"updated_at":    cfg.UpdatedAt,
	}
	if state != nil {
		resp["runtime"] = map[string]interface{}{
			"online":          true,
			"session_id":      state.SessionID,
			"queue_depth":     state.QueueDepth,
			"history_length":  state.HistoryLength,
			"active_routines": state.ActiveRoutines,
		}
	} else {
		resp["runtime"] = map[string]interface{}{
			"online": false,
		}
	}
	return resp
}

// routineToResponse serializes a routine config.
func routineToResponse(r *bot.RoutineConfig) map[string]interface{} {
	resp := map[string]interface{}{
		"id":          r.ID,
		"name":        r.Name,
		"schedule":    r.Schedule,
		"prompt":      r.Prompt,
		"enabled":     r.Enabled,
		"last_status": r.LastStatus,
		"created_at":  r.CreatedAt,
	}
	if r.LastRun != nil {
		resp["last_run"] = *r.LastRun
	}
	return resp
}

// handleBots routes /api/bots (list, create) requests.
func (s *Server) handleBots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleBotsList(w, r)
	case http.MethodPost:
		s.handleBotsCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBotByID routes /api/bots/{name}[/...] requests.
func (s *Server) handleBotByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/bots/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	name := ""
	if len(parts) > 0 {
		name = parts[0]
	}
	if name == "" {
		http.Error(w, "bot name is required", http.StatusBadRequest)
		return
	}

	switch {
	case len(parts) == 1:
		switch r.Method {
		case http.MethodGet:
			s.handleBotGet(w, r, name)
		case http.MethodPut, http.MethodPatch:
			s.handleBotUpdate(w, r, name)
		case http.MethodDelete:
			s.handleBotDelete(w, r, name)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 2 && parts[1] == "routines":
		s.handleBotRoutines(w, r, name)
	case len(parts) == 3 && parts[1] == "routines":
		s.handleBotRoutineByID(w, r, name, parts[2])
	case len(parts) == 2 && parts[1] == "chat":
		s.handleBotChat(w, r, name)
	case len(parts) == 3 && parts[1] == "chat" && parts[2] == "stream":
		s.handleBotChatStream(w, r, name)
	case len(parts) == 2 && parts[1] == "messages":
		s.handleBotMessages(w, r, name)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// requireBotManager resolves the shared bot manager or fails the request.
func (s *Server) requireBotManager(w http.ResponseWriter) *bot.Manager {
	mgr := s.botMgr()
	if mgr == nil {
		http.Error(w, "bot mode is not running", http.StatusServiceUnavailable)
		return nil
	}
	return mgr
}

// handleBotsList GET /api/bots
func (s *Server) handleBotsList(w http.ResponseWriter, r *http.Request) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	configs := mgr.List()
	result := make([]map[string]interface{}, 0, len(configs))
	for _, cfg := range configs {
		state := mgr.RuntimeStatus(cfg.Name)
		result = append(result, botToResponse(cfg, &state))
	}
	jsonResponse(w, result)
}

// handleBotsCreate POST /api/bots
func (s *Server) handleBotsCreate(w http.ResponseWriter, r *http.Request) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	var req struct {
		Name         string `json:"name"`
		Title        string `json:"title"`
		Description  string `json:"description"`
		SystemPrompt string `json:"system_prompt"`
		Model        string `json:"model"`
		Provider     string `json:"provider"`
		Start        *bool  `json:"start,omitempty"` // reserved; bots are online immediately when mode is on
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cfg := &bot.Config{
		Name:         req.Name,
		Title:        req.Title,
		Description:  req.Description,
		SystemPrompt: req.SystemPrompt,
		Model:        req.Model,
		Provider:     req.Provider,
	}
	if err := mgr.CreateBot(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	state := mgr.RuntimeStatus(cfg.Name)
	w.WriteHeader(http.StatusCreated)
	jsonResponse(w, botToResponse(cfg, &state))
}

// handleBotGet GET /api/bots/{name}
func (s *Server) handleBotGet(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	cfg, err := mgr.GetBot(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	state := mgr.RuntimeStatus(name)
	jsonResponse(w, botToResponse(cfg, &state))
}

// handleBotUpdate PUT/PATCH /api/bots/{name}
func (s *Server) handleBotUpdate(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	var req struct {
		Title        *string `json:"title,omitempty"`
		Description  *string `json:"description,omitempty"`
		SystemPrompt *string `json:"system_prompt,omitempty"`
		Model        *string `json:"model,omitempty"`
		Provider     *string `json:"provider,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cfg, err := mgr.UpdateBot(name, func(c *bot.Config) {
		if req.Title != nil {
			c.Title = *req.Title
		}
		if req.Description != nil {
			c.Description = *req.Description
		}
		if req.SystemPrompt != nil {
			c.SystemPrompt = *req.SystemPrompt
		}
		if req.Model != nil {
			c.Model = *req.Model
		}
		if req.Provider != nil {
			c.Provider = *req.Provider
		}
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	state := mgr.RuntimeStatus(name)
	jsonResponse(w, botToResponse(cfg, &state))
}

// handleBotDelete DELETE /api/bots/{name}
func (s *Server) handleBotDelete(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	if err := mgr.DeleteBot(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonResponse(w, map[string]interface{}{"deleted": name})
}

// handleBotRoutines GET/POST /api/bots/{name}/routines
func (s *Server) handleBotRoutines(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	switch r.Method {
	case http.MethodGet:
		routines, err := mgr.ListRoutines(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		result := make([]map[string]interface{}, 0, len(routines))
		for _, rt := range routines {
			result = append(result, routineToResponse(rt))
		}
		jsonResponse(w, result)
	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
			Prompt   string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		rt := &bot.RoutineConfig{
			Name:     req.Name,
			Schedule: req.Schedule,
			Prompt:   req.Prompt,
		}
		if rt.ID == "" || rt.ID == "auto" {
			rt.ID = fmt.Sprintf("web_%s", uuid.New().String()[:8])
		}
		if err := mgr.AddRoutine(name, rt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, routineToResponse(rt))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBotRoutineByID routes /api/bots/{name}/routines/{id}: DELETE removes,
// PATCH applies partial updates (schedule/prompt/enabled/name).
func (s *Server) handleBotRoutineByID(w http.ResponseWriter, r *http.Request, name, idOrName string) {
	switch r.Method {
	case http.MethodDelete:
		mgr := s.requireBotManager(w)
		if mgr == nil {
			return
		}
		if err := mgr.RemoveRoutine(name, idOrName); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		jsonResponse(w, map[string]interface{}{"deleted": idOrName})
	case http.MethodPatch, http.MethodPut:
		mgr := s.requireBotManager(w)
		if mgr == nil {
			return
		}

		var req struct {
			Name     *string `json:"name,omitempty"`
			Schedule *string `json:"schedule,omitempty"`
			Prompt   *string `json:"prompt,omitempty"`
			Enabled  *bool   `json:"enabled,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == nil && req.Schedule == nil && req.Prompt == nil && req.Enabled == nil {
			http.Error(w, "no fields to update", http.StatusBadRequest)
			return
		}

		rt, err := mgr.UpdateRoutine(name, idOrName, func(r *bot.RoutineConfig) {
			if req.Name != nil {
				r.Name = *req.Name
			}
			if req.Schedule != nil {
				r.Schedule = strings.TrimSpace(*req.Schedule)
			}
			if req.Prompt != nil {
				r.Prompt = *req.Prompt
			}
			if req.Enabled != nil {
				r.Enabled = *req.Enabled
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
		jsonResponse(w, routineToResponse(rt))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBotChat POST /api/bots/{name}/chat — synchronous send-and-wait turn.
func (s *Server) handleBotChat(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	reply, err := mgr.SendToBot(name, req.Message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"id":        uuid.New().String(),
		"role":      "assistant",
		"content":   reply,
		"timestamp": time.Now().UnixMilli(),
	})
}

// handleBotMessages GET /api/bots/{name}/messages — canonical chat history.
func (s *Server) handleBotMessages(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), readTimeout10s())
	defer cancel()

	sess, err := mgr.Sessions().LoadSession(ctx, bot.CanonicalSessionID(strings.ToLower(name)))
	if err != nil || sess == nil {
		jsonResponse(w, []interface{}{})
		return
	}
	type chatMsg struct {
		id        string
		role      string
		from      string
		content   string
		timestamp int64
	}

	var merged []chatMsg
	var pendingAssistants []chatMsg

	flushPending := func() {
		if len(pendingAssistants) == 0 {
			return
		}
		if len(pendingAssistants) == 1 {
			merged = append(merged, pendingAssistants[0])
		} else {
			var sb strings.Builder
			var lastTS int64
			for _, pa := range pendingAssistants {
				if sb.Len() > 0 {
					sb.WriteString("\n\n")
				}
				sb.WriteString(pa.content)
				if pa.timestamp > lastTS {
					lastTS = pa.timestamp
				}
			}
			merged = append(merged, chatMsg{
				id:        pendingAssistants[0].id,
				role:      "assistant",
				from:      pendingAssistants[0].from,
				content:   sb.String(),
				timestamp: lastTS,
			})
		}
		pendingAssistants = nil
	}

	for i, msg := range sess.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		ts := int64(0)
		if !msg.Timestamp.IsZero() {
			ts = msg.Timestamp.UnixMilli()
		} else if len(sess.Messages) > 1 {
			ratio := float64(i) / float64(len(sess.Messages)-1)
			startMs := sess.CreatedAt.UnixMilli()
			endMs := sess.UpdatedAt.UnixMilli()
			if endMs <= startMs {
				endMs = startMs + int64(len(sess.Messages)-1)*1000
			}
			ts = startMs + int64(float64(endMs-startMs)*ratio)
		} else {
			ts = sess.UpdatedAt.UnixMilli()
		}
		entry := chatMsg{
			id:        fmt.Sprintf("%s-%d", sess.ID, i),
			role:      msg.Role,
			from:      msg.From,
			content:   msg.Content,
			timestamp: ts,
		}
		if msg.Role == "user" {
			flushPending()
			merged = append(merged, entry)
		} else {
			pendingAssistants = append(pendingAssistants, entry)
		}
	}
	flushPending()

	result := make([]map[string]interface{}, 0, len(merged))
	for _, m := range merged {
		result = append(result, map[string]interface{}{
			"id":        m.id,
			"role":      m.role,
			"from":      m.from,
			"content":   m.content,
			"timestamp": m.timestamp,
		})
	}
	jsonResponse(w, result)
}

// handleBotChatStream POST /api/bots/{name}/chat/stream — SSE variant of
// handleBotChat. The turn still runs serialized on the bot's queue; assistant
// deltas are pushed as {"delta": "..."} events, ending with {"done": true}.
func (s *Server) handleBotChatStream(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	writeSSE := func(payload string) {
		fmt.Fprint(w, "data: "+payload+"\n\n")
		flusher.Flush()
	}
	writeJSONEvent := func(v interface{}) {
		if data, err := json.Marshal(v); err == nil {
			writeSSE(string(data))
		}
	}

	// Headers out immediately so proxies/clients see a live stream.
	writeSSE(`{"type":"connected"}`)

	reply, err := mgr.SendToBotStream(name, req.Message, func(content string, done bool) {
		if done || content == "" {
			return
		}
		writeJSONEvent(map[string]string{"delta": content})
	})

	if err != nil {
		msg := err.Error()
		writeJSONEvent(map[string]string{"error": msg})
	} else if strings.TrimSpace(reply) != "" {
		// Ensure the client has the full final text even if some deltas were
		// coalesced by intermediate proxies.
		writeJSONEvent(map[string]interface{}{"final": reply})
	}
	writeSSE(`{"done":true}`)
}
