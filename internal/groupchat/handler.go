package groupchat

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/compress"
)

// Handler HTTP 处理器
type Handler struct {
	server  *Server
	storage *Storage
	mux     *http.ServeMux
}

// NewHandler 创建处理器
func NewHandler(storage *Storage) *Handler {
	h := &Handler{
		server:  NewServer(storage),
		storage: storage,
		mux:     http.NewServeMux(),
	}
	h.registerRoutes()
	return h
}

// ServeHTTP 实现 http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// registerRoutes 注册路由
func (h *Handler) registerRoutes() {
	// WebSocket
	h.mux.HandleFunc("/ws", h.server.HandleWebSocket)

	// REST API
	h.mux.HandleFunc("GET /rooms", h.listRooms)
	h.mux.HandleFunc("POST /rooms", h.createRoom)
	h.mux.HandleFunc("GET /rooms/{id}", h.getRoom)
	h.mux.HandleFunc("DELETE /rooms/{id}", h.deleteRoom)
	h.mux.HandleFunc("PUT /rooms/{id}/config", h.updateRoomConfig)
	h.mux.HandleFunc("GET /room/join/{code}", h.joinByCode)

	// Agents
	h.mux.HandleFunc("GET /rooms/{id}/agents", h.listAgents)
	h.mux.HandleFunc("POST /rooms/{id}/agents", h.addAgent)
	h.mux.HandleFunc("DELETE /rooms/{id}/agents/{agentId}", h.removeAgent)

	// Members
	h.mux.HandleFunc("GET /rooms/{id}/members", h.listMembers)

	// Messages
	h.mux.HandleFunc("GET /rooms/{id}/messages", h.listMessages)
	h.mux.HandleFunc("POST /rooms/{id}/messages", h.sendMessage)

	// Context compression
	h.mux.HandleFunc("POST /rooms/{id}/compress", h.forceCompress)

	// Additional routes for web compatibility
	h.mux.HandleFunc("PUT /rooms/{id}", h.updateRoom)                       // PUT /rooms/{id} (not just /config)
	h.mux.HandleFunc("POST /rooms/{id}/invite", h.generateInviteCode)       // Generate invite code
	h.mux.HandleFunc("POST /rooms/join", h.joinRoom)                        // POST /rooms/join (not just GET)
	h.mux.HandleFunc("POST /rooms/{id}/leave", h.leaveRoom)                 // Leave room
	h.mux.HandleFunc("DELETE /rooms/{id}/members/{userId}", h.removeMember) // Remove member
	h.mux.HandleFunc("GET /agents", h.listAllAgents)                        // List all agents (global)
}

// ─── Room Handlers ───────────────────────────────────────────────────────────

// listRooms 列出所有房间
func (h *Handler) listRooms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rooms, err := h.storage.GetAllRooms()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if rooms == nil {
		rooms = []Room{}
	}

	jsonResponse(w, map[string]interface{}{"rooms": rooms})
}

