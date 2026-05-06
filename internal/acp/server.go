package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// HandlerFunc is the function signature for ACP method handlers
type HandlerFunc func(ctx context.Context, params json.RawMessage) (interface{}, error)

// MemoryProvider interface for providing memory to other agents
type MemoryProvider interface {
	GetMemory(ctx context.Context, query string) ([]MemoryItem, error)
	ShareMemory(ctx context.Context, item MemoryItem) error
}

// Server represents an ACP server that exposes agent capabilities
type Server struct {
	agentID   string
	agentInfo AgentInfo
	transport Transport
	skills    map[string]Skill
	handlers  map[string]HandlerFunc
	mu        sync.RWMutex
	running   bool
	stopCh    chan struct{}
}

// NewServer creates a new ACP server
func NewServer(agentID string, info AgentInfo) *Server {
	if info.ID == "" {
		info.ID = agentID
	}
	return &Server{
		agentID:   agentID,
		agentInfo: info,
		skills:    make(map[string]Skill),
		handlers:  make(map[string]HandlerFunc),
		stopCh:    make(chan struct{}),
	}
}

// NewServerWithTransport creates a new ACP server with a specific transport
func NewServerWithTransport(agentID string, info AgentInfo, transport Transport) *Server {
	server := NewServer(agentID, info)
	server.transport = transport
	return server
}

// RegisterSkill registers a skill with the server
func (s *Server) RegisterSkill(skill Skill, handler HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.skills[skill.Name] = skill
	s.handlers["skill/"+skill.Name] = handler
}

// RegisterHandler registers a custom method handler
func (s *Server) RegisterHandler(method string, handler HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.handlers[method] = handler
}

// Start starts the ACP server
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.running = true
	s.mu.Unlock()

	// Register built-in handlers
	s.registerBuiltinHandlers()

	// If we have a transport, start the request loop
	if s.transport != nil {
		go s.requestLoop(ctx)
	}

	return nil
}

// Stop stops the ACP server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	close(s.stopCh)

	if s.transport != nil {
		return s.transport.Close()
	}

	return nil
}

// requestLoop handles incoming requests
func (s *Server) requestLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
			if s.transport == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			req, err := s.transport.Receive(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				time.Sleep(100 * time.Millisecond)
				continue
			}

			go s.handleRequest(ctx, req)
		}
	}
}

// handleRequest handles a single request
func (s *Server) handleRequest(ctx context.Context, req *JSONRPCRequest) {
	var result interface{}
	var err error

	s.mu.RLock()
	handler, exists := s.handlers[req.Method]
	s.mu.RUnlock()

	if !exists {
		err = &JSONRPCError{
			Code:    ErrCodeMethodNotFound,
			Message: fmt.Sprintf("method not found: %s", req.Method),
		}
	} else {
		result, err = handler(ctx, req.Params)
	}

	var resp JSONRPCResponse
	resp.JSONRPC = JSONRPCVersion
	resp.ID = req.ID

	if err != nil {
		if rpcErr, ok := err.(*JSONRPCError); ok {
			resp.Error = rpcErr
		} else {
			resp.Error = &JSONRPCError{
				Code:    ErrCodeServerError,
				Message: err.Error(),
			}
		}
	} else {
		resp.Result, _ = json.Marshal(result)
	}

	// Send response if we have a transport
	if s.transport != nil {
		if err := s.transport.Send(ctx, &resp); err != nil {
			fmt.Printf("Failed to send response: %v\n", err)
		}
	}
}

// registerBuiltinHandlers registers built-in method handlers
func (s *Server) registerBuiltinHandlers() {
	// List available skills
	s.RegisterHandler("skill/list", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()

		skills := make([]Skill, 0, len(s.skills))
		for _, skill := range s.skills {
			skills = append(skills, skill)
		}
		return ListResponse{Items: toInterfaceSlice(skills), Count: len(skills)}, nil
	})

	// Get agent info
	s.RegisterHandler("agent/info", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()

		return s.agentInfo, nil
	})

	// Call a skill
	s.RegisterHandler("skill/call", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var req SkillCallRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &JSONRPCError{
				Code:    ErrCodeInvalidParams,
				Message: fmt.Sprintf("invalid params: %v", err),
			}
		}

		s.mu.RLock()
		handler, exists := s.handlers["skill/"+req.SkillName]
		_, hasSkill := s.skills[req.SkillName]
		s.mu.RUnlock()

		if !exists && !hasSkill {
			return nil, &JSONRPCError{
				Code:    ErrCodeMethodNotFound,
				Message: fmt.Sprintf("skill not found: %s", req.SkillName),
			}
		}

		result, err := handler(ctx, params)
		if err != nil {
			return SkillCallResponse{Success: false, Error: err.Error()}, nil
		}

		return SkillCallResponse{Success: true, Result: mustMarshal(result)}, nil
	})

	// Ping
	s.RegisterHandler("ping", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return map[string]interface{}{
			"pong":    true,
			"agentID": s.agentID,
			"version": ProtocolVersion,
		}, nil
	})

	// Connect/handshake
	s.RegisterHandler("connect", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var req ConnectionRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &JSONRPCError{
				Code:    ErrCodeInvalidParams,
				Message: fmt.Sprintf("invalid params: %v", err),
			}
		}

		s.mu.RLock()
		skills := make([]Skill, 0, len(s.skills))
		for _, skill := range s.skills {
			skills = append(skills, skill)
		}
		s.mu.RUnlock()

		return ConnectionResponse{
			Success:      true,
			AgentInfo:    s.agentInfo,
			Capabilities: s.agentInfo.Capabilities,
			Skills:       skills,
		}, nil
	})
}

// GetAgentInfo returns the agent info
func (s *Server) GetAgentInfo() AgentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agentInfo
}

// ListSkills returns all registered skills
func (s *Server) ListSkills() []Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()

	skills := make([]Skill, 0, len(s.skills))
	for _, skill := range s.skills {
		skills = append(skills, skill)
	}
	return skills
}

// IsRunning returns whether the server is running
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// toInterfaceSlice converts a slice of any type to []interface{}
func toInterfaceSlice[T any](slice []T) []interface{} {
	result := make([]interface{}, len(slice))
	for i, v := range slice {
		result[i] = v
	}
	return result
}

// GenerateRequestID generates a new request ID
func GenerateRequestID() string {
	return uuid.New().String()
}

// NewJSONRPCRequest creates a new JSON-RPC request
func NewJSONRPCRequest(method string, params interface{}, id interface{}) *JSONRPCRequest {
	var rawParams json.RawMessage
	if params != nil {
		rawParams = mustMarshal(params)
	}

	return &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  rawParams,
		ID:      id,
	}
}

// NewJSONRPCResponse creates a new JSON-RPC response
func NewJSONRPCResponse(id interface{}, result interface{}) *JSONRPCResponse {
	var rawResult json.RawMessage
	if result != nil {
		rawResult = mustMarshal(result)
	}

	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		Result:  rawResult,
		ID:      id,
	}
}

// NewJSONRPCError creates a new JSON-RPC error response
func NewJSONRPCError(id interface{}, code int, message string) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
		},
		ID: id,
	}
}
