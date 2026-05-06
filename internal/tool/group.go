package tool

import "sync"

// ToolGroup 工具分组
type ToolGroup struct {
	Name        string   // 分组名称
	Description string   // 分组描述
	Tools       []Tool   // 分组中的工具
	Enabled     bool     // 是否启用
	Metadata    map[string]any // 分组元数据
}

// NewToolGroup 创建新的工具分组
func NewToolGroup(name, description string) *ToolGroup {
	return &ToolGroup{
		Name:        name,
		Description: description,
		Tools:       make([]Tool, 0),
		Enabled:     true,
		Metadata:    make(map[string]any),
	}
}

// Add 添加工具到分组
func (g *ToolGroup) Add(tool Tool) {
	g.Tools = append(g.Tools, tool)
}

// Remove 从分组移除工具
func (g *ToolGroup) Remove(toolName string) bool {
	for i, t := range g.Tools {
		if t.Name() == toolName {
			g.Tools = append(g.Tools[:i], g.Tools[i+1:]...)
			return true
		}
	}
	return false
}

// Get 获取分组中的工具
func (g *ToolGroup) Get(toolName string) Tool {
	for _, t := range g.Tools {
		if t.Name() == toolName {
			return t
		}
	}
	return nil
}

// Count 返回分组中的工具数量
func (g *ToolGroup) Count() int {
	return len(g.Tools)
}

// ============================================================================
// GroupManager - 分组管理器
// ============================================================================

type GroupManager struct {
	mu      sync.RWMutex
	groups  map[string]*ToolGroup
	toolsInGroup map[string]string // toolName -> groupName
}

// NewGroupManager 创建分组管理器
func NewGroupManager() *GroupManager {
	return &GroupManager{
		groups:       make(map[string]*ToolGroup),
		toolsInGroup: make(map[string]string),
	}
}

// CreateGroup 创建新的分组
func (gm *GroupManager) CreateGroup(name, description string) *ToolGroup {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	
	group := NewToolGroup(name, description)
	gm.groups[name] = group
	return group
}

// GetGroup 获取分组
func (gm *GroupManager) GetGroup(name string) *ToolGroup {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.groups[name]
}

// DeleteGroup 删除分组（不删除工具）
func (gm *GroupManager) DeleteGroup(name string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	
	if group, ok := gm.groups[name]; ok {
		for _, tool := range group.Tools {
			delete(gm.toolsInGroup, tool.Name())
		}
		delete(gm.groups, name)
	}
}

// RegisterGroup 注册分组
func (gm *GroupManager) RegisterGroup(group *ToolGroup) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	
	gm.groups[group.Name] = group
	for _, tool := range group.Tools {
		gm.toolsInGroup[tool.Name()] = group.Name
	}
}

// AddToolToGroup 将工具添加到分组
func (gm *GroupManager) AddToolToGroup(tool Tool, groupName string) error {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	
	group, ok := gm.groups[groupName]
	if !ok {
		group = NewToolGroup(groupName, "")
		gm.groups[groupName] = group
	}
	
	group.Add(tool)
	gm.toolsInGroup[tool.Name()] = groupName
	return nil
}

// RemoveToolFromGroup 将工具从分组移除
func (gm *GroupManager) RemoveToolFromGroup(toolName, groupName string) error {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	
	group, ok := gm.groups[groupName]
	if !ok {
		return nil
	}
	
	group.Remove(toolName)
	delete(gm.toolsInGroup, toolName)
	return nil
}

// GetToolGroup 获取工具所属的分组
func (gm *GroupManager) GetToolGroup(toolName string) string {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.toolsInGroup[toolName]
}

// ListGroups 列出所有分组
func (gm *GroupManager) ListGroups() []*ToolGroup {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	
	groups := make([]*ToolGroup, 0, len(gm.groups))
	for _, g := range gm.groups {
		groups = append(groups, g)
	}
	return groups
}

// GetEnabledTools 获取所有启用的工具
func (gm *GroupManager) GetEnabledTools() []Tool {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	
	var tools []Tool
	for _, group := range gm.groups {
		if group.Enabled {
			tools = append(tools, group.Tools...)
		}
	}
	return tools
}

// EnableGroup 启用分组
func (gm *GroupManager) EnableGroup(name string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	
	if group, ok := gm.groups[name]; ok {
		group.Enabled = true
	}
}

// DisableGroup 禁用分组
func (gm *GroupManager) DisableGroup(name string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	
	if group, ok := gm.groups[name]; ok {
		group.Enabled = false
	}
}

// DefaultGroups 返回默认的工具分组
func DefaultToolGroups(registry *Registry) *GroupManager {
	gm := NewGroupManager()
	
	// 文件操作分组
	fileGroup := gm.CreateGroup("file", "文件操作工具")
	for _, name := range []string{"read_file", "write_file", "list_files", "search_in_files"} {
		if tool, err := registry.Get(name); err == nil {
			fileGroup.Add(tool)
		}
	}
	
	// Web 操作分组
	webGroup := gm.CreateGroup("web", "Web 操作工具")
	for _, name := range []string{"web_search", "web_extract", "web_fetch"} {
		if tool, err := registry.Get(name); err == nil {
			webGroup.Add(tool)
		}
	}
	
	// 代码执行分组
	codeGroup := gm.CreateGroup("code", "代码执行工具")
	for _, name := range []string{"execute_command", "python_execute", "node_execute"} {
		if tool, err := registry.Get(name); err == nil {
			codeGroup.Add(tool)
		}
	}
	
	// 记忆分组
	memoryGroup := gm.CreateGroup("memory", "记忆工具")
	for _, name := range []string{"memory_store", "memory_recall"} {
		if tool, err := registry.Get(name); err == nil {
			memoryGroup.Add(tool)
		}
	}
	
	return gm
}
