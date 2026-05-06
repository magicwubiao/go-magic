# ACP (Agent Communication Protocol)

ACP is a protocol for inter-agent communication, enabling agents to discover and call each other's skills dynamically.

## Overview

ACP is designed for agent-to-agent communication where each agent can act as both a client and a server:
- **Server**: Exposes agent capabilities (skills, info, memory) to other agents
- **Client**: Connects to other agents and calls their exposed skills

## Quick Start

### CLI Commands

```bash
# Connect to an agent via HTTP
magic acp connect math-agent http://localhost:8080

# Connect via SSE
magic acp connect math-agent sse://localhost:8080/events

# Connect via stdio (subprocess)
magic acp connect math-agent --transport stdio --command "python" --args "-m" "agent"

# List connected agents
magic acp list

# Call a skill on a connected agent
magic acp call math-agent calculate --params '{"expression":"2+2"}'

# List available skills
magic acp skills
magic acp skills math-agent

# Check connectivity
magic acp ping
magic acp ping math-agent

# Disconnect
magic acp disconnect math-agent
```

### Go API

```go
import "github.com/magicwubiao/go-magic/internal/acp"

// Create manager
manager := acp.NewManager()

// Connect to an agent
err := manager.ConnectHTTP("math-agent", "math-agent", "http://localhost:8080", nil)
if err != nil {
    log.Fatal(err)
}

// Call a skill
result, err := manager.CallSkill(ctx, "math-agent", "calculate", map[string]interface{}{
    "expression": "2 + 2",
})

// List skills from all connected agents
skills := manager.ListAllSkills()

// List connected agents
agents := manager.ListConnectedAgents()
```

### Creating an ACP Server

```go
import "github.com/magicwubiao/go-magic/internal/acp"

// Create server with agent info
info := acp.AgentInfo{
    ID:       "my-agent",
    Name:     "My Agent",
    Version:  "1.0.0",
    Capabilities: []string{
        "skill/call",
        "message/send",
    },
}

server := acp.NewServer("my-agent", info)

// Register a skill
server.RegisterSkill(acp.Skill{
    Name:        "greet",
    Description: "Greet someone",
    InputSchema: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "name": map[string]interface{}{
                "type":        "string",
                "description": "Name to greet",
            },
        },
    },
}, func(ctx context.Context, params json.RawMessage) (interface{}, error) {
    var args struct {
        Name string `json:"name"`
    }
    json.Unmarshal(params, &args)
    return map[string]string{
        "message": fmt.Sprintf("Hello, %s!", args.Name),
    }, nil
})

// Start server
ctx := context.Background()
server.Start(ctx)
```

### Using Built-in Tools

ACP provides built-in tools that can be integrated with the agent system:

```go
adapter := acp.NewToolAdapter(manager)

// List connected agents
result, _ := adapter.Execute(ctx, "acp_list_agents", nil)

// Get skills from all agents
result, _ := adapter.Execute(ctx, "acp_get_skills", nil)

// Ping agents
result, _ := adapter.Execute(ctx, "acp_ping", map[string]interface{}{
    "agent": "math-agent",
})
```

## Built-in Tools

| Tool | Description |
|------|-------------|
| `acp_connect` | Connect to another agent |
| `acp_call_skill` | Call a skill from a connected agent |
| `acp_share_memory` | Share a memory item with another agent |
| `acp_list_agents` | List all connected agents |
| `acp_get_skills` | Get skills from connected agents |
| `acp_ping` | Check connectivity to agents |

## Transport Types

### HTTP Transport
Simple request-response over HTTP.
```go
transport := acp.NewHTTPTransport("http://localhost:8080", headers)
client := acp.NewClient("agent-id", transport)
```

### SSE Transport
Server-Sent Events for server-initiated messages.
```go
transport, _ := acp.NewSSETransport("http://localhost:8080/events", headers)
```

### Stdio Transport
Local subprocess communication via stdin/stdout.
```go
transport, _ := acp.NewStdioTransport("python", []string{"-m", "agent"}, env)
```

## Protocol Messages

### JSON-RPC 2.0 Based

ACP uses JSON-RPC 2.0 as its wire format:

```json
// Request
{
    "jsonrpc": "2.0",
    "method": "skill/call",
    "params": {"skillName": "calculate", "params": {"expression": "2+2"}},
    "id": "req-123"
}

// Response
{
    "jsonrpc": "2.0",
    "result": {"success": true, "output": "4"},
    "id": "req-123"
}

// Error Response
{
    "jsonrpc": "2.0",
    "error": {"code": -32601, "message": "Method not found"},
    "id": "req-123"
}
```

### Built-in Methods

| Method | Description |
|--------|-------------|
| `connect` | Initial handshake with agent |
| `ping` | Health check |
| `agent/info` | Get agent information |
| `skill/list` | List available skills |
| `skill/call` | Call a skill |
| `message/send` | Send a message |
| `memory/share` | Share a memory item |

## Integration with Agent System

```go
import "github.com/magicwubiao/go-magic/internal/acp"
import "github.com/magicwubiao/go-magic/internal/agent"

// In agent initialization
acpManager := acp.NewManager()

// Connect to other agents
acpManager.ConnectHTTP("helper", "helper", "http://localhost:8081", nil)

// Register ACP tools
toolAdapter := acp.NewToolAdapter(acpManager)
for _, tool := range acp.BuiltinTools() {
    registry.Register(toolAdapter)
}

// Use in agent
result, _ := acpManager.CallSkill(ctx, "helper", "search", map[string]interface{}{
    "query": "golang best practices",
})
```

## Error Codes

| Code | Name | Description |
|------|------|-------------|
| -32700 | Parse Error | Invalid JSON |
| -32600 | Invalid Request | JSON-RPC request is invalid |
| -32601 | Method Not Found | Method does not exist |
| -32602 | Invalid Params | Invalid method parameters |
| -32603 | Internal Error | Internal server error |
| -32000 | Server Error | Generic server error |
| -32001 | Unauthorized | Authentication required |
| -32002 | Connection Failed | Unable to connect |

## License

Part of go-magic project.
