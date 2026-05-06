package acp

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolAdapter adapts ACP client operations to tool interface
type ToolAdapter struct {
	manager *Manager
}

// NewToolAdapter creates a new tool adapter for ACP
func NewToolAdapter(manager *Manager) *ToolAdapter {
	return &ToolAdapter{manager: manager}
}

// Execute executes an ACP tool
func (t *ToolAdapter) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "acp_connect":
		return t.executeConnect(ctx, args)
	case "acp_call_skill":
		return t.executeCallSkill(ctx, args)
	case "acp_share_memory":
		return t.executeShareMemory(ctx, args)
	case "acp_list_agents":
		return t.executeListAgents(ctx, args)
	case "acp_get_skills":
		return t.executeGetSkills(ctx, args)
	case "acp_ping":
		return t.executePing(ctx, args)
	default:
		return nil, fmt.Errorf("unknown ACP tool: %s", name)
	}
}

func (t *ToolAdapter) executeConnect(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}

	transportType, _ := args["transport"].(string)
	address, _ := args["address"].(string)
	agentID, _ := args["agent_id"].(string)

	if agentID == "" {
		agentID = name
	}

	var headers map[string]string
	if h, ok := args["headers"].(map[string]interface{}); ok {
		headers = make(map[string]string)
		for k, v := range h {
			if vs, ok := v.(string); ok {
				headers[k] = vs
			}
		}
	}

	switch transportType {
	case "http", "HTTP":
		if address == "" {
			return nil, fmt.Errorf("missing address for HTTP transport")
		}
		if err := t.manager.ConnectHTTP(name, agentID, address, headers); err != nil {
			return nil, err
		}
	case "sse", "SSE":
		if address == "" {
			return nil, fmt.Errorf("missing address for SSE transport")
		}
		if err := t.manager.ConnectSSE(name, agentID, address, headers); err != nil {
			return nil, err
		}
	case "stdio":
		command, _ := args["command"].(string)
		if command == "" {
			return nil, fmt.Errorf("missing command for stdio transport")
		}
		var cmdArgs []string
		if a, ok := args["args"].([]interface{}); ok {
			for _, v := range a {
				if s, ok := v.(string); ok {
					cmdArgs = append(cmdArgs, s)
				}
			}
		}
		var env []string
		if e, ok := args["env"].([]interface{}); ok {
			for _, v := range e {
				if s, ok := v.(string); ok {
					env = append(env, s)
				}
			}
		}
		if err := t.manager.ConnectStdio(name, agentID, command, cmdArgs, env); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", transportType)
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("connected to agent '%s'", name),
	}, nil
}

func (t *ToolAdapter) executeCallSkill(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentName, ok := args["agent"].(string)
	if !ok || agentName == "" {
		return nil, fmt.Errorf("missing required parameter: agent")
	}

	skillName, ok := args["skill"].(string)
	if !ok || skillName == "" {
		return nil, fmt.Errorf("missing required parameter: skill")
	}

	var params map[string]interface{}
	if p, ok := args["params"].(map[string]interface{}); ok {
		params = p
	}

	result, err := t.manager.CallSkill(ctx, agentName, skillName, params)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (t *ToolAdapter) executeShareMemory(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentName, ok := args["agent"].(string)
	if !ok || agentName == "" {
		return nil, fmt.Errorf("missing required parameter: agent")
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return nil, fmt.Errorf("missing required parameter: content")
	}

	memoryType, _ := args["type"].(string)
	if memoryType == "" {
		memoryType = "shared"
	}

	item := MemoryItem{
		Content: content,
		Type:    memoryType,
	}

	// Get client and share memory
	client, err := t.manager.GetClient(agentName)
	if err != nil {
		return nil, err
	}

	if err := client.ShareMemory(ctx, item); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"message": "memory shared successfully",
	}, nil
}

func (t *ToolAdapter) executeListAgents(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agents := t.manager.ListConnectedAgents()

	result := make([]map[string]interface{}, 0, len(agents))
	for _, agent := range agents {
		result = append(result, map[string]interface{}{
			"id":           agent.ID,
			"name":         agent.Name,
			"version":      agent.Version,
			"capabilities": agent.Capabilities,
		})
	}

	return map[string]interface{}{
		"agents": result,
		"count":  len(result),
	}, nil
}

