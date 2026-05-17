package main

import (
	"github.com/magicwubiao/go-magic/internal/subagent"
	"github.com/magicwubiao/go-magic/internal/tool"
)

// toolRegistryAdapter adapts tool.Registry to subagent.ToolRegistry
type toolRegistryAdapter struct {
	registry *tool.Registry
}

// List returns all tool names
func (a *toolRegistryAdapter) List() []string {
	return a.registry.List()
}

// Get retrieves a tool by name
func (a *toolRegistryAdapter) Get(name string) (subagent.Tool, error) {
	t, err := a.registry.Get(name)
	if err != nil {
		return nil, err
	}
	return &toolAdapter{tool: t}, nil
}

// toolAdapter adapts tool.Tool to subagent.Tool
type toolAdapter struct {
	tool tool.Tool
}

func (a *toolAdapter) Name() string {
	return a.tool.Name()
}

func (a *toolAdapter) Description() string {
	return a.tool.Description()
}

func (a *toolAdapter) Schema() map[string]interface{} {
	return a.tool.Schema()
}

// newToolRegistryAdapter creates a new adapter
func newToolRegistryAdapter(registry *tool.Registry) subagent.ToolRegistry {
	return &toolRegistryAdapter{registry: registry}
}
