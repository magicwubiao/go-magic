package groupchat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/magicwubiao/go-magic/internal/compress"
)

// Server Group Chat WebSocket 服务器
type Server struct {
	storage     *Storage
	upgrader    websocket.Upgrader
	rooms       map[string]map[string]*Client      // roomID -> clientID -> client
	roomAgents  map[string]map[string]*AgentClient // roomID -> agentID -> agent
	onlineUsers map[string]*Client                 // socketID -> client
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	compressors map[string]*ContextCompressor // roomID -> compressor
}

// Client WebSocket 客户端
type Client struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RoomID      string `json:"roomId,omitempty"`
	Conn        *websocket.Conn
	send        chan []byte
	server      *Server
	mu          sync.Mutex
}

// AgentClient Agent 客户端
type AgentClient struct {
	ID        string `json:"id"`
	RoomID    string `json:"roomId"`
	Profile   string `json:"profile"`
	Name      string `json:"name"`
	SessionID string `json:"sessionId"`
	Connected bool   `json:"connected"`
	CreatedAt int64  `json:"createdAt"`
}

// Message 消息结构
type Message struct {
	Type      string          `json:"type"`
	RoomID    string          `json:"roomId,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp int64           `json:"timestamp"`
}

// ContextCompressor handles context compression for rooms
type ContextCompressor struct {
	roomID           string
	triggerTokens    int
	maxHistoryTokens int
	tailMessageCount int
	mu               sync.RWMutex
}

// NewServer 创建服务器
func NewServer(storage *Storage) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		storage:     storage,
		upgrader:    websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		rooms:       make(map[string]map[string]*Client),
		roomAgents:  make(map[string]map[string]*AgentClient),
		onlineUsers: make(map[string]*Client),
		ctx:         ctx,
		cancel:      cancel,
		compressors: make(map[string]*ContextCompressor),
	}
}

// GetOrCreateCompressor gets or creates a context compressor for a room
func (s *Server) GetOrCreateCompressor(roomID string) *ContextCompressor {
	s.mu.Lock()
	defer s.mu.Unlock()

	if comp, ok := s.compressors[roomID]; ok {
		return comp
	}

	// Get room config
	room, err := s.storage.GetRoom(roomID)
	if err != nil || room == nil {
		// Use defaults
		comp := &ContextCompressor{
			roomID:           roomID,
			triggerTokens:    100000,
			maxHistoryTokens: 32000,
			tailMessageCount: 20,
		}
		s.compressors[roomID] = comp
		return comp
	}

	comp := &ContextCompressor{
		roomID:           roomID,
		triggerTokens:    room.TriggerTokens,
		maxHistoryTokens: room.MaxHistoryTokens,
		tailMessageCount: room.TailMessageCount,
	}
	s.compressors[roomID] = comp
	return comp
}

// HandleWebSocket 处理 WebSocket 连接
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &Client{
		ID:     uuid.New().String(),
		Conn:   conn,
		send:   make(chan []byte, 256),
		server: s,
	}

	go client.writePump()
	go client.readPump()
}

// readPump 处理读取
func (c *Client) readPump() {
	defer func() {
		c.server.removeClient(c)
		c.Conn.Close()
	}()

	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Message parse error: %v", err)
			continue
		}

		c.handleMessage(&msg)
	}
}

// writePump 处理写入
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)
			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Send sends a message to the client
func (c *Client) Send(data interface{}) error {
	message, err := json.Marshal(data)
	if err != nil {
		return err
	}

	select {
	case c.send <- message:
		return nil
	default:
		return fmt.Errorf("client send buffer full")
	}
}

// handleMessage 处理消息
func (c *Client) handleMessage(msg *Message) {
	switch msg.Type {
	case "join":
		c.handleJoin(msg)
	case "leave":
		c.handleLeave(msg)
	case "message":
		c.handleChatMessage(msg)
	case "typing":
		c.handleTyping(msg)
	case "stop_typing":
		c.handleStopTyping(msg)
	case "create_room":
		c.handleCreateRoom(msg)
	case "join_room":
		c.handleJoinRoom(msg)
	case "delete_room":
		c.handleDeleteRoom(msg)
	case "add_agent":
		c.handleAddAgent(msg)
	case "remove_agent":
		c.handleRemoveAgent(msg)
	case "get_history":
		c.handleGetHistory(msg)
	case "list_members":
		c.handleListMembers(msg)
	case "compress_context":
		c.handleCompressContext(msg)
	}
}

// handleJoin 处理加入
func (c *Client) handleJoin(msg *Message) {
	var data struct {
		UserID      string `json:"userId"`
		Name        string `json:"name"`
		Description string `json:"description"`
		RoomID      string `json:"roomId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError("Invalid join data")
		return
	}

	c.UserID = data.UserID
	c.Name = data.Name
	c.Description = data.Description
	c.RoomID = data.RoomID

	c.server.mu.Lock()
	c.server.onlineUsers[c.ID] = c
	if c.RoomID != "" {
		if _, ok := c.server.rooms[c.RoomID]; !ok {
			c.server.rooms[c.RoomID] = make(map[string]*Client)
		}
		c.server.rooms[c.RoomID][c.ID] = c

		// 更新数据库
		member := &Member{
			ID:          uuid.New().String(),
			RoomID:      c.RoomID,
			UserID:      c.UserID,
			Name:        c.Name,
			Description: c.Description,
			JoinedAt:    Now(),
			LastSeenAt:  Now(),
		}
		c.server.storage.AddMember(member)

		// 广播成员加入
		members, _ := c.server.storage.GetMembers(c.RoomID)
		c.server.broadcastToRoom(c.RoomID, map[string]interface{}{
			"type":     "member_joined",
			"roomId":   c.RoomID,
			"userId":   c.UserID,
			"userName": c.Name,
			"members":  members,
		})
	}
	c.server.mu.Unlock()

	// 发送确认
	c.Send(map[string]interface{}{
		"type":   "joined",
		"userId": c.UserID,
		"roomId": c.RoomID,
	})
}

