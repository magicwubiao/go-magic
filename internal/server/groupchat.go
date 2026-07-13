package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/groupchat"
	"github.com/magicwubiao/go-magic/internal/provider"
)

func (s *Server) handleGroupchatStream(w http.ResponseWriter, r *http.Request, roomID string) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	if s.provider == nil {
		http.Error(w, "provider not configured", 400)
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	// Find mentioned agents
	agents, err := s.groupchatStorage.GetAgents(roomID)
	if err != nil || len(agents) == 0 {
		http.Error(w, "no agents in room", 400)
		return
	}

	mentioned := make([]*groupchat.RoomAgent, 0)
	for i := range agents {
		mention := "@" + agents[i].Name
		if strings.Contains(req.Content, mention) {
			mentioned = append(mentioned, &agents[i])
		}
	}

	if len(mentioned) == 0 {
		http.Error(w, "no agent mentioned", 400)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Disable server-wide WriteTimeout for SSE streams (long-running connections)
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	sseW := newSSEWriter(w, flusher)
	writeSSE := sseW.Write

	// Flush headers immediately so the client/proxy knows the stream is alive.
	writeSSE("data: {\"type\":\"connected\"}\n\n")

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	defer sseW.Close()

	// Get recent messages for context
	recentMsgs, _ := s.groupchatStorage.GetMessages(roomID, 20)

	// Start heartbeat to prevent proxy/browser timeout
	heartbeatDone := make(chan struct{})
	safeGo(func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !writeSSE("data: {\"type\":\"ping\"}\n\n") {
					cancel()
					return
				}
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			}
		}
	})

	// Stream each mentioned agent's reply
	for _, agent := range mentioned {
		select {
		case <-ctx.Done():
			close(heartbeatDone)
			return
		default:
		}

		// Send agent start event
		startData, _ := json.Marshal(map[string]interface{}{
			"type":    "start",
			"agent":   agent.Name,
			"agentId": agent.ID,
		})
		writeSSE("data: " + string(startData) + "\n\n")

		// Build messages
		messages := make([]provider.Message, 0)
		systemPrompt := agent.SystemPrompt
		if systemPrompt == "" {
			systemPrompt = fmt.Sprintf("You are %s, an AI assistant in a group chat. Your profile is: %s. Description: %s. Reply concisely and helpfully.",
				agent.Name, agent.Profile, agent.Description)
		}
		messages = append(messages, provider.Message{Role: "system", Content: systemPrompt})

		for _, msg := range recentMsgs {
			role := "user"
			if msg.Type == "agent" {
				role = "assistant"
			}
			if msg.SenderID == "system" {
				role = "system"
			}
			messages = append(messages, provider.Message{Role: role, Content: msg.Content})
		}

		// Try streaming first
		agentCtx, agentCancel := context.WithTimeout(ctx, 10*time.Minute)
		fullContent := ""

		streamer, supportsStream := s.provider.(provider.Streamer)
		if supportsStream {
			streamErr := streamer.Stream(agentCtx, messages, func(resp *provider.StreamResponse) {
				if resp.Content != "" {
					select {
					case <-ctx.Done():
						return
					default:
					}
					fullContent += resp.Content
					data, _ := json.Marshal(map[string]interface{}{
						"type":    "content",
						"agent":   agent.Name,
						"agentId": agent.ID,
						"content": resp.Content,
					})
					writeSSE("data: " + string(data) + "\n\n")
				}
			})
			agentCancel()
			if streamErr != nil {
				errData, _ := json.Marshal(map[string]interface{}{
					"type":  "error",
					"agent": agent.Name,
					"error": streamErr.Error(),
				})
				writeSSE("data: " + string(errData) + "\n\n")
				continue
			}
		} else {
			// Fallback to non-streaming
			resp, chatErr := s.provider.Chat(agentCtx, messages)
			agentCancel()
			if chatErr != nil {
				errData, _ := json.Marshal(map[string]interface{}{
					"type":  "error",
					"agent": agent.Name,
					"error": chatErr.Error(),
				})
				writeSSE("data: " + string(errData) + "\n\n")
				continue
			}
			fullContent = resp.Content
			// Send as pseudo-stream
			chars := []rune(resp.Content)
			for i, ch := range chars {
				select {
				case <-ctx.Done():
					close(heartbeatDone)
					return
				default:
				}
				data, _ := json.Marshal(map[string]interface{}{
					"type":    "content",
					"agent":   agent.Name,
					"agentId": agent.ID,
					"content": string(ch),
				})
				writeSSE("data: " + string(data) + "\n\n")
				// Small delay for pseudo-streaming
				if i%10 == 0 {
					time.Sleep(5 * time.Millisecond)
				}
			}
		}

		// Save complete message to database
		replyMsg := &groupchat.ChatMessage{
			ID:         uuid.New().String(),
			RoomID:     roomID,
			SenderID:   agent.ID,
			SenderName: agent.Name,
			Content:    fullContent,
			Timestamp:  time.Now().UnixMilli(),
			Type:       "agent",
		}
		s.groupchatStorage.SaveMessage(replyMsg)

		// Send done event
		doneData, _ := json.Marshal(map[string]interface{}{
			"type":      "done",
			"agent":     agent.Name,
			"agentId":   agent.ID,
			"messageId": replyMsg.ID,
		})
		writeSSE("data: " + string(doneData) + "\n\n")
	}

	// Stop heartbeat
	close(heartbeatDone)

	// Send final done
	writeSSE("data: [DONE]\n\n")
}

