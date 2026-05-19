package main

import (
	"context"
	"fmt"

	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/subagent"
)

// simpleAgentRunner is a simple implementation of AgentRunner for subagents
type simpleAgentRunner struct {
	provider     provider.Provider
	registry     subagent.ToolRegistry
	toolsSchema  []map[string]interface{}
	systemPrompt string
}

// RunConversation runs a conversation with the given input
func (r *simpleAgentRunner) RunConversation(ctx context.Context, input string) (string, error) {
	// Build messages
	messages := []provider.Message{
		{Role: "system", Content: r.systemPrompt},
		{Role: "user", Content: input},
	}

	// Call provider
	resp, err := r.provider.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("provider chat failed: %w", err)
	}

	if resp.Content == "" {
		return "", fmt.Errorf("no response from provider")
	}

	return resp.Content, nil
}
