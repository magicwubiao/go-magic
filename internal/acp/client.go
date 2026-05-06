package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Client represents an ACP client for connecting to other agents
type Client struct {
	agentID   string
	transport Transport
	info      *AgentInfo
	skills    map[string]Skill
	mu        sync.RWMutex
	connected bool
	timeout   time.Duration
}

// NewClient creates a new ACP client
func NewClient(agentID string, transport Transport) *Client {
	return &Client{
		agentID: agentID,
		transport: transport,
		skills:   make(map[string]Skill),
		timeout:  30 * time.Second,
	}
}

// NewClientWithTimeout creates a new ACP client with custom timeout
func NewClientWithTimeout(agentID string, transport Transport, timeout time.Duration) *Client {
	client := NewClient(agentID, transport)
	client.timeout = timeout
	return client
}

// Connect connects to an ACP server and performs handshake
func (c *Client) Connect(ctx context.Context) (*AgentInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return c.info, nil
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Send connection request
	req := NewJSONRPCRequest("connect", ConnectionRequest{
		AgentID: c.agentID,
		AgentInfo: AgentInfo{
			ID:      c.agentID,
			Version: ProtocolVersion,
		},
		Capabilities: []string{"skill/call", "message/send", "memory/share"},
	}, uuid.New().String())

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("connection error: %s", resp.Error.Message)
	}

	var connResp ConnectionResponse
	if err := json.Unmarshal(resp.Result, &connResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !connResp.Success {
		return nil, fmt.Errorf("connection rejected: %s", connResp.Error)
	}

	c.info = &connResp.AgentInfo
	c.connected = true

	// Store skills
	for _, skill := range connResp.Skills {
		c.skills[skill.Name] = skill
	}

	return c.info, nil
}

// Disconnect disconnects from the ACP server
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	c.connected = false
	c.skills = make(map[string]Skill)
	c.info = nil

	return c.transport.Close()
}

// CallSkill calls a skill on the connected agent
func (c *Client) CallSkill(ctx context.Context, skillName string, params map[string]interface{}) (interface{}, error) {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req := NewJSONRPCRequest("skill/call", SkillCallRequest{
		SkillName: skillName,
		Params:    params,
	}, uuid.New().String())

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("skill call failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("skill error: %s", resp.Error.Message)
	}

	var skillResp SkillCallResponse
	if err := json.Unmarshal(resp.Result, &skillResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !skillResp.Success {
		return nil, fmt.Errorf("skill execution failed: %s", skillResp.Error)
	}

	if skillResp.Result != nil {
		var result interface{}
		if err := json.Unmarshal(skillResp.Result, &result); err != nil {
			return skillResp.Result, nil
		}
		return result, nil
	}

	return skillResp.Output, nil
}

// ListSkills lists all available skills from the connected agent
func (c *Client) ListSkills(ctx context.Context) ([]Skill, error) {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req := NewJSONRPCRequest("skill/list", nil, uuid.New().String())

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list skills failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("list skills error: %s", resp.Error.Message)
	}

	var listResp ListResponse
	if err := json.Unmarshal(resp.Result, &listResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	skills := make([]Skill, 0, listResp.Count)
	for _, item := range listResp.Items {
		if skill, ok := item.(map[string]interface{}); ok {
			skillBytes, _ := json.Marshal(skill)
			var s Skill
			json.Unmarshal(skillBytes, &s)
			skills = append(skills, s)
		}
	}

	return skills, nil
}

// GetAgentInfo returns information about the connected agent
func (c *Client) GetAgentInfo(ctx context.Context) (*AgentInfo, error) {
	c.mu.RLock()
	connected := c.connected
	info := c.info
	c.mu.RUnlock()

	if !connected {
		return nil, fmt.Errorf("not connected")
	}

	if info != nil {
		return info, nil
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req := NewJSONRPCRequest("agent/info", nil, uuid.New().String())

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get agent info failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("get agent info error: %s", resp.Error.Message)
	}

	var agentInfo AgentInfo
	if err := json.Unmarshal(resp.Result, &agentInfo); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	c.mu.Lock()
	c.info = &agentInfo
	c.mu.Unlock()

	return &agentInfo, nil
}

// SendMessage sends a message to another agent (via connected agent)
func (c *Client) SendMessage(ctx context.Context, targetAgent string, content string) error {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req := NewJSONRPCRequest("message/send", map[string]interface{}{
		"target": targetAgent,
		"content": content,
	}, uuid.New().String())

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("send message failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("send message error: %s", resp.Error.Message)
	}

	return nil
}

// ShareMemory shares a memory item with the connected agent
func (c *Client) ShareMemory(ctx context.Context, item MemoryItem) error {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req := NewJSONRPCRequest("memory/share", item, uuid.New().String())

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("share memory failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("share memory error: %s", resp.Error.Message)
	}

	return nil
}

// Ping sends a ping to check connection
func (c *Client) Ping(ctx context.Context) error {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return fmt.Errorf("not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req := NewJSONRPCRequest("ping", nil, uuid.New().String())

	resp, err := c.transport.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("ping error: %s", resp.Error.Message)
	}

	return nil
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// GetSkills returns a copy of the known skills
func (c *Client) GetSkills() map[string]Skill {
	c.mu.RLock()
	defer c.mu.RUnlock()

	skills := make(map[string]Skill, len(c.skills))
	for k, v := range c.skills {
		skills[k] = v
	}
	return skills
}
