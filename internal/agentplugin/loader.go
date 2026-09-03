package agentplugin

import (
	"fmt"
	"os"
	"path/filepath"
)

// Load 从 pluginRoot 加载一个 Agent Plugin,执行全部静态校验与组件发现,但不连接 MCP。
//
// 失败边界(按规范最窄原则):
//  1. plugin.json 解析在根外 / 缺失 / 致命 schema 违反 → 拒绝整个插件(FatalError 非空)。
//  2. 组件类型固定位置类型错误(如 skills/ 不是目录)→ 仅该组件类型无效。
//  3. 单个 skill 的 SKILL.md 逃逸根或不合规 → 跳过该 skill。
//  4. 单个 MCP entry 配置逃逸或不合规 → 该条目 Error,其他继续。
//  5. 顶层 mcp.json 无效 → 整个 MCP 禁用(MCPDisabled=true),skills 不受影响。
//
// dataDir 为 PLUGIN_DATA 可写目录(跨更新持久),由调用方提供;为空时自动推导。
// MCP 连接在 runtime.Start 中进行,Load 保持幂等且无副作用。
func Load(pluginRoot, dataDir string) *Plugin {
	// 解析符号链接,建立文件系统解析后的包边界。
	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(pluginRoot))
	if err != nil {
		return &Plugin{
			Root:          filepath.Clean(pluginRoot),
			FatalError:    fmt.Sprintf("resolve plugin root: %v", err),
			ManifestValid: false,
		}
	}

	plugin := &Plugin{
		Root:          resolvedRoot,
		DataDir:       dataDir,
		ManifestValid: true,
		Extensions:    map[string]any{},
	}

	// 1. 清单(致命边界)。
	manifest, mWarn, mErr := loadManifest(resolvedRoot)
	plugin.Warnings = append(plugin.Warnings, mWarn...)
	if mErr != nil {
		plugin.ManifestValid = false
		plugin.FatalError = mErr.Error()
		return plugin
	}
	plugin.Manifest = manifest
	// 透传已识别的扩展命名空间数据(未实现命名空间的值不校验,保留供消费)。
	if manifest.Extensions != nil {
		plugin.Extensions = manifest.Extensions
	}

	// 2. 发现 skills(组件类型级边界)。
	skillRefs, sWarn, sErr := discoverSkills(resolvedRoot)
	plugin.Warnings = append(plugin.Warnings, sWarn...)
	plugin.Skills = skillRefs
	if sErr != nil {
		// skills/ 类型错误:仅 skills 组件无效,不拒绝插件。
		plugin.Warnings = append(plugin.Warnings, fmt.Sprintf("skills component disabled: %v", sErr))
		plugin.Skills = nil
	}

	// 3. 加载 mcp.json(顶层级边界)。
	mcpCfg, mcpErr := loadMCPConfig(resolvedRoot)
	switch {
	case mcpErr != nil:
		// 顶层 mcp.json 无效 → 整个 MCP 禁用。
		plugin.MCPDisabled = true
		plugin.Warnings = append(plugin.Warnings, fmt.Sprintf("MCP disabled: %v", mcpErr))
	case mcpCfg == nil:
		// 缺失:无 MCP 组件。
	default:
		// 版本一致性:plugin.json 与 mcp.json 的 Agent Plugins 版本须匹配。
		mv := schemaVersion(manifest.Schema)
		cv := schemaVersion(mcpCfg.Schema)
		if mv != cv {
			plugin.MCPDisabled = true
			plugin.Warnings = append(plugin.Warnings,
				fmt.Sprintf("MCP disabled: version mismatch (plugin.json=%q, mcp.json=%q)", mv, cv))
		} else {
			// 逐条校验+展开(条目级边界)。
			for name, spec := range mcpCfg.MCPServers {
				entry := MCPEntryStatus{Name: name}
				expanded, err := validateAndExpandSpec(spec, resolvedRoot, dataDir)
				if err != nil {
					entry.Spec = spec
					entry.Error = err.Error()
				} else {
					entry.Spec = expanded
				}
				plugin.MCPServers = append(plugin.MCPServers, entry)
			}
		}
	}

	return plugin
}

// IsRejected 报告插件是否因致命错误被拒绝(不可用)。
func (p *Plugin) IsRejected() bool { return p.FatalError != "" }

// ensureDataDir 确保 PLUGIN_DATA 目录存在且可写。
func ensureDataDir(dataDir string) error {
	if dataDir == "" {
		return fmt.Errorf("data dir is empty")
	}
	return os.MkdirAll(dataDir, 0o755)
}
