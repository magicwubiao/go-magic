package agentplugin

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/pkg/clicatalog"
	"github.com/magicwubiao/go-magic/pkg/config"
)

// ManagedPlugin 封装一个已加载插件及其运行时(若有 MCP)。
type ManagedPlugin struct {
	Plugin   *Plugin
	Runtime  *Runtime // MCP 运行时;无 MCP 或被禁用时为 nil
	Disabled bool     // 是否被显式禁用(禁用时不启动 MCP、不注入 skills/tools)
}

// NewManager 创建一个扫描指定根目录列表的 Manager。
// disabled 为被显式禁用的插件名集合,这些插件不会被加载也不会启动 MCP。
func NewManager(scanDirs []string, disabled map[string]bool) *Manager {
	return &Manager{
		plugins:  make(map[string]*Plugin),
		roots:    scanDirs,
		disabled: disabled,
	}
}

// DefaultScanDir 返回默认扫描目录 ~/.magic/agent-plugins。
func DefaultScanDir() string {
	return filepath.Join(config.GetMagicHome(), "agent-plugins")
}

// EnsureDefaultScanDir 确保默认扫描目录存在。
func EnsureDefaultScanDir() error {
	return os.MkdirAll(DefaultScanDir(), 0o755)
}

// LoadAll 加载所有扫描根目录下的插件,返回按 name 索引的结果。
//
// 发现规则:每个扫描根的直接子目录,若含常规 plugin.json 则视为一个插件。
// 被禁用的插件仍会被加载(以便在 UI 中展示),但其 Runtime 为 nil,
// 调用方应据此跳过 StartAll / skills 注入 / tools 注入。
// 单个插件致命失败不阻塞其他插件(插件级失败隔离)。
func (m *Manager) LoadAll() map[string]*ManagedPlugin {
	result := make(map[string]*ManagedPlugin)
	if m == nil {
		return result
	}
	for _, root := range m.roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue // 扫描根不存在/不可读:跳过
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			pluginDir := filepath.Join(root, entry.Name())
			if !hasManifest(pluginDir) {
				continue
			}
			mp := loadManaged(pluginDir)
			if mp.Plugin == nil || mp.Plugin.Manifest == nil {
				continue // 致命失败已记录在 FatalError,跳过注册
			}
			if m.disabled[mp.Plugin.Manifest.Name] {
				mp.Disabled = true
				mp.Runtime = nil // 禁用:不创建运行时
			}
			result[mp.Plugin.Manifest.Name] = mp
		}
	}
	return result
}

// hasManifest 判断目录是否含常规 plugin.json(轻量探测,不解析)。
func hasManifest(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, manifestName))
	return err == nil && info.Mode().IsRegular()
}

// loadManaged 加载单个插件并按需创建运行时。
func loadManaged(pluginDir string) *ManagedPlugin {
	name := filepath.Base(pluginDir)
	dataDir := filepath.Join(config.GetMagicHome(), "agent-plugins-data", name)
	p := Load(pluginDir, dataDir)
	mp := &ManagedPlugin{Plugin: p}
	if !p.IsRejected() && !p.MCPDisabled && len(p.MCPServers) > 0 {
		mp.Runtime = NewRuntime(p)
	}
	return mp
}

// StartAll 启动所有受管插件的 MCP 运行时(跳过被禁用的插件)。
//
// 插件之间并行（上限 4）：每个插件的 Start 内部已经把条目并行化了，但
// "一个插件一台 npx server"是最常见的形态，插件再串行就等于没并行。
// 启动阶段这里直接决定端口多久能通，reload 阶段决定用户点完"重新扫描"
// 要转多久，两处都受益。
func StartAll(plugins map[string]*ManagedPlugin) {
	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for _, mp := range plugins {
		if mp.Disabled || mp.Runtime == nil {
			continue
		}
		wg.Add(1)
		go func(rt *Runtime) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_ = rt.Start()
		}(mp.Runtime)
	}
	wg.Wait()
}

