package groupchat

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Storage 数据存储
type Storage struct {
	db      *sql.DB
	homeDir string
}

// NewStorage 创建存储实例
func NewStorage(db *sql.DB) *Storage {
	return &Storage{db: db}
}

// NewStorageFromHome creates a new storage instance from a home directory path
func NewStorageFromHome(homeDir string) (*Storage, error) {
	if homeDir == "" {
		homeDir = "~/.magic"
	}
	homeDir = os.ExpandEnv(homeDir)

	// Ensure directory exists
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(homeDir, "groupchat.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	// Initialize schema
	if err := InitSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Storage{db: db, homeDir: homeDir}, nil
}

// GetRoom 获取房间
func (s *Storage) GetRoom(roomID string) (*Room, error) {
	row := s.db.QueryRow(`
		SELECT id, name, inviteCode, triggerTokens, maxHistoryTokens, tailMessageCount, totalTokens, createdAt, updatedAt
		FROM gc_rooms WHERE id = ?`, roomID)

	var room Room
	var inviteCode sql.NullString
	err := row.Scan(&room.ID, &room.Name, &inviteCode, &room.TriggerTokens, &room.MaxHistoryTokens,
		&room.TailMessageCount, &room.TotalTokens, &room.CreatedAt, &room.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if inviteCode.Valid {
		room.InviteCode = inviteCode.String
	}
	return &room, nil
}

// GetRoomByInviteCode 通过邀请码获取房间
func (s *Storage) GetRoomByInviteCode(code string) (*Room, error) {
	row := s.db.QueryRow(`
		SELECT id, name, inviteCode, triggerTokens, maxHistoryTokens, tailMessageCount, totalTokens, createdAt, updatedAt
		FROM gc_rooms WHERE inviteCode = ?`, code)

	var room Room
	var inviteCode sql.NullString
	err := row.Scan(&room.ID, &room.Name, &inviteCode, &room.TriggerTokens, &room.MaxHistoryTokens,
		&room.TailMessageCount, &room.TotalTokens, &room.CreatedAt, &room.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if inviteCode.Valid {
		room.InviteCode = inviteCode.String
	}
	return &room, nil
}

// GetAllRooms 获取所有房间
func (s *Storage) GetAllRooms() ([]Room, error) {
	rows, err := s.db.Query(`
		SELECT id, name, inviteCode, triggerTokens, maxHistoryTokens, tailMessageCount, totalTokens, createdAt, updatedAt
		FROM gc_rooms ORDER BY createdAt DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []Room
	for rows.Next() {
		var room Room
		var inviteCode sql.NullString
		if err := rows.Scan(&room.ID, &room.Name, &inviteCode, &room.TriggerTokens, &room.MaxHistoryTokens,
			&room.TailMessageCount, &room.TotalTokens, &room.CreatedAt, &room.UpdatedAt); err != nil {
			return nil, err
		}
		if inviteCode.Valid {
			room.InviteCode = inviteCode.String
		}
		rooms = append(rooms, room)
	}
	return rooms, nil
}

// CreateRoom 创建房间
func (s *Storage) CreateRoom(room *Room) error {
	_, err := s.db.Exec(`
		INSERT INTO gc_rooms (id, name, inviteCode, triggerTokens, maxHistoryTokens, tailMessageCount, totalTokens, createdAt, updatedAt)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		room.ID, room.Name, room.InviteCode, room.TriggerTokens, room.MaxHistoryTokens,
		room.TailMessageCount, room.TotalTokens, room.CreatedAt, room.UpdatedAt)
	return err
}

// UpdateRoom 更新房间
func (s *Storage) UpdateRoom(room *Room) error {
	room.UpdatedAt = Now()
	_, err := s.db.Exec(`
		UPDATE gc_rooms SET name = ?, inviteCode = ?, triggerTokens = ?, maxHistoryTokens = ?, 
		tailMessageCount = ?, totalTokens = ?, updatedAt = ? WHERE id = ?`,
		room.Name, room.InviteCode, room.TriggerTokens, room.MaxHistoryTokens,
		room.TailMessageCount, room.TotalTokens, room.UpdatedAt, room.ID)
	return err
}

// UpdateRoomConfig 更新房间配置
func (s *Storage) UpdateRoomConfig(roomID string, config CompressionConfig) error {
	_, err := s.db.Exec(`
		UPDATE gc_rooms SET triggerTokens = ?, maxHistoryTokens = ?, tailMessageCount = ?, updatedAt = ? WHERE id = ?`,
		config.TriggerTokens, config.MaxHistoryTokens, config.TailMessageCount, Now(), roomID)
	return err
}

// DeleteRoom 删除房间
func (s *Storage) DeleteRoom(roomID string) error {
	_, err := s.db.Exec("DELETE FROM gc_rooms WHERE id = ?", roomID)
	return err
}

// GetAgents 获取房间代理
func (s *Storage) GetAgents(roomID string) ([]RoomAgent, error) {
	rows, err := s.db.Query(`
		SELECT id, roomId, agentId, profile, name, description, systemPrompt, temperature, tools, invited, sessionId, createdAt
		FROM gc_room_agents WHERE roomId = ?`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []RoomAgent
	for rows.Next() {
		var agent RoomAgent
		var sessionID sql.NullString
		if err := rows.Scan(&agent.ID, &agent.RoomID, &agent.AgentID, &agent.Profile, &agent.Name,
			&agent.Description, &agent.SystemPrompt, &agent.Temperature, &agent.Tools,
			&agent.Invited, &sessionID, &agent.CreatedAt); err != nil {
			return nil, err
		}
		if sessionID.Valid {
			agent.SessionID = sessionID.String
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

// GetAgent 获取代理
func (s *Storage) GetAgent(agentID string) (*RoomAgent, error) {
	row := s.db.QueryRow(`
		SELECT id, roomId, agentId, profile, name, description, systemPrompt, temperature, tools, invited, sessionId, createdAt
		FROM gc_room_agents WHERE id = ?`, agentID)

	var agent RoomAgent
	var sessionID sql.NullString
	err := row.Scan(&agent.ID, &agent.RoomID, &agent.AgentID, &agent.Profile, &agent.Name,
		&agent.Description, &agent.SystemPrompt, &agent.Temperature, &agent.Tools,
		&agent.Invited, &sessionID, &agent.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if sessionID.Valid {
		agent.SessionID = sessionID.String
	}
	return &agent, nil
}

// CreateAgent 创建代理
func (s *Storage) CreateAgent(agent *RoomAgent) error {
	_, err := s.db.Exec(`
		INSERT INTO gc_room_agents (id, roomId, agentId, profile, name, description, systemPrompt, temperature, tools, invited, sessionId, createdAt)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		agent.ID, agent.RoomID, agent.AgentID, agent.Profile, agent.Name, agent.Description,
		agent.SystemPrompt, agent.Temperature, agent.Tools,
		agent.Invited, agent.SessionID, agent.CreatedAt)
	return err
}

// UpdateAgent 更新代理
func (s *Storage) UpdateAgent(agent *RoomAgent) error {
	_, err := s.db.Exec(`
		UPDATE gc_room_agents SET profile = ?, name = ?, description = ?, systemPrompt = ?, temperature = ?, tools = ?, sessionId = ? WHERE id = ?`,
		agent.Profile, agent.Name, agent.Description, agent.SystemPrompt, agent.Temperature, agent.Tools, agent.SessionID, agent.ID)
	return err
}

// DeleteAgent 删除代理
func (s *Storage) DeleteAgent(agentID string) error {
	_, err := s.db.Exec("DELETE FROM gc_room_agents WHERE id = ?", agentID)
	return err
}

// GetMembers 获取房间成员
func (s *Storage) GetMembers(roomID string) ([]Member, error) {
	rows, err := s.db.Query(`
		SELECT id, roomId, userId, name, description, joinedAt, lastSeenAt
		FROM gc_room_members WHERE roomId = ?`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var member Member
		if err := rows.Scan(&member.ID, &member.RoomID, &member.UserID, &member.Name,
			&member.Description, &member.JoinedAt, &member.LastSeenAt); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, nil
}

// AddMember 添加成员
func (s *Storage) AddMember(member *Member) error {
	_, err := s.db.Exec(`
		INSERT INTO gc_room_members (id, roomId, userId, name, description, joinedAt, lastSeenAt)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(roomId, userId) DO UPDATE SET name = excluded.name, description = excluded.description, lastSeenAt = excluded.lastSeenAt`,
		member.ID, member.RoomID, member.UserID, member.Name, member.Description, member.JoinedAt, member.LastSeenAt)
	return err
}

// UpdateMember 更新成员
func (s *Storage) UpdateMember(member *Member) error {
	_, err := s.db.Exec(`
		UPDATE gc_room_members SET name = ?, description = ?, lastSeenAt = ? WHERE id = ?`,
		member.Name, member.Description, member.LastSeenAt, member.ID)
	return err
}

// RemoveMember 移除成员
func (s *Storage) RemoveMember(roomID, memberID string) error {
	_, err := s.db.Exec("DELETE FROM gc_room_members WHERE roomId = ? AND id = ?", roomID, memberID)
	return err
}

// GetMessages 获取消息
func (s *Storage) GetMessages(roomID string, limit int) ([]ChatMessage, error) {
	rows, err := s.db.Query(`
		SELECT id, roomId, senderId, senderName, content, timestamp, type
		FROM gc_messages WHERE roomId = ? ORDER BY timestamp DESC LIMIT ?`, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []ChatMessage
	for rows.Next() {
		var msg ChatMessage
		var msgType sql.NullString
		if err := rows.Scan(&msg.ID, &msg.RoomID, &msg.SenderID, &msg.SenderName, &msg.Content, &msg.Timestamp, &msgType); err != nil {
			return nil, err
		}
		if msgType.Valid {
			msg.Type = msgType.String
		} else {
			msg.Type = "text"
		}
		messages = append(messages, msg)
	}

	// 反转使最早的消息在前
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// SaveRoom saves a room (wrapper for CreateRoom)
func (s *Storage) SaveRoom(room *Room) error {
	return s.CreateRoom(room)
}

// ListRooms returns all rooms
func (s *Storage) ListRooms() ([]Room, error) {
	return s.GetAllRooms()
}

// RoomInfo represents a chat room for JSON API
type RoomInfoAPI struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []string `json:"members"`
	AgentIDs    []string `json:"agent_ids"`
	CreatedAt   int64    `json:"created_at"`
}

// MessageInfo represents a chat message for JSON API
type MessageInfoAPI struct {
	ID        string `json:"id"`
	RoomID    string `json:"room_id"`
	Sender    string `json:"sender"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// SaveMessage 保存消息
func (s *Storage) SaveMessage(msg *ChatMessage) error {
	_, err := s.db.Exec(`
		INSERT INTO gc_messages (id, roomId, senderId, senderName, content, timestamp, type)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.RoomID, msg.SenderID, msg.SenderName, msg.Content, msg.Timestamp, msg.Type)
	return err
}

// UpdateRoomTokens 更新房间 token 数
func (s *Storage) UpdateRoomTokens(roomID string, totalTokens int) error {
	_, err := s.db.Exec("UPDATE gc_rooms SET totalTokens = ?, updatedAt = ? WHERE id = ?", totalTokens, Now(), roomID)
	return err
}

// GetSessionProfile 获取会话配置
func (s *Storage) GetSessionProfile(sessionID string) (roomID, agentID, profileName string, err error) {
	row := s.db.QueryRow("SELECT room_id, agent_id, profile_name FROM gc_session_profiles WHERE session_id = ?", sessionID)
	err = row.Scan(&roomID, &agentID, &profileName)
	if err == sql.ErrNoRows {
		return "", "", "", nil
	}
	return
}

// SaveSessionProfile 保存会话配置
func (s *Storage) SaveSessionProfile(sessionID, roomID, agentID, profileName string) error {
	_, err := s.db.Exec(`
		INSERT INTO gc_session_profiles (session_id, room_id, agent_id, profile_name, created_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(session_id) DO UPDATE SET room_id = excluded.room_id, agent_id = excluded.agent_id, profile_name = excluded.profile_name`,
		sessionID, roomID, agentID, profileName, Now())
	return err
}

// DeleteSessionProfile 删除会话配置
func (s *Storage) DeleteSessionProfile(sessionID string) error {
	_, err := s.db.Exec("DELETE FROM gc_session_profiles WHERE session_id = ?", sessionID)
	return err
}

// AgentExistsInRoom 检查代理是否在房间
func (s *Storage) AgentExistsInRoom(roomID, profile string) (bool, error) {
	row := s.db.QueryRow("SELECT COUNT(*) FROM gc_room_agents WHERE roomId = ? AND profile = ?", roomID, profile)
	var count int
	err := row.Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetMessageCount 获取房间消息数量
func (s *Storage) GetMessageCount(roomID string) (int, error) {
	row := s.db.QueryRow("SELECT COUNT(*) FROM gc_messages WHERE roomId = ?", roomID)
	var count int
	err := row.Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// DeleteMessagesBefore 删除指定时间之前的消息
func (s *Storage) DeleteMessagesBefore(roomID string, timestamp int64) error {
	_, err := s.db.Exec("DELETE FROM gc_messages WHERE roomId = ? AND timestamp < ?", roomID, timestamp)
	return err
}

// GetRecentMessages 获取最近的消息（用于压缩后保留）
func (s *Storage) GetRecentMessages(roomID string, count int) ([]ChatMessage, error) {
	rows, err := s.db.Query(`
		SELECT id, roomId, senderId, senderName, content, timestamp, type
		FROM gc_messages WHERE roomId = ? ORDER BY timestamp DESC LIMIT ?`, roomID, count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []ChatMessage
	for rows.Next() {
		var msg ChatMessage
		var msgType sql.NullString
		if err := rows.Scan(&msg.ID, &msg.RoomID, &msg.SenderID, &msg.SenderName, &msg.Content, &msg.Timestamp, &msgType); err != nil {
			return nil, err
		}
		if msgType.Valid {
			msg.Type = msgType.String
		} else {
			msg.Type = "text"
		}
		messages = append(messages, msg)
	}

	// 反转使最早的消息在前
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}