// createRoom 创建房间
func (h *Handler) createRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data struct {
		Name        string            `json:"name"`
		InviteCode  string            `json:"inviteCode"`
		Compression CompressionConfig `json:"compression"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if data.Compression.TriggerTokens == 0 {
		data.Compression = DefaultCompressionConfig()
	}

	// Generate invite code if not provided
	if data.InviteCode == "" {
		data.InviteCode = GenerateInviteCode()
	}

	room := &Room{
		ID:               uuid.New().String(),
		Name:             data.Name,
		InviteCode:       data.InviteCode,
		TriggerTokens:    data.Compression.TriggerTokens,
		MaxHistoryTokens: data.Compression.MaxHistoryTokens,
		TailMessageCount: data.Compression.TailMessageCount,
		CreatedAt:        Now(),
		UpdatedAt:        Now(),
	}

	if err := h.storage.CreateRoom(room); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Create compressor for room
	h.server.GetOrCreateCompressor(room.ID)

	agents, _ := h.storage.GetAgents(room.ID)
	jsonResponse(w, map[string]interface{}{"room": room, "agents": agents})
}

// getRoom 获取房间详情
func (h *Handler) getRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	room, err := h.storage.GetRoom(roomID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if room == nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	members, _ := h.storage.GetMembers(roomID)
	agents, _ := h.storage.GetAgents(roomID)
	messages, _ := h.storage.GetMessages(roomID, 100)

	jsonResponse(w, map[string]interface{}{
		"room":     room,
		"members":  members,
		"agents":   agents,
		"messages": messages,
	})
}

// deleteRoom 删除房间
func (h *Handler) deleteRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	if err := h.storage.DeleteRoom(roomID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true})
}

// updateRoomConfig 更新房间配置
func (h *Handler) updateRoomConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	var config CompressionConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := h.storage.UpdateRoomConfig(roomID, config); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update compressor
	compressor := h.server.GetOrCreateCompressor(roomID)
	if compressor != nil {
		compressor.triggerTokens = config.TriggerTokens
		compressor.maxHistoryTokens = config.MaxHistoryTokens
		compressor.tailMessageCount = config.TailMessageCount
	}

	room, _ := h.storage.GetRoom(roomID)
	jsonResponse(w, map[string]interface{}{"room": room})
}

// joinByCode 通过邀请码加入 (GET /room/join/{code})
func (h *Handler) joinByCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := extractPathVar(r.URL.Path, "code")
	if code == "" {
		http.Error(w, "Invite code required", http.StatusBadRequest)
		return
	}

	room, err := h.storage.GetRoomByInviteCode(code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if room == nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	jsonResponse(w, map[string]interface{}{"room": room})
}

// updateRoom 更新房间信息 (PUT /rooms/{id})
func (h *Handler) updateRoom(w http.ResponseWriter, r *http.Request) {
	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	var data struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	room, err := h.storage.GetRoom(roomID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if room == nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	if data.Name != "" {
		room.Name = data.Name
	}
	room.UpdatedAt = Now()

	if err := h.storage.UpdateRoom(room); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"room": room})
}

// generateInviteCode 生成邀请码 (POST /rooms/{id}/invite)
func (h *Handler) generateInviteCode(w http.ResponseWriter, r *http.Request) {
	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	room, err := h.storage.GetRoom(roomID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if room == nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	// Generate new invite code
	room.InviteCode = GenerateInviteCode()
	room.UpdatedAt = Now()

	if err := h.storage.UpdateRoom(room); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"code": room.InviteCode})
}

// joinRoom 加入房间 (POST /rooms/join)
func (h *Handler) joinRoom(w http.ResponseWriter, r *http.Request) {
	var data struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if data.Code == "" {
		http.Error(w, "Invite code required", http.StatusBadRequest)
		return
	}

	room, err := h.storage.GetRoomByInviteCode(data.Code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if room == nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}

	jsonResponse(w, map[string]interface{}{"room": room})
}

// leaveRoom 离开房间 (POST /rooms/{id}/leave)
func (h *Handler) leaveRoom(w http.ResponseWriter, r *http.Request) {
	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	// For now, just return success (actual leave logic would remove member)
	jsonResponse(w, map[string]interface{}{"success": true})
}

// removeMember 移除成员 (DELETE /rooms/{id}/members/{userId})
func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	roomID := extractPathVar(r.URL.Path, "id")
	userID := extractPathVar(r.URL.Path, "userId")
	if roomID == "" || userID == "" {
		http.Error(w, "Room ID and User ID required", http.StatusBadRequest)
		return
	}

	// Remove member from storage
	if err := h.storage.RemoveMember(roomID, userID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{"success": true})
}

// listAllAgents 列出所有代理 (GET /agents)
func (h *Handler) listAllAgents(w http.ResponseWriter, r *http.Request) {
	// Return a list of available agent profiles
	agents := []map[string]interface{}{
		{"id": "default", "name": "Default Agent", "description": "Default assistant"},
		{"id": "coder", "name": "Coder", "description": "Programming assistant"},
		{"id": "researcher", "name": "Researcher", "description": "Research assistant"},
	}
	jsonResponse(w, map[string]interface{}{"agents": agents})
}

// ─── Agent Handlers ─────────────────────────────────────────────────────────

// listAgents 列出房间代理
func (h *Handler) listAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	agents, err := h.storage.GetAgents(roomID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if agents == nil {
		agents = []RoomAgent{}
	}

	jsonResponse(w, map[string]interface{}{"agents": agents})
}

// addAgent 添加代理
func (h *Handler) addAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	var data struct {
		Profile     string `json:"profile"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 检查是否已存在
	exists, _ := h.storage.AgentExistsInRoom(roomID, data.Profile)
	if exists {
		http.Error(w, "agent already in room", http.StatusConflict)
		return
	}

	agent := &RoomAgent{
		ID:          uuid.New().String(),
		RoomID:      roomID,
		AgentID:     uuid.New().String(),
		Profile:     data.Profile,
		Name:        data.Name,
		Description: data.Description,
		Invited:     1,
		CreatedAt:   Now(),
	}

	if err := h.storage.CreateAgent(agent); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	agents, _ := h.storage.GetAgents(roomID)
	jsonResponse(w, map[string]interface{}{"agent": agent, "agents": agents})
}

