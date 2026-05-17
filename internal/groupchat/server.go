package groupchat

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Server Group Chat WebSocket 服务器
type Server struct {
	storage      *Storage
	upgrader     websocket.Upgrader
	rooms        map[string]map[string]*Client // roomID -> clientID -> client
	roomAgents   map[string]map[string]*AgentClient // roomID -> agentID -> agent
	onlineUsers  map[string]*Client // socketID -> client
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
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
}

// AgentClient Agent 客户端
type AgentClient struct {
	ID        string    `json:"id"`
	RoomID    string    `json:"roomId"`
	Profile   string    `json:"profile"`
	Name      string    `json:"name"`
	SessionID string    `json:"sessionId"`
	Connected bool      `json:"connected"`
}

// Message 消息结构
type Message struct {
	Type      string          `json:"type"`
	RoomID    string          `json:"roomId,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp int64           `json:"timestamp"`
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
	}
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
			"type":    "member_joined",
			"roomId":  c.RoomID,
			"members": members,
		})
	}
	c.server.mu.Unlock()

	// 发送确认
	c.send <- []byte(`{"type":"joined","userId":"` + c.UserID + `"}`)
}

// handleLeave 处理离开
func (c *Client) handleLeave(msg *Message) {
	c.server.removeClient(c)

	if c.RoomID != "" {
		c.server.broadcastToRoom(c.RoomID, map[string]interface{}{
			"type":   "member_left",
			"roomId": c.RoomID,
			"userId": c.UserID,
		})
	}
}

// handleChatMessage 处理聊天消息
func (c *Client) handleChatMessage(msg *Message) {
	var data struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	chatMsg := &ChatMessage{
		ID:         uuid.New().String(),
		RoomID:     c.RoomID,
		SenderID:   c.UserID,
		SenderName: c.Name,
		Content:    data.Content,
		Timestamp:  Now(),
	}

	// 保存消息
	c.server.storage.SaveMessage(chatMsg)

	// 广播消息
	c.server.broadcastToRoom(c.RoomID, map[string]interface{}{
		"type": "message",
		"data": chatMsg,
	})

	// 如果是 @ 某个代理，转发给代理处理
	if len(data.Content) > 0 && data.Content[0] == '@' {
		c.server.handleAgentMention(c.RoomID, chatMsg)
	}
}

// handleTyping 处理打字
func (c *Client) handleTyping(msg *Message) {
	c.server.broadcastToRoom(c.RoomID, map[string]interface{}{
		"type":    "typing",
		"roomId":  c.RoomID,
		"userId":  c.UserID,
		"userName": c.Name,
	}, c.ID)
}

// handleStopTyping 处理停止打字
func (c *Client) handleStopTyping(msg *Message) {
	c.server.broadcastToRoom(c.RoomID, map[string]interface{}{
		"type":   "stop_typing",
		"roomId": c.RoomID,
		"userId": c.UserID,
	}, c.ID)
}

// handleCreateRoom 处理创建房间
func (c *Client) handleCreateRoom(msg *Message) {
	var data struct {
		Name        string             `json:"name"`
		InviteCode  string             `json:"inviteCode"`
		Compression CompressionConfig   `json:"compression"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
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
		return
	}

	// 发送房间信息
	c.send <- mustMarshal(map[string]interface{}{
		"type": "room_created",
		"room": room,
	})
}

// handleJoinRoom 处理加入房间
func (c *Client) handleJoinRoom(msg *Message) {
	var data struct {
		RoomID string `json:"roomId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	room, err := c.server.storage.GetRoom(data.RoomID)
	if err != nil || room == nil {
		return
	}

	members, _ := c.server.storage.GetMembers(data.RoomID)
	messages, _ := c.server.storage.GetMessages(data.RoomID, 100)
	agents, _ := c.server.storage.GetAgents(data.RoomID)

	c.send <- mustMarshal(map[string]interface{}{
		"type":     "room_joined",
		"roomId":   data.RoomID,
		"roomName": room.Name,
		"members":  members,
		"messages": messages,
		"agents":   agents,
	})
}

// handleDeleteRoom 处理删除房间
func (c *Client) handleDeleteRoom(msg *Message) {
	var data struct {
		RoomID string `json:"roomId"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	if err := c.server.storage.DeleteRoom(data.RoomID); err != nil {
		log.Printf("Delete room error: %v", err)
		return
	}

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
		return
	}

	// 检查是否已存在
	exists, _ := c.server.storage.AgentExistsInRoom(data.RoomID, data.Profile)
	if exists {
		c.send <- []byte(`{"type":"error","message":"Agent already in room"}`)
		return
	}

	agent := &RoomAgent{
		ID:          uuid.New().String(),
		RoomID:      data.RoomID,
		AgentID:    uuid.New().String(),
		Profile:     data.Profile,
		Name:        data.Name,
		Description: data.Description,
		Invited:     1,
		CreatedAt:   Now(),
	}

	if err := c.server.storage.CreateAgent(agent); err != nil {
		log.Printf("Add agent error: %v", err)
		return
	}

	agents, _ := c.server.storage.GetAgents(data.RoomID)
	c.server.broadcastToRoom(data.RoomID, map[string]interface{}{
		"type":   "agent_added",
		"roomId": data.RoomID,
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
		return
	}

	if err := c.server.storage.DeleteAgent(data.AgentID); err != nil {
		log.Printf("Remove agent error: %v", err)
		return
	}

	agents, _ := c.server.storage.GetAgents(data.RoomID)
	c.server.broadcastToRoom(data.RoomID, map[string]interface{}{
		"type":   "agent_removed",
		"roomId": data.RoomID,
		"agents": agents,
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
			// 广播上下文状态
			s.broadcastToRoom(roomID, map[string]interface{}{
				"type":       "context_status",
				"roomId":     roomID,
				"agentName":  agent.Name,
				"status":     "compressing",
			})

			// TODO: 调用 Agent 处理消息
			// 这里可以调用 go-magic 的 Agent 来处理
			_ = agent
			break
		}
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

	message := mustMarshal(data)
	for _, client := range room {
		if excludeSet[client.ID] {
			continue
		}
		select {
		case client.send <- message:
		default:
			close(client.send)
			delete(room, client.ID)
		}
	}
	s.mu.RUnlock()
}

// Close 关闭服务器
func (s *Server) Close() {
	s.cancel()
}

// mustMarshal JSON 序列化
func mustMarshal(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"type":"error","message":"marshal error"}`)
	}
	return data
}
