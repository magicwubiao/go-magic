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
	schedules := make([]map[string]interface{}, 0, len(r.Schedules))
	for _, sc := range r.Schedules {
		schedules = append(schedules, roomScheduleToWire(sc))
	}
	return map[string]interface{}{
		"id":           r.ID,
		"name":         r.Name,
		"topic":        r.Topic,
		"members":      r.Members,
		"max_rounds":   r.Rounds(),
		"max_messages": r.MessagesCap(),
		"history_cap":  r.History(),
		"host":         r.Host,
		"auto_route":   r.AutoRoutes(),
		"schedules":    schedules,
		"created_at":   r.CreatedAt,
		"updated_at":   r.UpdatedAt,
	}
}

// roomScheduleToWire serializes one scheduled room push.
func roomScheduleToWire(sc bot.RoomSchedule) map[string]interface{} {
	out := map[string]interface{}{
		"id":       sc.ID,
		"name":     sc.Name,
		"schedule": sc.Schedule,
		"prompt":   sc.Prompt,
		"target":   sc.Target,
		"enabled":  !sc.Disabled,
	}
	if sc.LastRun != nil {
		// Unix seconds -> milliseconds, matching the rest of the dashboard API.
		out["last_run"] = *sc.LastRun * 1000
	}
	if sc.LastStatus != "" {
		out["last_status"] = sc.LastStatus
	}
	if sc.LastError != "" {
		out["last_error"] = sc.LastError
	}
	if sc.CreatedAt > 0 {
		out["created_at"] = sc.CreatedAt * 1000
	}
	return out
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
	case len(parts) == 2 && parts[1] == "status":
		// 群聊回合可能跑几分钟（成员逐个发言，各自还有回合上限），阻塞的
		// POST 不适合当进度信号：前端靠这个端点判断还要不要继续轮询房间
		// 日志，从而在回合进行中就能看到成员回复。
		if r.Method == http.MethodGet {
			s.handleRoomStatus(w, r, id)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 2 && parts[1] == "stop":
		// 停止进行中的一轮：跳过剩余成员/轮次、取消正在发言的成员回合。
		// 此前唯一的出路是删房间，显然不是"停止"。
		if r.Method == http.MethodPost {
			s.handleRoomStop(w, r, id)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 2 && parts[1] == "send":
		if r.Method == http.MethodPost {
			s.handleRoomSend(w, r, id)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 2 && parts[1] == "notes":
		// 群共享笔记：所有人（成员 bot 与人类）读同一份，不随时间线裁剪。
		switch r.Method {
		case http.MethodGet, http.MethodPut:
			s.handleRoomNotes(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 2 && parts[1] == "schedules":
		switch r.Method {
		case http.MethodGet, http.MethodPost:
			s.handleRoomSchedules(w, r, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 3 && parts[1] == "schedules":
		// Schedule ids are generated (rs_xxxxxxxx), but the manager also
		// resolves by human name, so the segment is only checked for path
		// safety rather than against the bot-name pattern.
		if strings.ContainsAny(parts[2], `/\`) || parts[2] == "." || parts[2] == ".." {
			http.Error(w, "invalid schedule id", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPatch, http.MethodPut:
			s.handleRoomScheduleUpdate(w, r, id, parts[2])
		case http.MethodDelete:
			s.handleRoomScheduleDelete(w, r, id, parts[2])
		default:
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
		Host        string   `json:"host,omitempty"`
		AutoRoute   *bool    `json:"auto_route,omitempty"`
		MaxRounds   *int     `json:"max_rounds,omitempty"`
		MaxMessages *int     `json:"max_messages,omitempty"`
		HistoryCap  *int     `json:"history_cap,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	cfg := &bot.RoomConfig{
		Name:      req.Name,
		Topic:     req.Topic,
		Members:   req.Members,
		Host:      req.Host,
		AutoRoute: req.AutoRoute,
	}
	if req.MaxRounds != nil {
		cfg.MaxRounds = *req.MaxRounds
	}
	if req.MaxMessages != nil {
		cfg.MaxMessages = *req.MaxMessages
	}
	if req.HistoryCap != nil {
		cfg.HistoryCap = *req.HistoryCap
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
		Host        *string  `json:"host,omitempty"`
		AutoRoute   *bool    `json:"auto_route,omitempty"`
		MaxRounds   *int     `json:"max_rounds,omitempty"`
		MaxMessages *int     `json:"max_messages,omitempty"`
		HistoryCap  *int     `json:"history_cap,omitempty"`
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
		if req.Host != nil {
			c.Host = *req.Host
		}
		if req.AutoRoute != nil {
			c.AutoRoute = req.AutoRoute
		}
		if req.MaxRounds != nil {
			c.MaxRounds = *req.MaxRounds
		}
		if req.MaxMessages != nil {
			c.MaxMessages = *req.MaxMessages
		}
		if req.HistoryCap != nil {
			c.HistoryCap = *req.HistoryCap
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

// handleRoomStatus GET /api/rooms/{id}/status — reports whether a coordinated
// round is currently in flight for this room. The blocking /send call is the
// wrong progress signal for the dashboard: a round legitimately takes minutes
// (members speak one after another, each with its own turn budget), so the UI
// polls the room log while this says running.
func (s *Server) handleRoomStatus(w http.ResponseWriter, r *http.Request, id string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	jsonResponse(w, map[string]interface{}{"running": mgr.RoomRoundRunning(id)})
}

// handleRoomStop POST /api/rooms/{id}/stop — abort the room's in-flight
// round. A coordinated round legitimately runs for minutes (members speak one
// after another, each with its own turn budget), and until now the only way
// out was deleting the whole room. Returns {"stopped": true} when a round was
// actually aborted, false when the room was already idle.
func (s *Server) handleRoomStop(w http.ResponseWriter, r *http.Request, id string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	jsonResponse(w, map[string]interface{}{"stopped": mgr.StopRoomRound(id)})
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

// handleRoomNotes GET/PUT /api/rooms/{id}/notes — the room's shared notes.
//
// Notes are a small markdown file every member reads on every turn; unlike the
// message log they are neither capped by HistoryCap nor trimmed, so a decision
// made weeks ago still reaches the room. PUT replaces the whole document
// (the editor is a plain textarea) and is capped server-side.
func (s *Server) handleRoomNotes(w http.ResponseWriter, r *http.Request, id string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		notes, err := mgr.RoomNotes(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		jsonResponse(w, map[string]interface{}{
			"content": notes,
			"limit":   bot.MaxRoomNotesBytes,
		})
	case http.MethodPut:
		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, bot.MaxRoomNotesBytes*2)).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		notes, err := mgr.SaveRoomNotes(id, req.Content)
		if err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "not found") {
				status = http.StatusNotFound
			}
			if strings.Contains(err.Error(), "exceed") {
				status = http.StatusBadRequest
			}
			http.Error(w, err.Error(), status)
			return
		}
		jsonResponse(w, map[string]interface{}{"content": notes, "limit": bot.MaxRoomNotesBytes})
	}
}

// handleRoomSchedules GET/POST /api/rooms/{id}/schedules — recurring pushes.
func (s *Server) handleRoomSchedules(w http.ResponseWriter, r *http.Request, id string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		room, err := mgr.GetRoom(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		out := make([]map[string]interface{}, 0, len(room.Schedules))
		for _, sc := range room.Schedules {
			out = append(out, roomScheduleToWire(sc))
		}
		jsonResponse(w, out)
	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
			Prompt   string `json:"prompt"`
			Target   string `json:"target"`
			Enabled  *bool  `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		sc := &bot.RoomSchedule{
			Name:     strings.TrimSpace(req.Name),
			Schedule: req.Schedule,
			Prompt:   req.Prompt,
			Target:   req.Target,
		}
		if req.Enabled != nil && !*req.Enabled {
			sc.Disabled = true
		}
		room, err := mgr.AddRoomSchedule(id, sc)
		if err != nil {
			http.Error(w, err.Error(), roomErrorStatus(err))
			return
		}
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, roomToResponse(room))
	}
}

// handleRoomScheduleUpdate PATCH/PUT /api/rooms/{id}/schedules/{sid}.
func (s *Server) handleRoomScheduleUpdate(w http.ResponseWriter, r *http.Request, id, sid string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	var req struct {
		Name     *string `json:"name"`
		Schedule *string `json:"schedule"`
		Prompt   *string `json:"prompt"`
		Target   *string `json:"target"`
		Enabled  *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	room, err := mgr.UpdateRoomSchedule(id, sid, func(sc *bot.RoomSchedule) {
		if req.Name != nil {
			sc.Name = strings.TrimSpace(*req.Name)
		}
		if req.Schedule != nil {
			sc.Schedule = *req.Schedule
		}
		if req.Prompt != nil {
			sc.Prompt = *req.Prompt
		}
		if req.Target != nil {
			sc.Target = *req.Target
		}
		if req.Enabled != nil {
			sc.Disabled = !*req.Enabled
		}
	})
	if err != nil {
		http.Error(w, err.Error(), roomErrorStatus(err))
		return
	}
	jsonResponse(w, roomToResponse(room))
}

// handleRoomScheduleDelete DELETE /api/rooms/{id}/schedules/{sid}.
func (s *Server) handleRoomScheduleDelete(w http.ResponseWriter, r *http.Request, id, sid string) {
	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}
	room, err := mgr.RemoveRoomSchedule(id, sid)
	if err != nil {
		http.Error(w, err.Error(), roomErrorStatus(err))
		return
	}
	jsonResponse(w, roomToResponse(room))
}

// roomErrorStatus maps room/schedule errors to HTTP status codes without
// string-matching at every call site.
func roomErrorStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found"):
		return http.StatusNotFound
	case strings.Contains(msg, "invalid schedule"),
		strings.Contains(msg, "is required"),
		strings.Contains(msg, "is not a member"),
		strings.Contains(msg, "exceed"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