// StopAll 停止所有受管插件的 MCP 运行时。
func StopAll(plugins map[string]*ManagedPlugin) {
	for _, mp := range plugins {
		if mp.Runtime != nil {
			mp.Runtime.Stop()
		}
	}
}

// AllSkills 收集所有受管插件中成功加载的 skills,按插件名分组返回。
// 被禁用的插件的 skills 不计入(禁用即不注入)。
// 集成层(如 skills.Manager)可据此注入插件来源的 skills。
func AllSkills(plugins map[string]*ManagedPlugin) []*SkillsGroup {
	var out []*SkillsGroup
	for name, mp := range plugins {
		if mp.Disabled {
			continue
		}
		g := &SkillsGroup{Plugin: name}
		for _, ref := range mp.Plugin.Skills {
			if ref.Skill != nil {
				g.Skills = append(g.Skills, ref.Skill)
			}
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Plugin < out[j].Plugin })
	return out
}

// SkillsGroup 按插件分组的 skills。
type SkillsGroup struct {
	Plugin string
	Skills []*skills.Skill
}

// RegisterAllTools 将所有受管插件的 MCP 工具注册到工具注册表(跳过被禁用的插件)。
func RegisterAllTools(plugins map[string]*ManagedPlugin, reg ToolRegistrar) {
	for name, mp := range plugins {
		if mp.Disabled || mp.Runtime == nil {
			continue
		}
		mp.Runtime.RegisterTools(reg, name)
	}
}

// UnregisterAllTools 将所有受管插件此前注册的工具从注册表注销。
// 在 reload/uninstall/disable 停用旧插件集之前调用,防止:
//   - 已停用 runtime 的僵尸工具残留(调用必然失败);
//   - 卸载后的插件工具仍出现在模型可用工具列表中。
func UnregisterAllTools(plugins map[string]*ManagedPlugin, reg ToolRegistrar) {
	for _, mp := range plugins {
		if mp.Runtime != nil {
			mp.Runtime.UnregisterTools(reg)
		}
	}
}

