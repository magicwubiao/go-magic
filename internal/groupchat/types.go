package groupchat

import (
	"fmt"
	"time"
)

// Room represents a group chat room
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

// RoomAgent represents an AI agent in a room
type RoomAgent struct {
	ID           string  `json:"id"`
	RoomID       string  `json:"roomId"`
	AgentID      string  `json:"agentId"`
	Profile      string  `json:"profile"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	SystemPrompt string  `json:"systemPrompt,omitempty"`
	Temperature  float64 `json:"temperature,omitempty"`
	Tools        string  `json:"tools,omitempty"` // JSON array of tool names, empty means all
	Invited      int     `json:"invited"`
	SessionID    string  `json:"sessionId,omitempty"`
	CreatedAt    int64   `json:"createdAt"`
}

// Member represents a user in a room
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

// ChatMessage represents a message in the chat
type ChatMessage struct {
	ID         string `json:"id"`
	RoomID     string `json:"roomId"`
	SenderID   string `json:"senderId"`
	SenderName string `json:"senderName"`
	Content    string `json:"content"`
	Timestamp  int64  `json:"timestamp"`
	Type       string `json:"type,omitempty"` // text, system, agent
}

// CompressionConfig holds context compression settings
type CompressionConfig struct {
	TriggerTokens    int `json:"triggerTokens"`
	MaxHistoryTokens int `json:"maxHistoryTokens"`
	TailMessageCount int `json:"tailMessageCount"`
}

// DefaultCompressionConfig returns default compression settings
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		TriggerTokens:    100000,
		MaxHistoryTokens: 32000,
		TailMessageCount: 20,
	}
}

// Now returns current timestamp in milliseconds
func Now() int64 {
	return time.Now().UnixMilli()
}

// GenerateInviteCode generates a random 6-digit invite code
func GenerateInviteCode() string {
	return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
}

// RoomInfo contains room information with members and messages
type RoomInfo struct {
	Room     *Room         `json:"room"`
	Members  []Member      `json:"members"`
	Agents   []RoomAgent   `json:"agents"`
	Messages []ChatMessage `json:"messages"`
}

// MessageEvent represents a message event for WebSocket
type MessageEvent struct {
	Type      string      `json:"type"`
	RoomID    string      `json:"roomId,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// JoinRequest represents a request to join a room
type JoinRequest struct {
	UserID      string `json:"userId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RoomID      string `json:"roomId"`
	InviteCode  string `json:"inviteCode,omitempty"`
}

// CreateRoomRequest represents a request to create a room
type CreateRoomRequest struct {
	Name        string            `json:"name"`
	InviteCode  string            `json:"inviteCode,omitempty"`
	Compression CompressionConfig `json:"compression,omitempty"`
}

// SendMessageRequest represents a request to send a message
type SendMessageRequest struct {
	RoomID      string `json:"roomId"`
	Content     string `json:"content"`
	MessageType string `json:"type,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// SuccessResponse represents a success response
type SuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
}