// handleLeave 处理离开
func (c *Client) handleLeave(msg *Message) {
	if c.RoomID != "" {
		c.server.broadcastToRoom(c.RoomID, map[string]interface{}{
			"type":     "member_left",
			"roomId":   c.RoomID,
			"userId":   c.UserID,
			"userName": c.Name,
		})
	}
	c.server.removeClient(c)
}

// handleChatMessage 处理聊天消息
func (c *Client) handleChatMessage(msg *Message) {
	var data struct {
		Content string `json:"content"`
		Type    string `json:"type,omitempty"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError("Invalid message data")
		return
	}

	if c.RoomID == "" {
		c.sendError("Not in a room")
		return
	}

	msgType := data.Type
	if msgType == "" {
		msgType = "text"
	}

	chatMsg := &ChatMessage{
		ID:         uuid.New().String(),
		RoomID:     c.RoomID,
		SenderID:   c.UserID,
		SenderName: c.Name,
		Content:    data.Content,
		Timestamp:  Now(),
		Type:       msgType,
	}

	// 保存消息
	if err := c.server.storage.SaveMessage(chatMsg); err != nil {
		log.Printf("Failed to save message: %v", err)
	}

	// 广播消息
	c.server.broadcastToRoom(c.RoomID, map[string]interface{}{
		"type":   "message",
		"roomId": c.RoomID,
		"data":   chatMsg,
	})

	// 如果是 @ 某个代理，转发给代理处理
	if len(data.Content) > 0 && data.Content[0] == '@' {
		c.server.handleAgentMention(c.RoomID, chatMsg)
	}

	// Check if context compression is needed
	c.server.checkAndCompressContext(c.RoomID)
}

// handleTyping 处理打字
func (c *Client) handleTyping(msg *Message) {
	if c.RoomID == "" {
		return
	}
	c.server.broadcastToRoom(c.RoomID, map[string]interface{}{
		"type":     "typing",
		"roomId":   c.RoomID,
		"userId":   c.UserID,
		"userName": c.Name,
	}, c.ID)
}

// handleStopTyping 处理停止打字
func (c *Client) handleStopTyping(msg *Message) {
	if c.RoomID == "" {
		return
	}
	c.server.broadcastToRoom(c.RoomID, map[string]interface{}{
		"type":   "stop_typing",
		"roomId": c.RoomID,
		"userId": c.UserID,
	}, c.ID)
}

// handleCreateRoom 处理创建房间
func (c *Client) handleCreateRoom(msg *Message) {
	var data struct {
		Name        string            `json:"name"`
		InviteCode  string            `json:"inviteCode"`
		Compression CompressionConfig `json:"compression"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError("Invalid create room data")
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

	if err := c.server.storage.CreateRoom(room); err != nil {
		log.Printf("Create room error: %v", err)
		c.sendError("Failed to create room")
		return
	}

	// Create compressor for room
	c.server.GetOrCreateCompressor(room.ID)

	// 发送房间信息
	c.Send(map[string]interface{}{
		"type":       "room_created",
		"room":       room,
		"inviteCode": data.InviteCode,
	})
}

// handleJoinRoom 处理加入房间
func (c *Client) handleJoinRoom(msg *Message) {
	var data struct {
		RoomID     string `json:"roomId"`
		InviteCode string `json:"inviteCode,omitempty"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError("Invalid join room data")
		return
	}

	var room *Room
	var err error

	if data.InviteCode != "" {
		room, err = c.server.storage.GetRoomByInviteCode(data.InviteCode)
	} else {
		room, err = c.server.storage.GetRoom(data.RoomID)
	}

	if err != nil {
		log.Printf("Get room error: %v", err)
		c.sendError("Failed to get room")
		return
	}

	if room == nil {
		c.sendError("Room not found")
		return
	}

	c.RoomID = room.ID

	// Add to room
	c.server.mu.Lock()
	if _, ok := c.server.rooms[c.RoomID]; !ok {
		c.server.rooms[c.RoomID] = make(map[string]*Client)
	}
	c.server.rooms[c.RoomID][c.ID] = c
	c.server.mu.Unlock()

	// Add member
	member := &Member{
		ID:          uuid.New().String(),
		RoomID:      c.RoomID,
		UserID:      c.UserID,
		Name:        c.Name,
		Description: c.Description,
		JoinedAt:    Now(),
		LastSeenAt:  Now(),
	}
	c.server.storage.AddMember(member)

	members, _ := c.server.storage.GetMembers(c.RoomID)
	messages, _ := c.server.storage.GetMessages(c.RoomID, 100)
	agents, _ := c.server.storage.GetAgents(c.RoomID)

	c.Send(map[string]interface{}{
		"type":     "room_joined",
		"roomId":   c.RoomID,
		"roomName": room.Name,
		"members":  members,
		"messages": messages,
		"agents":   agents,
	})

	// Broadcast member joined
	c.server.broadcastToRoom(c.RoomID, map[string]interface{}{
		"type":     "member_joined",
		"roomId":   c.RoomID,
		"userId":   c.UserID,
		"userName": c.Name,
		"members":  members,
	}, c.ID)
}

// handleDeleteRoom 处理删除房间
func (c *Client) handleDeleteRoom(msg *Message) {
	var data struct {
		RoomID string `json:"roomId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError("Invalid delete room data")
		return
	}

	// Check if user is in the room
	if c.RoomID != data.RoomID {
		c.sendError("Not authorized to delete this room")
		return
	}

	if err := c.server.storage.DeleteRoom(data.RoomID); err != nil {
		log.Printf("Delete room error: %v", err)
		c.sendError("Failed to delete room")
		return
	}

	// Remove compressor
	c.server.mu.Lock()
	delete(c.server.compressors, data.RoomID)
	c.server.mu.Unlock()

	// 广播房间删除
	c.server.broadcastToRoom(data.RoomID, map[string]interface{}{
		"type":   "room_deleted",
		"roomId": data.RoomID,
	})
}

// handleAddAgent 处理添加代理
func (c *Client) handleAddAgent(msg *Message) {
	var data struct {
		RoomID      string `json:"roomId"`
		Profile     string `json:"profile"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError("Invalid add agent data")
		return
	}

	// 检查是否已存在
	exists, _ := c.server.storage.AgentExistsInRoom(data.RoomID, data.Profile)
	if exists {
		c.sendError("Agent already in room")
		return
	}

	agent := &RoomAgent{
		ID:          uuid.New().String(),
		RoomID:      data.RoomID,
		AgentID:     uuid.New().String(),
		Profile:     data.Profile,
		Name:        data.Name,
		Description: data.Description,
		Invited:     1,
		CreatedAt:   Now(),
	}

	if err := c.server.storage.CreateAgent(agent); err != nil {
		log.Printf("Add agent error: %v", err)
		c.sendError("Failed to add agent")
		return
	}

	agents, _ := c.server.storage.GetAgents(data.RoomID)
	c.server.broadcastToRoom(data.RoomID, map[string]interface{}{
		"type":   "agent_added",
		"roomId": data.RoomID,
		"agent":  agent,
		"agents": agents,
	})
}

// handleRemoveAgent 处理移除代理
func (c *Client) handleRemoveAgent(msg *Message) {
	var data struct {
		RoomID  string `json:"roomId"`
		AgentID string `json:"agentId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError("Invalid remove agent data")
		return
	}

	if err := c.server.storage.DeleteAgent(data.AgentID); err != nil {
		log.Printf("Remove agent error: %v", err)
		c.sendError("Failed to remove agent")
		return
	}

	agents, _ := c.server.storage.GetAgents(data.RoomID)
	c.server.broadcastToRoom(data.RoomID, map[string]interface{}{
		"type":    "agent_removed",
		"roomId":  data.RoomID,
		"agentId": data.AgentID,
		"agents":  agents,
	})
}

// handleGetHistory 处理获取历史消息
func (c *Client) handleGetHistory(msg *Message) {
	var data struct {
		RoomID string `json:"roomId"`
		Limit  int    `json:"limit,omitempty"`
		Before int64  `json:"before,omitempty"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError("Invalid get history data")
		return
	}

	if data.Limit <= 0 || data.Limit > 100 {
		data.Limit = 100
	}

	messages, err := c.server.storage.GetMessages(data.RoomID, data.Limit)
	if err != nil {
		log.Printf("Get messages error: %v", err)
		c.sendError("Failed to get messages")
		return
	}

	c.Send(map[string]interface{}{
		"type":     "history",
		"roomId":   data.RoomID,
		"messages": messages,
	})
}

// handleListMembers 处理列出成员
func (c *Client) handleListMembers(msg *Message) {
	var data struct {
		RoomID string `json:"roomId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError("Invalid list members data")
		return
	}

	members, err := c.server.storage.GetMembers(data.RoomID)
	if err != nil {
		log.Printf("Get members error: %v", err)
		c.sendError("Failed to get members")
		return
	}

	// Add online status
	c.server.mu.RLock()
	roomClients, ok := c.server.rooms[data.RoomID]
	c.server.mu.RUnlock()

	if ok {
		onlineMap := make(map[string]bool)
		for _, client := range roomClients {
			onlineMap[client.UserID] = true
		}
		for i := range members {
			members[i].Online = onlineMap[members[i].UserID]
		}
	}

	c.Send(map[string]interface{}{
		"type":    "members",
		"roomId":  data.RoomID,
		"members": members,
	})
}

// handleCompressContext 处理压缩上下文
func (c *Client) handleCompressContext(msg *Message) {
	var data struct {
		RoomID string `json:"roomId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError("Invalid compress context data")
		return
	}

	compressor := c.server.GetOrCreateCompressor(data.RoomID)
	if compressor == nil {
		c.sendError("Failed to get compressor")
		return
	}

	// Trigger compression
	c.server.broadcastToRoom(data.RoomID, map[string]interface{}{
		"type":   "context_compressing",
		"roomId": data.RoomID,
	})

	// Perform actual compression
	messages, err := c.server.storage.GetMessages(data.RoomID, 1000)
	if err == nil && len(messages) > 0 {
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
		compressMgr := compress.NewManager("")
		_, _, compressErr := compressMgr.CompressSession(data.RoomID, compressMsgs, 10)
		if compressErr != nil {
			c.sendError("Compression failed: " + compressErr.Error())
			return
		}
	}

	c.Send(map[string]interface{}{
		"type":       "context_compressed",
		"roomId":     data.RoomID,
		"compressed": true,
	})
}

// handleAgentMention 处理 @ 代理提及
func (s *Server) handleAgentMention(roomID string, msg *ChatMessage) {
	// 提取被 @ 的代理名称
	content := msg.Content
	if len(content) == 0 || content[0] != '@' {
		return
	}

	// 找到空格或换行
	end := 1
	for end < len(content) && content[end] != ' ' && content[end] != '\n' {
		end++
	}
	mentionedName := content[1:end]

	agents, _ := s.storage.GetAgents(roomID)
	for _, agent := range agents {
		if agent.Name == mentionedName || agent.Profile == mentionedName {
			// Broadcast context status
			s.broadcastToRoom(roomID, map[string]interface{}{
				"type":      "context_status",
				"roomId":    roomID,
				"agentName": agent.Name,
				"status":    "processing",
			})

			// Process agent mention asynchronously
			go func(a RoomAgent, userMsg *ChatMessage) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[groupchat] Agent panic recovered: %v", r)
					}
				}()

				// Get recent messages as context
				recentMsgs, err := s.storage.GetMessages(roomID, 20)
				if err != nil || len(recentMsgs) == 0 {
					s.broadcastToRoom(roomID, map[string]interface{}{
						"type":      "agent_response",
						"roomId":    roomID,
						"agentName": a.Name,
						"content":   "No context available.",
					})
					return
				}

				// Build context from recent messages
				var ctxBuilder strings.Builder
				for _, m := range recentMsgs {
					ctxBuilder.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Content))
				}

				// Store agent response as a system message (placeholder - actual processing via REST API)
				response := fmt.Sprintf("Agent %s: I see your message. Processing via the AI service...", a.Name)
				s.storage.SaveMessage(&ChatMessage{
					RoomID:     roomID,
					SenderID:   "agent-" + a.Name,
					SenderName: a.Name,
					Content:    response,
					Type:       "agent",
					Timestamp:  time.Now().Unix(),
				})

				s.broadcastToRoom(roomID, map[string]interface{}{
					"type":      "agent_response",
					"roomId":    roomID,
					"agentName": a.Name,
					"content":   response,
				})
			}(agent, msg)
			break
		}
	}
}

// checkAndCompressContext checks if context compression is needed
func (s *Server) checkAndCompressContext(roomID string) {
	compressor := s.GetOrCreateCompressor(roomID)
	if compressor == nil {
		return
	}

	// Get message count
	messages, err := s.storage.GetMessages(roomID, 1000)
	if err != nil {
		return
	}

	// Token estimation using improved estimator
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += compress.EstimateTokens(msg.Content)
	}

	// Update room token count
	room, _ := s.storage.GetRoom(roomID)
	if room != nil {
		room.TotalTokens = totalTokens
		s.storage.UpdateRoom(room)
	}

	// Check if compression is needed
	if totalTokens > compressor.triggerTokens {
		s.broadcastToRoom(roomID, map[string]interface{}{
			"type":        "context_compression_needed",
			"roomId":      roomID,
			"totalTokens": totalTokens,
			"message":     "Context compression recommended",
		})
	}
}

// removeClient 移除客户端
func (s *Server) removeClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.onlineUsers[c.ID]; ok {
		delete(s.onlineUsers, c.ID)
	}

	if c.RoomID != "" && c.UserID != "" {
		if room, ok := s.rooms[c.RoomID]; ok {
			if _, ok := room[c.ID]; ok {
				delete(room, c.ID)
				// 更新成员最后活跃时间
				s.storage.RemoveMember(c.RoomID, c.UserID)
			}
		}
	}
}

// broadcastToRoom 广播到房间
func (s *Server) broadcastToRoom(roomID string, data interface{}, excludeIDs ...string) {
	s.mu.RLock()
	room, ok := s.rooms[roomID]
	if !ok {
		s.mu.RUnlock()
		return
	}

	excludeSet := make(map[string]bool)
	for _, id := range excludeIDs {
		excludeSet[id] = true
	}

	message, err := json.Marshal(data)
	if err != nil {
		s.mu.RUnlock()
		return
	}

	// Copy clients to avoid holding lock during send
	clients := make([]*Client, 0, len(room))
	for _, client := range room {
		if !excludeSet[client.ID] {
			clients = append(clients, client)
		}
	}
	s.mu.RUnlock()

	for _, client := range clients {
		select {
		case client.send <- message:
		default:
			// Client buffer full, close and remove
			client.mu.Lock()
			if client.Conn != nil {
				client.Conn.Close()
			}
			client.mu.Unlock()
			s.removeClient(client)
		}
	}
}

// sendError sends an error message to the client
func (c *Client) sendError(message string) {
	c.Send(map[string]interface{}{
		"type":    "error",
		"message": message,
	})
}

// Close 关闭服务器
func (s *Server) Close() {
	s.cancel()

	// Close all client connections
	s.mu.Lock()
	for _, client := range s.onlineUsers {
		if client.Conn != nil {
			client.Conn.Close()
		}
	}
	s.mu.Unlock()
}

// GetRoomClients returns all clients in a room
func (s *Server) GetRoomClients(roomID string) []*Client {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room, ok := s.rooms[roomID]
	if !ok {
		return nil
	}

	clients := make([]*Client, 0, len(room))
	for _, client := range room {
		clients = append(clients, client)
	}
	return clients
}

// GetOnlineCount returns the number of online users
func (s *Server) GetOnlineCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.onlineUsers)
}

// GetRoomCount returns the number of active rooms
func (s *Server) GetRoomCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.rooms)
}
