package acp

import (
	"context"
	"fmt"
	"sync"
)

// Manager manages multiple ACP connections (servers and clients)
type Manager struct {
	clients map[string]*Client
	servers map[string]*Server
	mu      sync.RWMutex
}

// NewManager creates a new ACP manager
func NewManager() *Manager {
	return &Manager{
		clients: make(map[string]*Client),
		servers: make(map[string]*Server),
	}
}

// ConnectStdio connects to an ACP agent using stdio transport
func (m *Manager) ConnectStdio(name string, agentID string, command string, args, env []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[name]; exists {
		return fmt.Errorf("agent '%s' already connected", name)
	}

	transport, err := NewStdioTransport(command, args, env)
	if err != nil {
		return fmt.Errorf("failed to create stdio transport: %w", err)
	}

	client := NewClient(agentID, transport)
	ctx := context.Background()
	info, err := client.Connect(ctx)
	if err != nil {
		transport.Close()
		return fmt.Errorf("failed to connect: %w", err)
	}

	m.clients[name] = client
	_ = info // info is stored in client

	return nil
}

// ConnectHTTP connects to an ACP agent using HTTP transport
func (m *Manager) ConnectHTTP(name string, agentID string, baseURL string, headers map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[name]; exists {
		return fmt.Errorf("agent '%s' already connected", name)
	}

	transport := NewHTTPTransport(baseURL, headers)
	client := NewClient(agentID, transport)
	ctx := context.Background()
	info, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	m.clients[name] = client
	_ = info // info is stored in client

	return nil
}

// ConnectSSE connects to an ACP agent using SSE transport
func (m *Manager) ConnectSSE(name string, agentID string, url string, headers map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[name]; exists {
		return fmt.Errorf("agent '%s' already connected", name)
	}

	transport, err := NewSSETransport(url, headers)
	if err != nil {
		return fmt.Errorf("failed to create SSE transport: %w", err)
	}

	client := NewClient(agentID, transport)
	ctx := context.Background()
	info, err := client.Connect(ctx)
	if err != nil {
		transport.Close()
		return fmt.Errorf("failed to connect: %w", err)
	}

	m.clients[name] = client
	_ = info // info is stored in client

	return nil
}

// Connect connects using a pre-configured transport
func (m *Manager) Connect(name string, agentID string, transport Transport) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[name]; exists {
		return fmt.Errorf("agent '%s' already connected", name)
	}

	client := NewClient(agentID, transport)
	ctx := context.Background()
	info, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	m.clients[name] = client
	_ = info // info is stored in client

	return nil
}

// Disconnect disconnects from an ACP agent
func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, exists := m.clients[name]
	if !exists {
		return fmt.Errorf("agent '%s' not found", name)
	}

	if err := client.Disconnect(); err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	delete(m.clients, name)
	return nil
}

// GetClient gets a client by name
func (m *Manager) GetClient(name string) (*Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, exists := m.clients[name]
	if !exists {
		return nil, fmt.Errorf("agent '%s' not found", name)
	}

	return client, nil
}

// ListConnected lists all connected agent names
func (m *Manager) ListConnected() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	return names
}

// ListConnectedAgents returns detailed info about all connected agents
func (m *Manager) ListConnectedAgents() []AgentInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agents := make([]AgentInfo, 0, len(m.clients))
	for _, client := range m.clients {
		info, err := client.GetAgentInfo(context.Background())
		if err != nil {
			// Skip client with error
			continue
		}
		if info != nil {
			agents = append(agents, *info)
		}
	}
	return agents
}

// StartServer starts an ACP server
func (m *Manager) StartServer(name string, agentID string, info AgentInfo, transport Transport) (*Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.servers[name]; exists {
		return nil, fmt.Errorf("server '%s' already exists", name)
	}

	server := NewServerWithTransport(agentID, info, transport)
	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start server: %w", err)
	}

	m.servers[name] = server
	return server, nil
}

// StopServer stops an ACP server
func (m *Manager) StopServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	server, exists := m.servers[name]
	if !exists {
		return fmt.Errorf("server '%s' not found", name)
	}

	if err := server.Stop(); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	delete(m.servers, name)
	return nil
}

// GetServer gets a server by name
func (m *Manager) GetServer(name string) (*Server, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	server, exists := m.servers[name]
	if !exists {
		return nil, fmt.Errorf("server '%s' not found", name)
	}

	return server, nil
}

// ListServers lists all running servers
func (m *Manager) ListServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	return names
}

// CallSkill calls a skill on a connected agent
func (m *Manager) CallSkill(ctx context.Context, agentName string, skillName string, params map[string]interface{}) (interface{}, error) {
	client, err := m.GetClient(agentName)
	if err != nil {
		return nil, err
	}

	return client.CallSkill(ctx, skillName, params)
}

// ListAllSkills lists all skills from all connected agents
func (m *Manager) ListAllSkills() []Skill {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allSkills []Skill
	for name, client := range m.clients {
		skills := client.GetSkills()
		for _, skill := range skills {
			// Prefix skill name with agent name to avoid conflicts
			skill.Source = name
			allSkills = append(allSkills, skill)
		}
	}
	return allSkills
}

// Ping checks connectivity to an agent
func (m *Manager) Ping(ctx context.Context, name string) error {
	client, err := m.GetClient(name)
	if err != nil {
		return err
	}

	return client.Ping(ctx)
}

// HealthCheck checks health of all connections
func (m *Manager) HealthCheck() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]bool)
	for name, client := range m.clients {
		results[name] = client.Ping(context.Background()) == nil
	}
	return results
}

// Close closes all connections and servers
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	// Close all clients
	for name, client := range m.clients {
		if err := client.Disconnect(); err != nil {
			errs = append(errs, fmt.Errorf("client %s: %w", name, err))
		}
	}

	// Stop all servers
	for name, server := range m.servers {
		if err := server.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("server %s: %w", name, err))
		}
	}

	m.clients = make(map[string]*Client)
	m.servers = make(map[string]*Server)

	if len(errs) > 0 {
		return fmt.Errorf("errors during close: %v", errs)
	}
	return nil
}
