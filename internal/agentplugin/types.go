// Package agentplugin 实现 OpenAI Agent Plugins 1.0.0 规范的可移植插件加载器。
//
// 规范要点:
//   - 一个插件是一个目录,根目录必须含 plugin.json 清单(闭包 schema)。
//   - 组件固定位置:skills/<name>/SKILL.md 与根目录 mcp.json。
//   - 失败隔离:单个 MCP server 失败不阻塞 skills;不合规 skill 跳过;顶层 mcp.json
//     无效仅禁用 MCP;manifest 致命违反才拒绝整个插件。
//   - 两个运行时变量 ${PLUGIN_ROOT} / ${PLUGIN_DATA} 仅在 stdio 的 args/env 值/cwd 展开。
//
// 本包只负责「加载、校验、发现、连接」,不规定分发、安装与权限模型(规范明确排除)。
package agentplugin

import (
	"github.com/magicwubiao/go-magic/internal/skills"
)

// 规范常量
const (
	// SpecVersion 当前支持的 Agent Plugins 规范版本。
	SpecVersion = "1.0.0"
	// PluginSchemaURL plugin.json 的规范 schema 标识。
	PluginSchemaURL = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	// MCPSchemaURL mcp.json 的规范 schema 标识。
	MCPSchemaURL = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"

	// skillsDirName skills 组件的固定目录名。
	skillsDirName = "skills"
	// skillMdName 每个 skill 子目录必须包含的清单文件。
	skillMdName = "SKILL.md"
	// manifestName plugin.json 文件名。
	manifestName = "plugin.json"
	// mcpConfigName mcp.json 文件名。
	mcpConfigName = "mcp.json"
)

// Transport 类型枚举。
const (
	TransportStdio          = "stdio"
	TransportStreamableHTTP = "streamable-http"
	TransportSSE            = "sse"
)

// Manifest 描述 plugin.json 清单。
// schema 为闭包:未知顶层字段须报告并忽略(非致命),其他 schema 违反为致命。
type Manifest struct {
	Schema      string         `json:"$schema"`           // 必须,选择规范校验契约
	Name        string         `json:"name"`              // 必须,1-64 字符,小写字母/数字/连字符/点
	Version     string         `json:"version,omitempty"` // 可选,推荐 SemVer
	Description string         `json:"description,omitempty"`
	Author      *Author        `json:"author,omitempty"`
	Homepage    string         `json:"homepage,omitempty"`
	Repository  string         `json:"repository,omitempty"`
	License     string         `json:"license,omitempty"`
	Keywords    []string       `json:"keywords,omitempty"`
	Extensions  map[string]any `json:"extensions,omitempty"` // 反向域名命名空间,客户端按需消费
	Unknown     []string       `json:"-"`                    // 加载时检测到的未知顶层字段(报告用)
}

// Author 清单中的作者信息。
type Author struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// MCPServerSpec 描述 mcp.json 中单个 MCP server 条目。
type MCPServerSpec struct {
	Type    string            `json:"type"`              // stdio | streamable-http | sse
	Command string            `json:"command,omitempty"` // stdio:单个可执行 token(不展开占位符)
	Args    []string          `json:"args,omitempty"`    // stdio:展开占位符
	Env     map[string]string `json:"env,omitempty"`     // stdio:env 值展开占位符,key 不展开
	Cwd     string            `json:"cwd,omitempty"`     // stdio:展开占位符,须在 PLUGIN_ROOT/PLUGIN_DATA 内
	URL     string            `json:"url,omitempty"`     // http:绝对 URL(不展开占位符)
	Headers map[string]string `json:"headers,omitempty"` // http:字面 headers,不含凭证
}

// MCPConfig 描述 mcp.json 文档。
type MCPConfig struct {
	Schema     string                   `json:"$schema"`
	MCPServers map[string]MCPServerSpec `json:"mcpServers"`
}

// SkillRef 描述插件内发现的一个 skill。
type SkillRef struct {
	Name  string        // skill 子目录名
	Dir   string        // skill 目录绝对路径(已解析符号链接)
	Skill *skills.Skill // 解析后的 skill(失败时为 nil)
	Error string        // 该 skill 加载失败原因(空表示成功)
}

// MCPEntryStatus 描述一个 MCP server 条目的连接状态。
type MCPEntryStatus struct {
	Name      string        // server 条目名
	Spec      MCPServerSpec // 原始配置(占位符已展开)
	Connected bool          // 是否已连接
	ToolCount int           // 已发现的工具数
	Error     string        // 连接/校验失败原因(空表示成功)
}

// Plugin 表示一个已加载的 Agent Plugin。
type Plugin struct {
	Root       string           // 插件根目录(文件系统解析后的绝对路径)
	DataDir    string           // PLUGIN_DATA 可写数据目录(跨更新持久)
	Manifest   *Manifest        // 清单
	Skills     []SkillRef       // 发现的 skills
	MCPServers []MCPEntryStatus // MCP 连接状态
	Extensions map[string]any   // 已识别的扩展命名空间数据(未实现的命名空间已忽略)

	// 加载报告
	MCPDisabled   bool     // 顶层 mcp.json 无效时整个 MCP 被禁用
	ManifestValid bool     // 清单是否通过校验
	FatalError    string   // 致命错误(非空时插件被拒绝,仅 Root/Manifest 可用)
	Warnings      []string // 非致命报告(unknown 字段、非 object extensions、跳过的 skill 等)
}

// Manager 管理多个已加载的 Agent Plugin。
type Manager struct {
	plugins  map[string]*Plugin // key = manifest.name
	roots    []string           // 扫描根目录列表
	disabled map[string]bool    // 被显式禁用的插件名集合
}