func (t *ToolAdapter) executeGetSkills(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentName, _ := args["agent"].(string)

	if agentName == "" {
		// Return all skills from all agents
		skills := t.manager.ListAllSkills()
		result := make([]map[string]interface{}, 0, len(skills))
		for _, skill := range skills {
			result = append(result, map[string]interface{}{
				"name":        skill.Name,
				"description": skill.Description,
				"source":      skill.Source,
			})
		}
		return map[string]interface{}{
			"skills": result,
			"count":  len(result),
		}, nil
	}

	// Get skills from specific agent
	client, err := t.manager.GetClient(agentName)
	if err != nil {
		return nil, err
	}

	skills, err := client.ListSkills(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(skills))
	for _, skill := range skills {
		result = append(result, map[string]interface{}{
			"name":        skill.Name,
			"description": skill.Description,
		})
	}

	return map[string]interface{}{
		"agent":  agentName,
		"skills": result,
		"count":  len(result),
	}, nil
}

func (t *ToolAdapter) executePing(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentName, ok := args["agent"].(string)
	if !ok || agentName == "" {
		// Ping all agents
		health := t.manager.HealthCheck()
		result := make(map[string]interface{})
		for name, ok := range health {
			result[name] = map[string]bool{"healthy": ok}
		}
		return result, nil
	}

	if err := t.manager.Ping(ctx, agentName); err != nil {
		return map[string]interface{}{
			"agent":    agentName,
			"healthy":  false,
			"error":    err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"agent":   agentName,
		"healthy": true,
	}, nil
}

// BuiltinTools returns the list of built-in ACP tools
func BuiltinTools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "acp_connect",
			"description": "Connect to another agent via ACP protocol",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name to identify this connection",
					},
					"agent_id": map[string]interface{}{
						"type":        "string",
						"description": "Agent ID for the connection",
					},
					"transport": map[string]interface{}{
						"type":        "string",
						"description": "Transport type: http, sse, or stdio",
						"enum":        []string{"http", "sse", "stdio"},
					},
					"address": map[string]interface{}{
						"type":        "string",
						"description": "URL or address for http/sse transport",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command for stdio transport",
					},
					"args": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
						"description": "Command arguments for stdio transport",
					},
					"headers": map[string]interface{}{
						"type":        "object",
						"description": "HTTP headers for http/sse transport",
					},
				},
				"required": []string{"name", "transport"},
			},
		},
		{
			"name":        "acp_call_skill",
			"description": "Call a skill from a connected agent",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"description": "Name of the connected agent",
					},
					"skill": map[string]interface{}{
						"type":        "string",
						"description": "Name of the skill to call",
					},
					"params": map[string]interface{}{
						"type":        "object",
						"description": "Parameters to pass to the skill",
					},
				},
				"required": []string{"agent", "skill"},
			},
		},
		{
			"name":        "acp_share_memory",
			"description": "Share a memory item with another agent",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"description": "Name of the connected agent",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Memory content to share",
					},
					"type": map[string]interface{}{
						"type":        "string",
						"description": "Memory type (default: shared)",
					},
				},
				"required": []string{"agent", "content"},
			},
		},
		{
			"name":        "acp_list_agents",
			"description": "List all connected ACP agents",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties":  map[string]interface{}{},
			},
		},
		{
			"name":        "acp_get_skills",
			"description": "Get skills available from connected agents",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"description": "Optional: specific agent name",
					},
				},
			},
		},
		{
			"name":        "acp_ping",
			"description": "Check connectivity to ACP agents",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent": map[string]interface{}{
						"type":        "string",
						"description": "Optional: specific agent name, omit to ping all",
					},
				},
			},
		},
	}
}

// ToolSchemaToMap converts a tool schema to map format used by agent
func ToolSchemaToMap(tool map[string]interface{}) map[string]interface{} {
	return tool
}

// ParseSkillCall parses a skill call request
func ParseSkillCall(params json.RawMessage) (string, string, map[string]interface{}, error) {
	var req struct {
		Agent string                 `json:"agent"`
		Skill string                 `json:"skill"`
		Params map[string]interface{} `json:"params"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return "", "", nil, fmt.Errorf("failed to parse skill call: %w", err)
	}

	if req.Agent == "" {
		return "", "", nil, fmt.Errorf("missing agent name")
	}

	if req.Skill == "" {
		return "", "", nil, fmt.Errorf("missing skill name")
	}

	return req.Agent, req.Skill, req.Params, nil
}
