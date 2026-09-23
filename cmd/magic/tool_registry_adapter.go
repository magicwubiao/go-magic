package main

import (
	"context"

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

// Execute 让适配器满足子代理运行器所需的执行接口。
// subagent.Tool 自身只描述元信息（名字/描述/Schema），但子代理的工具循环
// 必须真的能跑工具——否则声明了工具却调不动，模型会一直重试到回合耗尽。
func (a *toolAdapter) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	return a.tool.Execute(ctx, params)
}

// newToolRegistryAdapter creates a new adapter
func newToolRegistryAdapter(registry *tool.Registry) subagent.ToolRegistry {
	return &toolRegistryAdapter{registry: registry}
}
