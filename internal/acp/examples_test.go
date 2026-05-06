package acp

import (
	"context"
	"encoding/json"
	"fmt"
)

// ExampleUsage demonstrates how to use the ACP package
func ExampleUsage() {
	// Create a manager
	manager := NewManager()

	// Connect to an agent using HTTP
	err := manager.ConnectHTTP("math-agent", "math-agent", "http://localhost:8080", nil)
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}

	// Call a skill
	ctx := context.Background()
	result, err := manager.CallSkill(ctx, "math-agent", "calculate", map[string]interface{}{
		"expression": "2 + 2",
	})
	if err != nil {
		fmt.Printf("Skill call failed: %v\n", err)
		return
	}

	fmt.Printf("Result: %v\n", result)
}

// ExampleServer demonstrates how to create an ACP server
func ExampleServer() {
	// Create server info
	info := AgentInfo{
		ID:       "my-agent",
		Name:     "My Agent",
		Version:  "1.0.0",
		Capabilities: []string{
			"skill/call",
			"skill/list",
			"message/send",
		},
	}

	// Create server
	server := NewServer("my-agent", info)

	// Register a skill
	server.RegisterSkill(Skill{
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

	// ... server is running ...

	// Stop server when done
	server.Stop()
}

// ExampleStdioTransport demonstrates stdio transport usage
func ExampleStdioTransport() {
	// Create stdio transport to a subprocess
	transport, err := NewStdioTransport("python", []string{"-m", "my_agent"}, nil)
	if err != nil {
		fmt.Printf("Failed to create transport: %v\n", err)
		return
	}
	defer transport.Close()

	// Create client
	client := NewClient("client-agent", transport)

	// Connect
	ctx := context.Background()
	info, err := client.Connect(ctx)
	if err != nil {
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}

	fmt.Printf("Connected to: %s\n", info.Name)

	// Call a skill
	result, err := client.CallSkill(ctx, "hello", nil)
	if err != nil {
		fmt.Printf("Skill call failed: %v\n", err)
		return
	}

	fmt.Printf("Result: %v\n", result)
}
