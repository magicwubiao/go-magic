package groupchat

import (
	"database/sql"
	"time"
)

// 数据库 Schema 初始化
const SchemaSQL = `
-- Group Chat Rooms
CREATE TABLE IF NOT EXISTS gc_rooms (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    inviteCode TEXT,
    triggerTokens INTEGER DEFAULT 100000,
    maxHistoryTokens INTEGER DEFAULT 32000,
    tailMessageCount INTEGER DEFAULT 20,
    totalTokens INTEGER DEFAULT 0,
    createdAt INTEGER NOT NULL,
    updatedAt INTEGER NOT NULL
);

-- Room Agents
CREATE TABLE IF NOT EXISTS gc_room_agents (
    id TEXT PRIMARY KEY,
    roomId TEXT NOT NULL,
    agentId TEXT NOT NULL,
    profile TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    invited INTEGER DEFAULT 0,
    sessionId TEXT,
    createdAt INTEGER NOT NULL,
    FOREIGN KEY (roomId) REFERENCES gc_rooms(id) ON DELETE CASCADE
);

-- Room Members (Users)
CREATE TABLE IF NOT EXISTS gc_room_members (
    id TEXT PRIMARY KEY,
    roomId TEXT NOT NULL,
    userId TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    joinedAt INTEGER NOT NULL,
    lastSeenAt INTEGER NOT NULL,
    FOREIGN KEY (roomId) REFERENCES gc_rooms(id) ON DELETE CASCADE,
    UNIQUE(roomId, userId)
);

-- Chat Messages
CREATE TABLE IF NOT EXISTS gc_messages (
    id TEXT PRIMARY KEY,
    roomId TEXT NOT NULL,
    senderId TEXT NOT NULL,
    senderName TEXT NOT NULL,
    content TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    FOREIGN KEY (roomId) REFERENCES gc_rooms(id) ON DELETE CASCADE
);

-- Session Profiles (links agent sessions to rooms)
CREATE TABLE IF NOT EXISTS gc_session_profiles (
    session_id TEXT PRIMARY KEY,
    room_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    profile_name TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

-- Pending Session Deletes
CREATE TABLE IF NOT EXISTS gc_pending_session_deletes (
    session_id TEXT PRIMARY KEY,
    profile_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    next_attempt_at INTEGER NOT NULL
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_gc_messages_room ON gc_messages(roomId, timestamp);
CREATE INDEX IF NOT EXISTS idx_gc_room_agents_room ON gc_room_agents(roomId);
CREATE UNIQUE INDEX IF NOT EXISTS idx_gc_room_members_unique ON gc_room_members(roomId, userId);
CREATE INDEX IF NOT EXISTS idx_gc_pending_session_deletes_profile ON gc_pending_session_deletes(profile_name, status, next_attempt_at, created_at);
CREATE INDEX IF NOT EXISTS idx_gc_session_profiles_profile ON gc_session_profiles(profile_name, created_at);
`

// InitSchema 初始化数据库表
func InitSchema(db *sql.DB) error {
	_, err := db.Exec(SchemaSQL)
	return err
}

// Room 房间
type Room struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	InviteCode       string `json:"inviteCode,omitempty"`
	TriggerTokens    int    `json:"triggerTokens,omitempty"`
	MaxHistoryTokens int    `json:"maxHistoryTokens,omitempty"`
	TailMessageCount int    `json:"tailMessageCount,omitempty"`
	TotalTokens      int    `json:"totalTokens,omitempty"`
	CreatedAt        int64  `json:"createdAt"`
	UpdatedAt        int64  `json:"updatedAt"`
}

// RoomAgent 房间代理
type RoomAgent struct {
	ID          string `json:"id"`
	RoomID      string `json:"roomId"`
	AgentID     string `json:"agentId"`
	Profile     string `json:"profile"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Invited     int    `json:"invited"`
	SessionID   string `json:"sessionId,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
}

// Member 房间成员
type Member struct {
	ID          string `json:"id"`
	RoomID      string `json:"roomId"`
	UserID      string `json:"userId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	JoinedAt    int64  `json:"joinedAt"`
	LastSeenAt  int64  `json:"lastSeenAt"`
	Online      bool   `json:"online,omitempty"`
	SocketID    string `json:"socketId,omitempty"`
}

// ChatMessage 聊天消息
type ChatMessage struct {
	ID         string `json:"id"`
	RoomID     string `json:"roomId"`
	SenderID   string `json:"senderId"`
	SenderName string `json:"senderName"`
	Content    string `json:"content"`
	Timestamp  int64  `json:"timestamp"`
}

// CompressionConfig 压缩配置
type CompressionConfig struct {
	TriggerTokens    int `json:"triggerTokens"`
	MaxHistoryTokens int `json:"maxHistoryTokens"`
	TailMessageCount int `json:"tailMessageCount"`
}

// DefaultCompressionConfig 默认压缩配置
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		TriggerTokens:    100000,
		MaxHistoryTokens:  32000,
		TailMessageCount: 20,
	}
}

// Now 当前时间戳
func Now() int64 {
	return time.Now().UnixMilli()
}
