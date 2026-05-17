package acp

// Server represents an ACP server (alias for backward compatibility)
type ServerStub = Server

// NewServer creates a new ACP server (alias for backward compatibility)
func NewACPServer(agentID string, info AgentInfo) *Server {
	return NewServer(agentID, info)
}

// HandlerFunc is the function signature for ACP method handlers
// (documented in server.go)
type Handler = HandlerFunc

// MemoryProvider is the interface for memory providers
// (documented in server.go)
type MemoryProviderInterface = MemoryProvider