// removeAgent 移除代理
func (h *Handler) removeAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := extractPathVar(r.URL.Path, "id")
	agentID := extractPathVar(r.URL.Path, "agentId")
	if roomID == "" || agentID == "" {
		http.Error(w, "Room ID and Agent ID required", http.StatusBadRequest)
		return
	}

	if err := h.storage.DeleteAgent(agentID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	agents, _ := h.storage.GetAgents(roomID)
	jsonResponse(w, map[string]interface{}{"agents": agents})
}

// ─── Member Handlers ─────────────────────────────────────────────────────────

// listMembers 列出房间成员
func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	members, err := h.storage.GetMembers(roomID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if members == nil {
		members = []Member{}
	}

	// Add online status
	h.server.mu.RLock()
	roomClients, ok := h.server.rooms[roomID]
	h.server.mu.RUnlock()

	if ok {
		onlineMap := make(map[string]bool)
		for _, client := range roomClients {
			onlineMap[client.UserID] = true
		}
		for i := range members {
			members[i].Online = onlineMap[members[i].UserID]
		}
	}

	jsonResponse(w, map[string]interface{}{"members": members})
}

// ─── Message Handlers ────────────────────────────────────────────────────────

// listMessages 列出消息
func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	messages, err := h.storage.GetMessages(roomID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if messages == nil {
		messages = []ChatMessage{}
	}

	jsonResponse(w, map[string]interface{}{"messages": messages})
}

// sendMessage 发送消息
func (h *Handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	var data struct {
		SenderID   string `json:"senderId"`
		SenderName string `json:"senderName"`
		Content    string `json:"content"`
		Type       string `json:"type,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	msgType := data.Type
	if msgType == "" {
		msgType = "text"
	}

	chatMsg := &ChatMessage{
		ID:         uuid.New().String(),
		RoomID:     roomID,
		SenderID:   data.SenderID,
		SenderName: data.SenderName,
		Content:    data.Content,
		Timestamp:  Now(),
		Type:       msgType,
	}

	if err := h.storage.SaveMessage(chatMsg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Broadcast to room via WebSocket
	h.server.broadcastToRoom(roomID, map[string]interface{}{
		"type":   "message",
		"roomId": roomID,
		"data":   chatMsg,
	})

	jsonResponse(w, map[string]interface{}{"message": chatMsg})
}

// ─── Context Handlers ────────────────────────────────────────────────────────

// forceCompress 强制压缩上下文
func (h *Handler) forceCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	roomID := extractPathVar(r.URL.Path, "id")
	if roomID == "" {
		http.Error(w, "Room ID required", http.StatusBadRequest)
		return
	}

	compressor := h.server.GetOrCreateCompressor(roomID)
	if compressor == nil {
		http.Error(w, "Failed to get compressor", http.StatusInternalServerError)
		return
	}

	// Broadcast compression event
	h.server.broadcastToRoom(roomID, map[string]interface{}{
		"type":   "context_compressing",
		"roomId": roomID,
	})

	// Perform actual compression
	room, err := h.storage.GetRoom(roomID)
	if err != nil || room == nil {
		jsonResponse(w, map[string]interface{}{"compressed": false, "error": "room not found"})
		return
	}

	messages, err := h.storage.GetMessages(roomID, 1000)
	if err != nil || len(messages) == 0 {
		jsonResponse(w, map[string]interface{}{"room": room, "compressed": false, "error": "no messages"})
		return
	}

	// Convert to compress.Message format
	compressMsgs := make([]compress.Message, len(messages))
	for i, msg := range messages {
		role := "user"
		if msg.Type == "agent" || msg.Type == "system" {
			role = msg.Type
		}
		compressMsgs[i] = compress.Message{
			Role:      role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		}
	}

	// Execute compression
	compressMgr := compress.NewManager("")
	summary, compressed, err := compressMgr.CompressSession(roomID, compressMsgs, compressor.tailMessageCount)
	if err != nil {
		jsonResponse(w, map[string]interface{}{"room": room, "compressed": false, "error": err.Error()})
		return
	}

	// Update room token count
	if room != nil && summary != nil {
		newTokens := 0
		for _, msg := range compressed {
			newTokens += compress.EstimateTokens(msg.Content)
		}
		room.TotalTokens = newTokens
		h.storage.UpdateRoom(room)
	}

	jsonResponse(w, map[string]interface{}{"room": room, "compressed": true, "summary": summary})
}

// jsonResponse 返回 JSON 响应
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// extractPathVar 提取路径变量 (简单实现)
func extractPathVar(path, name string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		if part == name && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	// 处理 {name} 格式
	pattern := "{" + name + "}"
	for i, part := range parts {
		if strings.HasPrefix(part, pattern[:1]) && strings.HasSuffix(part, pattern[len(pattern)-1:]) && len(part) > 2 {
			return part[1 : len(part)-1]
		}
		if part == pattern && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
