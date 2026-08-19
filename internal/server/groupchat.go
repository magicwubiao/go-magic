package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/compress"
	"github.com/magicwubiao/go-magic/internal/groupchat"
	"github.com/magicwubiao/go-magic/internal/tool"
)

var (
	gcToolStartRe    = regexp.MustCompile(`>>>TOOL_START\|([^|]+)\|([\s\S]*?)<<<`)
	gcToolResultRe   = regexp.MustCompile(`>>>TOOL_RESULT_START\|([^|]+)\|([^|]+)\|([^|]+)<<<\n?([\s\S]*?)\n?>>>TOOL_RESULT_END<<<`)
	gcTurnStartRe    = regexp.MustCompile(`>>>TURN_START<<<`)
)

// stripAgentProtocolMarks strips all internal agent protocol markers
// (TOOL_START, TOOL_RESULT, TURN_START) from the given content string.
func stripAgentProtocolMarks(s string) string {
	s = gcToolStartRe.ReplaceAllString(s, "")
	s = gcToolResultRe.ReplaceAllString(s, "")
	s = gcTurnStartRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

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

	// Build conversation context with optional compression
	room, _ := s.groupchatStorage.GetRoom(roomID)
	tailCount := 20
	if room != nil && room.TailMessageCount > 0 {
		tailCount = room.TailMessageCount
	}
	conversationContext, summary, _, _ := s.buildAgentContext(roomID, tailCount)

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
	for _, a := range mentioned {
		select {
		case <-ctx.Done():
			close(heartbeatDone)
			return
		default:
		}

		// Send agent start event
		startData, _ := json.Marshal(map[string]interface{}{
			"type":    "start",
			"agent":   a.Name,
			"agentId": a.ID,
		})
		writeSSE("data: " + string(startData) + "\n\n")

		// Create working directory for this agent
		workDir := filepath.Join(s.cfg.WorkingDir, "groupchat", roomID, a.ID)
		os.MkdirAll(workDir, 0755)

		// Build enhanced system prompt with compression summary
		systemPrompt := buildAgentSystemPrompt(a, workDir, summary)

		// Build input with conversation context
		input := fmt.Sprintf("Recent conversation:\n%s\n\nUser message: %s", conversationContext, req.Content)

		// Create context with working directory
		agentCtx := tool.WithWorkDir(ctx, workDir)

		// Get tools and filter by agent's tool permissions
		tools := s.toolReg.ListWithSchemas()
		if a.Tools != "" {
			tools = filterToolsMap(a.Tools, tools)
		}

		// Agent options
		agentOpts := []agent.AgentOption{
			agent.WithLoopLimits(3, 10),
			agent.WithSteering(agent.SteeringConfig{MaxIterations: 20}),
		}

		// Create EnhancedAgent with full tool access
		enhancedAgent := agent.NewEnhancedAgent(s.provider, s.toolReg, tools, systemPrompt, agentOpts...)

		// Run streaming conversation
		var fullContent string
		streamErr := enhancedAgent.RunConversationStream(agentCtx, input, func(content string, done bool) {
			if content == "" {
				return
			}
			select {
			case <-agentCtx.Done():
				return
			default:
			}

			// Skip TURN_START markers completely (internal protocol)
			if strings.TrimSpace(content) == ">>>TURN_START<<<" {
				return
			}

			// Handle tool start marker -> emit tool_start event, don't render as text
			if strings.Contains(content, ">>>TOOL_START|") {
				matches := gcToolStartRe.FindStringSubmatch(content)
				if len(matches) > 2 {
					data, _ := json.Marshal(map[string]interface{}{
						"type":    "tool_start",
						"agent":   a.Name,
						"agentId": a.ID,
						"name":    matches[1],
						"args":    matches[2],
					})
					writeSSE("data: " + string(data) + "\n\n")
					return
				}
			}

			// Handle tool result marker -> emit tool_result event, don't render as text
			if strings.Contains(content, ">>>TOOL_RESULT_START|") {
				matches := gcToolResultRe.FindStringSubmatch(content)
				if len(matches) > 4 {
					toolName := matches[1]
					success := matches[2] == "true"
					duration := matches[3]
					toolContent := strings.TrimSpace(matches[4])
					data, _ := json.Marshal(map[string]interface{}{
						"type":     "tool_result",
						"agent":    a.Name,
						"agentId":  a.ID,
						"name":     toolName,
						"success":  success,
						"duration": duration,
						"content":  toolContent,
					})
					writeSSE("data: " + string(data) + "\n\n")
					return
				}
			}

			fullContent += content
			data, _ := json.Marshal(map[string]interface{}{
				"type":    "content",
				"agent":   a.Name,
				"agentId": a.ID,
				"content": content,
			})
			writeSSE("data: " + string(data) + "\n\n")
		})

		if streamErr != nil {
			errData, _ := json.Marshal(map[string]interface{}{
				"type":  "error",
				"agent": a.Name,
				"error": streamErr.Error(),
			})
			writeSSE("data: " + string(errData) + "\n\n")
			continue
		}

		// Save complete message to database with protocol markers stripped
		cleanContent := stripAgentProtocolMarks(fullContent)
		replyMsg := &groupchat.ChatMessage{
			ID:         uuid.New().String(),
			RoomID:     roomID,
			SenderID:   a.ID,
			SenderName: a.Name,
			Content:    cleanContent,
			Timestamp:  time.Now().UnixMilli(),
			Type:       "agent",
		}
		s.groupchatStorage.SaveMessage(replyMsg)

		// Save session profile for continuity
		s.groupchatStorage.SaveSessionProfile(replyMsg.ID, roomID, a.ID, a.Profile)

		// Send done event
		doneData, _ := json.Marshal(map[string]interface{}{
			"type":      "done",
			"agent":     a.Name,
			"agentId":   a.ID,
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
			"description": room.Description,
			"members":     []string{},
			"agent_ids":   []string{},
			"created_at":  room.CreatedAt,
		})
	case "PUT":
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", 400)
			return
		}
		if req.Name == "" {
			http.Error(w, "name is required", 400)
			return
		}
		room.Name = req.Name
		if err := s.groupchatStorage.UpdateRoom(room); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		jsonResponse(w, map[string]interface{}{"ok": true, "name": room.Name})
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

func (s *Server) replyAsAgent(roomID string, a *groupchat.RoomAgent, history []groupchat.ChatMessage, userMessage string) {
	if s.provider == nil || s.groupchatStorage == nil || s.toolReg == nil {
		return
	}

	// Create working directory for this agent
	workDir := filepath.Join(s.cfg.WorkingDir, "groupchat", roomID, a.ID)
	os.MkdirAll(workDir, 0755)

	// Build conversation context
	var contextBuilder strings.Builder
	for _, msg := range history {
		role := msg.SenderName
		if role == "" {
			role = msg.SenderID
		}
		contextBuilder.WriteString(fmt.Sprintf("%s: %s\n", role, msg.Content))
	}
	conversationContext := contextBuilder.String()
	input := fmt.Sprintf("Recent conversation:\n%s\n\nUser message: %s", conversationContext, userMessage)

	// Create context with working directory
	ctx := tool.WithWorkDir(context.Background(), workDir)

	// Get tools and filter by agent's tool permissions
	tools := s.toolReg.ListWithSchemas()
	if a.Tools != "" {
		tools = filterToolsMap(a.Tools, tools)
	}

	// Agent options
	agentOpts := []agent.AgentOption{
		agent.WithLoopLimits(3, 10),
		agent.WithSteering(agent.SteeringConfig{MaxIterations: 20}),
	}

	// Build enhanced system prompt with compression summary
	systemPrompt := buildAgentSystemPrompt(a, workDir, "")
	// Create EnhancedAgent with full tool access
	enhancedAgent := agent.NewEnhancedAgent(s.provider, s.toolReg, tools, systemPrompt, agentOpts...)

	// Run conversation
	result, err := enhancedAgent.RunConversation(ctx, input)
	if err != nil {
		// Save error as agent message
		errMsg := &groupchat.ChatMessage{
			ID:         uuid.New().String(),
			RoomID:     roomID,
			SenderID:   a.ID,
			SenderName: a.Name,
			Content:    fmt.Sprintf("[Error: %s]", err.Error()),
			Timestamp:  time.Now().UnixMilli(),
			Type:       "agent",
		}
		s.groupchatStorage.SaveMessage(errMsg)
		return
	}

	// Save agent reply with internal protocol markers stripped
	cleanResult := stripAgentProtocolMarks(result)
	replyMsg := &groupchat.ChatMessage{
		ID:         uuid.New().String(),
		RoomID:     roomID,
		SenderID:   a.ID,
		SenderName: a.Name,
		Content:    cleanResult,
		Timestamp:  time.Now().UnixMilli(),
		Type:       "agent",
	}
	s.groupchatStorage.SaveMessage(replyMsg)

	// Save session profile for continuity
	s.groupchatStorage.SaveSessionProfile(replyMsg.ID, roomID, a.ID, a.Profile)
}

// buildAgentContext builds conversation context with optional compression.
// Returns: context string, summary (empty if no compression), whether compression was applied.
func (s *Server) buildAgentContext(roomID string, tailCount int) (string, string, bool, error) {
	if tailCount <= 0 {
		tailCount = 20
	}

	// Get messages for compression check
	allMsgs, err := s.groupchatStorage.GetMessages(roomID, 1000)
	if err != nil {
		// Fallback to just recent messages
		recent, err2 := s.groupchatStorage.GetMessages(roomID, tailCount)
		if err2 != nil {
			return "", "", false, err2
		}
		return buildSimpleContext(recent), "", false, nil
	}

	// Check if compression is needed
	totalTokens := compress.EstimateMessagesTokens(toCompressMessages(allMsgs), "")
	triggerTokens := 100000 // default

	room, _ := s.groupchatStorage.GetRoom(roomID)
	if room != nil && room.TriggerTokens > 0 {
		triggerTokens = room.TriggerTokens
	}

	if totalTokens < triggerTokens || len(allMsgs) <= tailCount+2 {
		// No compression needed, use recent messages
		recent := allMsgs
		if len(allMsgs) > tailCount {
			recent = allMsgs[len(allMsgs)-tailCount:]
		}
		return buildSimpleContext(recent), "", false, nil
	}

	// Compression needed - compress middle messages
	compressMgr := compress.NewManager("")
	summary, _, err := compressMgr.CompressSession(roomID, toCompressMessages(allMsgs), tailCount)
	if err != nil {
		// Fallback to recent messages
		recent := allMsgs
		if len(allMsgs) > tailCount {
			recent = allMsgs[len(allMsgs)-tailCount:]
		}
		return buildSimpleContext(recent), "", false, nil
	}

	// Build context from tail messages (most recent)
	recent := allMsgs
	if len(allMsgs) > tailCount {
		recent = allMsgs[len(allMsgs)-tailCount:]
	}
	contextStr := buildSimpleContext(recent)

	return contextStr, summary, true, nil
}

// buildSimpleContext builds a conversation context string from messages.
func buildSimpleContext(msgs []groupchat.ChatMessage) string {
	var sb strings.Builder
	for _, msg := range msgs {
		role := msg.SenderName
		if role == "" {
			role = msg.SenderID
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", role, msg.Content))
	}
	return sb.String()
}

// toCompressMessages converts ChatMessage slice to compress.Message slice.
func toCompressMessages(msgs []groupchat.ChatMessage) []compress.Message {
	result := make([]compress.Message, len(msgs))
	for i, msg := range msgs {
		role := "user"
		if msg.Type == "agent" || msg.Type == "system" {
			role = msg.Type
		}
		result[i] = compress.Message{
			Role:      role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		}
	}
	return result
}

// filterToolsMap filters tools based on the agent's Tools field.
// agent.Tools is a JSON array string like ["write_file","read_file"].
// Empty string means all tools are available.
func filterToolsMap(agentTools string, allTools []map[string]interface{}) []map[string]interface{} {
	if agentTools == "" {
		return allTools
	}

	var allowed []string
	if err := json.Unmarshal([]byte(agentTools), &allowed); err != nil || len(allowed) == 0 {
		return allTools
	}

	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}

	filtered := make([]map[string]interface{}, 0, len(allowed))
	for _, t := range allTools {
		if name, ok := t["name"].(string); ok && allowedSet[name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// buildAgentSystemPrompt builds an enhanced system prompt for a group chat agent.
func buildAgentSystemPrompt(a *groupchat.RoomAgent, workDir string, summary string) string {
	systemPrompt := a.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = fmt.Sprintf(`You are %s, an AI assistant in a group chat.

Your profile is: %s
Description: %s

## Guidelines
- Reply concisely and helpfully.
- You are part of a group conversation with multiple participants.
- Pay attention to who is speaking and maintain context.
- When asked about code or technical topics, provide clear explanations.
- If you need to write files, use your working directory.`,
			a.Name, a.Profile, a.Description)
	}

	// Add working directory instructions
	systemPrompt += fmt.Sprintf("\n\n## Working Directory\nYour working directory is: %s\n- Use write_file with RELATIVE paths to write files to this directory.\n- Do NOT use absolute paths like /tmp/.\n- Files you write here are accessible to other agents in the same room.", workDir)

	// Add tool usage guidelines
	systemPrompt += "\n\n## Tool Usage\n- You have access to tools like file read/write, code execution, and web search.\n- Use tools when they help accomplish the task more effectively.\n- For simple questions, just reply directly without calling tools."

	// Add temperature instruction
	if a.Temperature > 0 {
		systemPrompt += fmt.Sprintf("\n\n## Response Style\n- Temperature: %.1f (higher = more creative, lower = more precise)", a.Temperature)
	}

	// Add compressed context summary if available
	if summary != "" {
		systemPrompt += fmt.Sprintf("\n\n%s\n\n%s", compress.SummaryPrefix, summary)
	}

	return systemPrompt
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
			ID:          uuid.New().String(),
			Name:        req.Name,
			Description: req.Description,
			InviteCode:  "",
			CreatedAt:   time.Now().UnixMilli(),
			UpdatedAt:   time.Now().UnixMilli(),
		}

		if err := s.groupchatStorage.SaveRoom(room); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		jsonResponse(w, map[string]interface{}{
			"id":          room.ID,
			"name":        room.Name,
			"description": room.Description,
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
				"description": room.Description,
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
			// 归一化 role：存储层 Type 用 "text" 表示用户消息，
			// 前端依赖标准 role（user/agent/system）做样式区分。
			role := msg.Type
			if role == "text" || role == "" {
				role = "user"
			}
			result = append(result, map[string]interface{}{
				"id":        msg.ID,
				"room_id":   msg.RoomID,
				"sender":    sender,
				"role":      role,
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
			"role":      "user",
			"content":   msg.Content,
			"timestamp": msg.Timestamp,
		})
		return
	}

	http.Error(w, "method not allowed", 405)
}