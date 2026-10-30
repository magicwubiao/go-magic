package agentplugin

import (
	"fmt"
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
				if err := validateEntryName(name); err != nil {
					// 条目名非法(会被拼进工具名 ap_<plugin>_<server>_<tool>):仅该条目失效。
					entry.Spec = spec
					entry.Error = err.Error()
					plugin.MCPServers = append(plugin.MCPServers, entry)
					continue
				}
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

	// 4. 加载 cli.json(可选扩展组件,顶层级边界)。
	cliCfg, cliErr := loadCLIConfig(resolvedRoot, manifest.Schema)
	switch {
	case cliErr != nil:
		// 顶层 cli.json 无效 → 整个 CLI 组件禁用,skills/MCP 不受影响。
		plugin.CLIDisabled = true
		plugin.Warnings = append(plugin.Warnings, fmt.Sprintf("CLI disabled: %v", cliErr))
	case cliCfg == nil:
		// 缺失:无 CLI 组件。
	default:
		plugin.CLIConnectors = buildCLIEntries(cliCfg)
	}

	return plugin
}

// validateEntryName 校验 mcp.json 中 server 条目名:1-64 字符,仅允许
// 字母/数字/下划线/连字符/点。条目名会被拼进工具名 ap_<plugin>_<server>_<tool>,
// 放任任意字符会污染工具注册表键名。
func validateEntryName(name string) error {
	if l := len(name); l < 1 || l > 64 {
		return fmt.Errorf("entry name must be 1-64 characters, got %d", l)
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_'
		if !ok {
			return fmt.Errorf("entry name %q contains illegal character %q (allowed: letters, digits, hyphen, period, underscore)", name, r)
		}
	}
	return nil
}

// IsRejected 报告插件是否因致命错误被拒绝(不可用)。
func (p *Plugin) IsRejected() bool { return p.FatalError != "" }
