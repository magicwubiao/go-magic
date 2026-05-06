package acp

import (
	"encoding/json"
	"fmt"
)

// Protocol version constants
const (
	ProtocolVersion = "1.0"
	JSONRPCVersion  = "2.0"
)

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface
func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// Error codes for ACP
const (
	ErrCodeParseError       = -32700
	ErrCodeInvalidRequest   = -32600
	ErrCodeMethodNotFound   = -32601
	ErrCodeInvalidParams    = -32602
	ErrCodeInternalError    = -32603
	ErrCodeServerError      = -32000
	ErrCodeUnauthorized     = -32001
	ErrCodeConnectionFailed = -32002
)

// AgentInfo represents information about an agent
type AgentInfo struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Capabilities []string          `json:"capabilities"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// Skill represents an agent skill/capability
type Skill struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	Source      string                 `json:"source,omitempty"`
}

// MemoryItem represents a shared memory entry
type MemoryItem struct {
	ID       string                 `json:"id"`
	Content  string                 `json:"content"`
	Type     string                 `json:"type"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Message represents a message between agents
type Message struct {
	ID        string                 `json:"id"`
	From      string                 `json:"from"`
	To        string                 `json:"to"`
	Content   string                 `json:"content"`
	Type      string                 `json:"type"`
	Timestamp int64                  `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ConnectionRequest is sent when connecting to an agent
type ConnectionRequest struct {
	AgentID      string       `json:"agentId"`
	AgentInfo    AgentInfo    `json:"agentInfo"`
	Capabilities []string     `json:"capabilities"`
	Skills       []Skill     `json:"skills"`
}

// ConnectionResponse is returned after a successful connection
type ConnectionResponse struct {
	Success      bool         `json:"success"`
	AgentInfo    AgentInfo    `json:"agentInfo,omitempty"`
	Capabilities []string     `json:"capabilities,omitempty"`
	Skills       []Skill      `json:"skills,omitempty"`
	Error        string       `json:"error,omitempty"`
}

// ListResponse wraps a list result
type ListResponse struct {
	Items []interface{} `json:"items"`
	Count int           `json:"count"`
}

// SkillCallRequest represents a request to call a skill
type SkillCallRequest struct {
	SkillName string                 `json:"skillName"`
	Params    map[string]interface{} `json:"params,omitempty"`
}

// SkillCallResponse represents the result of a skill call
type SkillCallResponse struct {
	Success bool                   `json:"success"`
	Result  json.RawMessage        `json:"result,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Output  map[string]interface{} `json:"output,omitempty"`
}

// TransportType represents the type of transport
type TransportType string

const (
	TransportStdio TransportType = "stdio"
	TransportHTTP  TransportType = "http"
	TransportSSE   TransportType = "sse"
	TransportTCP   TransportType = "tcp"
)

// TransportConfig holds transport configuration
type TransportConfig struct {
	Type     TransportType `json:"type"`
	Address  string        `json:"address,omitempty"`
	Command  string        `json:"command,omitempty"`
	Args     []string      `json:"args,omitempty"`
	Env      []string      `json:"env,omitempty"`
	BaseURL  string        `json:"baseURL,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
}