func (s *Server) handleGroupchatRoomInvite(w http.ResponseWriter, r *http.Request, roomID string) {

	if s.groupchatStorage == nil {
		http.Error(w, "groupchat not initialized", 500)
		return
	}

	if r.Method == "POST" {
		room, err := s.groupchatStorage.GetRoom(roomID)
		if err != nil {
			http.Error(w, "room not found", 404)
			return
		}
		if room.InviteCode == "" {
			room.InviteCode = uuid.New().String()[:8]
			room.UpdatedAt = time.Now().UnixMilli()
			if err := s.groupchatStorage.UpdateRoom(room); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		jsonResponse(w, map[string]string{
			"invite_code": room.InviteCode,
		})
		return
	}

	http.Error(w, "method not allowed", 405)
}

func (s *Server) handleAgentMentions(roomID, content string) {
	if s.provider == nil || s.groupchatStorage == nil {
		return
	}

	agents, err := s.groupchatStorage.GetAgents(roomID)
	if err != nil || len(agents) == 0 {
		return
	}

	// Find mentioned agents (e.g., "@AgentName" or "@agent_name")
	mentioned := make(map[string]*groupchat.RoomAgent)
	for _, a := range agents {
		// Match @Name or @name (case insensitive)
		mention := "@" + a.Name
		if strings.Contains(content, mention) {
			mentioned[a.ID] = &a
		}
	}

	if len(mentioned) == 0 {
		return
	}

	// Get recent messages for context
	recentMsgs, _ := s.groupchatStorage.GetMessages(roomID, 20)

	for _, agent := range mentioned {
		s.replyAsAgent(roomID, agent, recentMsgs, content)
	}
}

func (s *Server) handleGroupchatRoomMembers(w http.ResponseWriter, r *http.Request, roomID string) {

	if s.groupchatStorage == nil {
		http.Error(w, "groupchat not initialized", 500)
		return
	}

	if r.Method == "GET" {
		members, _ := s.groupchatStorage.GetMembers(roomID)
		result := make([]interface{}, 0)
		for _, m := range members {
			result = append(result, map[string]interface{}{
				"id":        m.ID,
				"user_id":   m.UserID,
				"name":      m.Name,
				"online":    m.Online,
				"joined_at": m.JoinedAt,
				"last_seen": m.LastSeenAt,
			})
		}
		jsonResponse(w, result)
		return
	}

	if r.Method == "POST" {
		var req struct {
			UserID string `json:"user_id"`
			Name   string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		member := &groupchat.Member{
			ID:       uuid.New().String(),
			RoomID:   roomID,
			UserID:   req.UserID,
			Name:     req.Name,
			JoinedAt: time.Now().UnixMilli(),
			Online:   true,
		}
		if err := s.groupchatStorage.AddMember(member); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResponse(w, map[string]interface{}{
			"id":        member.ID,
			"user_id":   member.UserID,
			"name":      member.Name,
			"online":    true,
			"joined_at": member.JoinedAt,
		})
		return
	}

	if r.Method == "DELETE" {
		// Parse member ID from URL path: /api/groupchat/rooms/{roomId}/members/{memberId}
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/groupchat/rooms/"), "/")
		if len(pathParts) >= 4 {
			memberID := pathParts[3]
			if err := s.groupchatStorage.RemoveMember(roomID, memberID); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			jsonResponse(w, map[string]bool{"ok": true})
			return
		}
		http.Error(w, "member ID required", 400)
		return
	}

	http.Error(w, "method not allowed", 405)
}

func (s *Server) handleGroupchatRoomSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/groupchat/rooms/")
	if path == "" {
		http.Error(w, "not found", 404)
		return
	}

	parts := strings.Split(path, "/")
	roomID := parts[0]

	// Sub-routes: /messages, /members, /agents, /invite, /stream
	if len(parts) >= 2 {
		switch parts[1] {
		case "messages":
			s.handleGroupchatMessages(w, r, roomID)
			return
		case "members":
			s.handleGroupchatRoomMembers(w, r, roomID)
			return
		case "agents":
			s.handleGroupchatRoomAgents(w, r, roomID)
			return
		case "invite":
			s.handleGroupchatRoomInvite(w, r, roomID)
			return
		case "stream":
			s.handleGroupchatStream(w, r, roomID)
			return
		}
	}

	// Room-level operations (GET/DELETE)
	if s.groupchatStorage == nil {
		http.Error(w, "groupchat not initialized", 500)
		return
	}

	room, err := s.groupchatStorage.GetRoom(roomID)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	switch r.Method {
	case "GET":
		jsonResponse(w, map[string]interface{}{
			"id":          room.ID,
			"name":        room.Name,
			"description": room.InviteCode,
			"members":     []string{},
			"agent_ids":   []string{},
			"created_at":  room.CreatedAt,
		})
	case "DELETE":
		if err := s.groupchatStorage.DeleteRoom(roomID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) replyAsAgent(roomID string, agent *groupchat.RoomAgent, history []groupchat.ChatMessage, userMessage string) {
	// Build message history for LLM
	messages := make([]provider.Message, 0)

	// System prompt
	systemPrompt := agent.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = fmt.Sprintf("You are %s, an AI assistant in a group chat. Your profile is: %s. Description: %s. Reply concisely and helpfully.",
			agent.Name, agent.Profile, agent.Description)
	}
	messages = append(messages, provider.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	// Add recent history as context
	for _, msg := range history {
		role := "user"
		if msg.Type == "agent" {
			role = "assistant"
		}
		if msg.SenderID == "system" {
			role = "system"
		}
		messages = append(messages, provider.Message{
			Role:    role,
			Content: msg.Content,
		})
	}

	// Call LLM
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := s.provider.Chat(ctx, messages)
	if err != nil {
		// Save error as agent message
		errMsg := &groupchat.ChatMessage{
			ID:         uuid.New().String(),
			RoomID:     roomID,
			SenderID:   agent.ID,
			SenderName: agent.Name,
			Content:    fmt.Sprintf("[Error: %s]", err.Error()),
			Timestamp:  time.Now().UnixMilli(),
			Type:       "agent",
		}
		s.groupchatStorage.SaveMessage(errMsg)
		return
	}

	// Save agent reply
	replyMsg := &groupchat.ChatMessage{
		ID:         uuid.New().String(),
		RoomID:     roomID,
		SenderID:   agent.ID,
		SenderName: agent.Name,
		Content:    resp.Content,
		Timestamp:  time.Now().UnixMilli(),
		Type:       "agent",
	}
	s.groupchatStorage.SaveMessage(replyMsg)
}

func (s *Server) handleGroupchatRoomAgents(w http.ResponseWriter, r *http.Request, roomID string) {

	if s.groupchatStorage == nil {
		http.Error(w, "groupchat not initialized", 500)
		return
	}

	if r.Method == "GET" {
		agents, _ := s.groupchatStorage.GetAgents(roomID)
		result := make([]interface{}, 0)
		for _, a := range agents {
			result = append(result, map[string]interface{}{
				"id":            a.ID,
				"agent_id":      a.AgentID,
				"name":          a.Name,
				"profile":       a.Profile,
				"description":   a.Description,
				"system_prompt": a.SystemPrompt,
				"temperature":   a.Temperature,
				"tools":         a.Tools,
				"invited":       a.Invited,
			})
		}
		jsonResponse(w, result)
		return
	}

	if r.Method == "POST" {
		var req struct {
			AgentID      string  `json:"agent_id"`
			Name         string  `json:"name"`
			Profile      string  `json:"profile"`
			Description  string  `json:"description"`
			SystemPrompt string  `json:"system_prompt"`
			Temperature  float64 `json:"temperature"`
			Tools        string  `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		temp := req.Temperature
		if temp <= 0 {
			temp = 0.7
		}
		agent := &groupchat.RoomAgent{
			ID:           uuid.New().String(),
			RoomID:       roomID,
			AgentID:      req.AgentID,
			Name:         req.Name,
			Profile:      req.Profile,
			Description:  req.Description,
			SystemPrompt: req.SystemPrompt,
			Temperature:  temp,
			Tools:        req.Tools,
			Invited:      1,
			CreatedAt:    time.Now().UnixMilli(),
		}
		if err := s.groupchatStorage.CreateAgent(agent); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResponse(w, map[string]interface{}{
			"id":            agent.ID,
			"agent_id":      agent.AgentID,
			"name":          agent.Name,
			"profile":       agent.Profile,
			"description":   agent.Description,
			"system_prompt": agent.SystemPrompt,
			"temperature":   agent.Temperature,
			"tools":         agent.Tools,
			"invited":       true,
		})
		return
	}

	if r.Method == "PUT" {
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/groupchat/rooms/"), "/")
		if len(pathParts) >= 3 {
			agentID := pathParts[2]
			var req struct {
				Name         string  `json:"name"`
				Profile      string  `json:"profile"`
				Description  string  `json:"description"`
				SystemPrompt string  `json:"system_prompt"`
				Temperature  float64 `json:"temperature"`
				Tools        string  `json:"tools"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request", 400)
				return
			}
			existing, err := s.groupchatStorage.GetAgent(agentID)
			if err != nil || existing == nil {
				http.Error(w, "agent not found", 404)
				return
			}
			if req.Name != "" {
				existing.Name = req.Name
			}
			if req.Profile != "" {
				existing.Profile = req.Profile
			}
			existing.Description = req.Description
			existing.SystemPrompt = req.SystemPrompt
			if req.Temperature > 0 {
				existing.Temperature = req.Temperature
			}
			existing.Tools = req.Tools
			if err := s.groupchatStorage.UpdateAgent(existing); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			jsonResponse(w, map[string]interface{}{
				"id":            existing.ID,
				"agent_id":      existing.AgentID,
				"name":          existing.Name,
				"profile":       existing.Profile,
				"description":   existing.Description,
				"system_prompt": existing.SystemPrompt,
				"temperature":   existing.Temperature,
				"tools":         existing.Tools,
			})
			return
		}
		http.Error(w, "agent ID required", 400)
		return
	}

	if r.Method == "DELETE" {
		pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/groupchat/rooms/"), "/")
		if len(pathParts) >= 3 {
			agentID := pathParts[2]
			if err := s.groupchatStorage.DeleteAgent(agentID); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			jsonResponse(w, map[string]bool{"ok": true})
			return
		}
		http.Error(w, "agent ID required", 400)
		return
	}

	http.Error(w, "method not allowed", 405)
}

func (s *Server) handleGroupchatRooms(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		if s.groupchatStorage == nil {
			http.Error(w, "groupchat not initialized", 500)
			return
		}

		room := &groupchat.Room{
			ID:         uuid.New().String(),
			Name:       req.Name,
			InviteCode: "",
			CreatedAt:  time.Now().UnixMilli(),
			UpdatedAt:  time.Now().UnixMilli(),
		}

		if err := s.groupchatStorage.SaveRoom(room); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		jsonResponse(w, map[string]interface{}{
			"id":          room.ID,
			"name":        room.Name,
			"description": room.InviteCode,
			"members":     []string{},
			"agent_ids":   []string{},
			"created_at":  room.CreatedAt,
		})
		return
	}

	if r.Method == "GET" {
		if s.groupchatStorage == nil {
			jsonResponse(w, []interface{}{})
			return
		}

		rooms, _ := s.groupchatStorage.ListRooms()
		result := make([]interface{}, 0)
		for _, room := range rooms {
			agents, _ := s.groupchatStorage.GetAgents(room.ID)
			agentIDs := make([]string, 0, len(agents))
			for _, a := range agents {
				agentIDs = append(agentIDs, a.ID)
			}
			result = append(result, map[string]interface{}{
				"id":          room.ID,
				"name":        room.Name,
				"description": room.InviteCode,
				"members":     []string{},
				"agent_ids":   agentIDs,
				"created_at":  room.CreatedAt,
			})
		}
		jsonResponse(w, result)
		return
	}

	http.Error(w, "method not allowed", 405)
}

func (s *Server) handleGroupchatMessages(w http.ResponseWriter, r *http.Request, roomID string) {
	if s.groupchatStorage == nil {
		http.Error(w, "groupchat not initialized", 500)
		return
	}

	if r.Method == "GET" {
		messages, _ := s.groupchatStorage.GetMessages(roomID, 100)
		result := make([]interface{}, 0)
		for _, msg := range messages {
			// Use SenderName for display, fallback to SenderID
			sender := msg.SenderName
			if sender == "" {
				sender = msg.SenderID
			}
			result = append(result, map[string]interface{}{
				"id":        msg.ID,
				"room_id":   msg.RoomID,
				"sender":    sender,
				"role":      msg.Type,
				"content":   msg.Content,
				"timestamp": msg.Timestamp,
			})
		}
		jsonResponse(w, result)
		return
	}

	if r.Method == "POST" {
		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}

		msg := &groupchat.ChatMessage{
			ID:         uuid.New().String(),
			RoomID:     roomID,
			SenderID:   "user",
			SenderName: "User",
			Content:    req.Content,
			Timestamp:  time.Now().UnixMilli(),
			Type:       "text",
		}

		if err := s.groupchatStorage.SaveMessage(msg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		jsonResponse(w, map[string]interface{}{
			"id":        msg.ID,
			"room_id":   msg.RoomID,
			"sender":    msg.SenderName,
			"role":      msg.Type,
			"content":   msg.Content,
			"timestamp": msg.Timestamp,
		})
		return
	}

	http.Error(w, "method not allowed", 405)
}
