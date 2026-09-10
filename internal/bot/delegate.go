package bot

import (
	"context"
	"fmt"
	"strings"
)

// delegateTaskTool implements task delegation between bots: a bot hands a
// self-contained subtask to a teammate and synchronously waits for the
// teammate's full reply, which it can then incorporate into its own answer.
// This complements message_agent (fire-and-forget) with a request/response
// workflow for multi-step collaboration.
type delegateTaskTool struct {
	manager *Manager
	sender  string // Mention tag of the delegating bot

	// teammates is a snapshot taken at construction time (see messageAgentTool
	// for why we avoid taking m.mu at runtime).
	teammates string
}

func newDelegateTaskTool(m *Manager, senderTag string) *delegateTaskTool {
	return &delegateTaskTool{
		manager:   m,
		sender:    senderTag,
		teammates: m.rosterLocked(), // Caller holds m.mu; safe snapshot
	}
}

func (t *delegateTaskTool) Name() string { return "delegate_task" }

func (t *delegateTaskTool) Description() string {
	return fmt.Sprintf(
		"Delegate a self-contained subtask to another bot and wait for its full reply. "+
			"Available teammates: %s. "+
			"Use this when a piece of work is better done by a specialized teammate and you need "+
			"its result to continue (e.g. ask the researcher to gather facts, then you synthesize). "+
			"The call blocks until the teammate finishes; its reply is returned to you. "+
			"Prefer message_agent for fire-and-forget notices where you do not need the reply.",
		t.teammates,
	)
}

func (t *delegateTaskTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Teammate mention tag or name (e.g. \"researcher\")",
			},
			"task": map[string]interface{}{
				"type":        "string",
				"description": "A clear, self-contained description of the subtask for the teammate to complete.",
			},
		},
		"required": []string{"target", "task"},
	}
}

func (t *delegateTaskTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	target, _ := params["target"].(string)
	task, _ := params["task"].(string)
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("target is required")
	}
	if strings.TrimSpace(task) == "" {
		return nil, fmt.Errorf("task is required")
	}

	target = strings.TrimPrefix(strings.TrimSpace(target), "@")
	reply, err := t.manager.SendToBot(target, task)
	if err != nil {
		return nil, fmt.Errorf("delegation to @%s failed: %w", target, err)
	}
	return fmt.Sprintf("Reply from @%s:\n%s", target, reply), nil
}