// Summary 返回所有插件的加载摘要(供 API/日志)。
func Summary(plugins map[string]*ManagedPlugin) []map[string]any {
	out := make([]map[string]any, 0, len(plugins))
	for name, mp := range plugins {
		p := mp.Plugin
		entry := map[string]any{
			"name":         name,
			"root":         p.Root,
			"data_dir":     p.DataDir,
			"version":      "",
			"description":  "",
			"rejected":     p.IsRejected(),
			"fatal_error":  p.FatalError,
			"mcp_disabled": p.MCPDisabled,
			"cli_disabled": p.CLIDisabled,
			"enabled":      !mp.Disabled,
			"warnings":     p.Warnings,
		}
		if p.Manifest != nil {
			entry["version"] = p.Manifest.Version
			entry["description"] = p.Manifest.Description
			entry["icon"] = p.Manifest.Icon
			entry["examples"] = p.Manifest.Examples
		}
		skillNames := make([]string, 0, len(p.Skills))
		for _, s := range p.Skills {
			if s.Skill != nil {
				skillNames = append(skillNames, s.Name)
			}
		}
		entry["skills"] = skillNames

		// 目录来源标记：从内置目录装出来的插件带着 kind/id，UI 据此把
		// "命令没装"的 CLI 连接器直接接到目录里的安装命令上。
		catKind, catID, fromCatalog := CatalogSource(p)
		if fromCatalog {
			entry["catalog_kind"] = catKind
			entry["catalog_id"] = catID
		}

		// 条目状态在后台连接期间持续变化，走 Runtime 的锁内快照；
		// 直接遍历 p.MCPServers 会与连接 goroutine 并发写。
		mcpStatuses := p.MCPServers
		if mp.Runtime != nil {
			mcpStatuses = mp.Runtime.EntryStatuses()
		}
		// 待填凭据的元数据（label / 去哪里获取）：与 required_env 同分组键，
		// 前端据此把"去获取"直链直接放在条目上，用户不必先打开表单才知道去哪拿。
		credFields := CredentialFields(p)
		mcpEntries := make([]map[string]any, 0, len(mcpStatuses))
		for _, ms := range mcpStatuses {
			e := map[string]any{
				"name":       ms.Name,
				"type":       ms.Spec.Type,
				"connected":  ms.Connected,
				"tools":      ms.ToolCount,
				"error":      ms.Error,
				"error_kind": ms.ErrorKind,
				// env 空值 + args/headers 里的 ${CRED:NAME} = 需要用户填写的凭据。
				"required_env": SpecCredentialKeys(ms.Spec),
			}
			if fields := credFields[ms.Name]; len(fields) > 0 {
				e["credential_fields"] = fields
			}
			// OAuth 视图：可用时前端渲染"登录/登出"，不必再让用户找 API key。
			if ms.OAuthAvailable {
				e["oauth_available"] = true
				e["oauth_state"] = ms.OAuthState
				if ms.OAuthError != "" {
					e["oauth_error"] = ms.OAuthError
				}
				if scopes := oauthScopes(ms.Spec); len(scopes) > 0 {
					e["oauth_scopes"] = scopes
				}
			}
			mcpEntries = append(mcpEntries, e)
		}
		entry["mcp_servers"] = mcpEntries

		// CLI 组件：只回显声明与校验结果，工具清单由工具名推导，
		// 不依赖运行时状态（CLI 没有连接这一层）。
		// 目录装出来的连接器额外带上本机安装状态与安装/登录命令：
		// "命令不存在"是 CLI 唯一的前置阻塞，UI 要能一键装（见 CLI 安装任务）。
		var catalogInstall, catalogLogin, catalogLoginExec []string
		if fromCatalog && catKind == CatalogKindCLI {
			if e, ok := clicatalog.Get(catID); ok {
				catalogInstall = e.Install.CommandsFor(runtime.GOOS)
				catalogLogin = e.Login.CommandsFor(runtime.GOOS)
				catalogLoginExec = append([]string(nil), e.LoginExec...)
			}
		}
		// 先并行预热，避免逐条 LookPath 串行把响应拖到秒级。
		cliCommands := make([]string, 0, len(p.CLIConnectors))
		for _, cs := range p.CLIConnectors {
			cliCommands = append(cliCommands, cs.Connector.Command)
		}
		PrimeCommands(cliCommands)

		cliEntries := make([]map[string]any, 0, len(p.CLIConnectors))
		for _, cs := range p.CLIConnectors {
			names := make([]string, 0, len(cs.Connector.Tools))
			for _, spec := range cs.Connector.Tools {
				names = append(names, fmt.Sprintf("ap_%s_%s", name, cliconn.ToolName(cs.Name, spec.Name)))
			}
			sort.Strings(names)
			cliEntry := map[string]any{
				"name":         cs.Name,
				"command":      cs.Connector.Command,
				"error":        cs.Error,
				"tools":        names,
				"required_env": envPlaceholderKeys(cs.Connector.Env),
				"binary_found": CommandAvailable(cs.Connector.Command),
			}
			if fields := credFields[CLICredentialGroup(cs.Name)]; len(fields) > 0 {
				cliEntry["credential_fields"] = fields
			}
			if fromCatalog && catKind == CatalogKindCLI {
				cliEntry["catalog_id"] = catID
				cliEntry["install_commands"] = catalogInstall
				cliEntry["login_commands"] = catalogLogin
				cliEntry["login_exec"] = catalogLoginExec
			}
			cliEntries = append(cliEntries, cliEntry)
		}
		entry["cli_connectors"] = cliEntries
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	return out
}

// emptyEnvKeys 返回 env 中值为空的 key 集合(排序保证稳定)。
// 约定:mcp.json 里值为 "" 的 env key 是"需要用户填写的凭据占位",
// 用户在授权表单里补值后经凭据合并生效。
func emptyEnvKeys(env map[string]string) []string {
	var keys []string
	for k, v := range env {
		if strings.TrimSpace(v) == "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
