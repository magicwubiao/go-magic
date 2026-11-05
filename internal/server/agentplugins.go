package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// loadAgentPlugins 加载默认扫描目录下的所有 Agent Plugins,启动 MCP 运行时,
// 并把插件 skills / MCP tools 注入到 skills 管理器与工具注册表。
//
// 返回 (manager, plugins):manager 用于后续 reload,plugins 用于 API 摘要。
// 任何插件失败都不阻断整体加载(插件级失败隔离)。
// 被禁用的插件(配置 AgentPlugins.Disabled)不启动 MCP、不注入 skills/tools。
//
// 这是"完整加载"版本,reload / install / enable 等生命周期接口用它——调用方
// 需要在返回时插件工具已经可用。启动路径不走这里，见 loadAgentPluginsParsed
// 与 startAgentPluginRuntimes 的拆分说明。
func (s *Server) loadAgentPlugins() (*agentplugin.Manager, map[string]*agentplugin.ManagedPlugin) {
	mgr, plugins := s.loadAgentPluginsParsed()
	s.startAgentPluginRuntimes(plugins)
	return mgr, plugins
}

// loadAgentPluginsParsed 只做磁盘解析与纯内存注入：扫描插件目录、读取
// plugin.json / mcp.json / cli.json、注入 skills 与 CLI 工具。
//
// 不启动 MCP：连接要拉起子进程（npx 冷启动还要下载包）或做 HTTP 握手，
// 单条上限 30s，多台串行累加会直接把端口监听推到几十秒之后。
// 启动路径把这一步放后台，首个会话创建前再有界等待。
func (s *Server) loadAgentPluginsParsed() (*agentplugin.Manager, map[string]*agentplugin.ManagedPlugin) {
	_ = agentplugin.EnsureDefaultScanDir()

	disabled := s.disabledAgentPlugins()
	mgr := agentplugin.NewManager([]string{agentplugin.DefaultScanDir()}, disabled)
	plugins := mgr.LoadAll()

	// 注入 MCP 管理器 / 用户凭据 / OAuth（必须在 StartAll 之前：
	// 连接时要把凭据合并进子进程环境，远程条目才能"点一下登录"）。
	for _, mp := range plugins {
		s.preparePluginRuntime(mp)
	}

	// 注入 skills 到 skills 管理器(禁用插件跳过)。
	if s.skillMgr != nil {
		for _, g := range agentplugin.AllSkills(plugins) {
			for _, sk := range g.Skills {
				if sk.Source == "" {
					sk.Source = agentplugin.SkillSourceAgentPlugin
				}
				s.skillMgr.RegisterSkill(sk)
			}
		}
	}

	// 插件自带的 CLI 连接器(cli.json 组件):CLI 无连接阶段,注册即生效。
	// token source 工厂让 cli.json 里声明了 oauth 的连接器在每次执行前
	// 拿到本程序换来的 token（键带插件命名空间，见 agentplugin.CLIOAuthKey）。
	if s.toolReg != nil {
		agentplugin.RegisterCLIToolsWithOptions(plugins, s.toolReg, s.cliRegisterOptions())
	}

	return mgr, plugins
}

// preparePluginRuntime 给单个插件的运行时注入共享依赖。
//
// 三样缺一不可：MCP 管理器（连接、工具发现、懒连接）、OAuth 管理器
// （远程条目授权/刷新）、用户凭据（合并进子进程 env）。
func (s *Server) preparePluginRuntime(mp *agentplugin.ManagedPlugin) {
	if mp == nil || mp.Runtime == nil {
		return
	}
	mp.Runtime.SetMCPManager(s.pluginMCPMgr)
	mp.Runtime.SetOAuth(s.oauthManagerForPlugins())
	if creds := s.agentPluginCredentials(mp.Name()); len(creds) > 0 {
		mp.Runtime.SetCredentials(creds)
	}
}

// pluginMCPPolicy 是插件 MCP 条目的准入校验，注入给 pluginMCPMgr。
//
// 抽成方法而不是内联闭包，是为了让"插件入口确实接了 policy"这件事可以被
// 单测直接钉住（此前这个入口完全没有调用 CheckMCP，policy 只管连接器）。
func (s *Server) pluginMCPPolicy(name string, cfg mcp.ServerConfig) error {
	return s.policySnapshot().CheckMCP(name, cfg.Command, cfg.URL)
}

// cliRegisterOptions 组装插件 CLI 工具的注册参数（含准入校验）。
//
// policy 是"配置里写什么都不算数"的最后一道锁：插件是第三方内容，
// cli.json 的 command 完全由作者决定，不校验就等于"装个插件即可运行
// 本机任意程序"。
func (s *Server) cliRegisterOptions() agentplugin.CLIRegisterOptions {
	return agentplugin.CLIRegisterOptions{
		Creds:        s.agentPluginCredentials,
		TokenSources: s.pluginCLITokenSource,
		// 与全局连接器同一套 profiles 语义：插件里的 cli.json 也可能只在某个分身生效。
		Profile: s.activeProfile(),
		AllowCommand: func(connectorName, command string) error {
			return s.policySnapshot().CheckCLI(connectorName, command)
		},
	}
}

// startAgentPluginRuntimes 连接插件声明的 MCP server，并把发现的工具注册进
// 工具注册表。连接失败只记在条目状态上，不影响其它条目与启动。
//
// 幂等性：Start 对已连接的条目会再连一次，因此本方法只在"刚解析完"或
// "刚 reload 完"时调用，不要当成刷新接口用（刷新走 ReconnectEntry）。
func (s *Server) startAgentPluginRuntimes(plugins map[string]*agentplugin.ManagedPlugin) {
	if len(plugins) == 0 {
		return
	}
	began := time.Now()
	agentplugin.StartAll(plugins)
	if s.toolReg != nil {
		// 必须在 StartAll 之后：未连接的条目没有工具清单可注册。
		agentplugin.RegisterAllTools(plugins, s.toolReg)
	}
	if elapsed := time.Since(began); elapsed > time.Second {
		log.Infof("[AgentPlugin] MCP runtimes ready in %v (plugins=%d)", elapsed.Round(time.Millisecond), len(plugins))
	}
}

// disabledAgentPlugins 从配置读取被禁用的插件名集合。
func (s *Server) disabledAgentPlugins() map[string]bool {
	out := make(map[string]bool)
	// cfgSnapshot：调用方可能不持任何锁（loadAgentPlugins 在启动/重载路径），
	// 而 s.cfg 会被 reloadConfig 并发整体替换。
	cfg := s.cfgSnapshot()
	if cfg == nil {
		return out
	}
	for _, n := range cfg.AgentPlugins.Disabled {
		out[n] = true
	}
	return out
}

// agentPluginCredentials 读取指定插件的用户凭据(env key → value)。
// 只在连接路径使用,永远不要把返回值直接序列化给前端。
//
// 数据来自加密存储（见 agentplugin_creds.go），不再读 config.json。
func (s *Server) agentPluginCredentials(name string) map[string]string {
	if s.pluginCreds == nil {
		return nil
	}
	return s.pluginCreds.Load(name)
}

// setPluginCredentials 把插件凭据写进加密存储。
//
// 不再写 config.json：那个文件用户会手工编辑/备份/分享，明文 key 一旦
// 写进去就等于默认泄露（与 OAuth token 不进 config.json 是同一条纪律）。
func (s *Server) setPluginCredentials(name string, creds map[string]string) error {
	if s.pluginCreds == nil {
		return fmt.Errorf("credential store is not available")
	}
	return s.pluginCreds.Save(name, creds)
}

// setPluginDisabled 更新配置中的禁用状态并持久化。
// disabled=true → 加入禁用列表;false → 从禁用列表移除。
//
// 全程在 s.mu + cfgMu 内：这里原地改写 s.cfg.AgentPlugins 并 persistConfig
// （序列化整份 s.cfg）。调用方（卸载/启停插件两条路径）此前都不持锁，
// 裸改写就是数据竞态。cfgMu 与 s.mu 互不相干（setCfg 走 cfgMu），
// 只拿 s.mu 会被 CI 的 -race 判为 DATA RACE；cfgMu 是叶子锁。
func (s *Server) setPluginDisabled(name string, disabled bool) {
	s.acquireServerMu("")
	defer s.releaseServerMu()
	s.lockCfgForWrite()
	defer s.unlockCfgForWrite()
	// lockCfgForWrite 保证 s.cfg 非 nil（nil 时会补 DefaultConfig）。
	list := s.cfg.AgentPlugins.Disabled
	if disabled {
		found := false
		for _, n := range list {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			list = append(list, name)
		}
	} else {
		next := list[:0]
		for _, n := range list {
			if n != name {
				next = append(next, n)
			}
		}
		list = next
	}
	s.cfg.AgentPlugins.Disabled = list
	_ = s.persistConfig(true)
}

// swapAgentPlugins 串行执行插件生命周期变更共用的
// 「注销旧注入(skills/tools) → 停旧运行时 → 重新加载 → 换入」流程。
// reload / install / enable / disable 都经由它:
//   - agentPluginsMu 防止并发变更交错(孤儿 MCP 子进程、重复注入);
//   - 每次换入前先清掉上一轮注入的 skills/tools,避免卸载或禁用后残留
//     僵尸条目(旧 runtime 已停,工具调用必然失败)。
func (s *Server) swapAgentPlugins() map[string]*agentplugin.ManagedPlugin {
	s.agentPluginsMu.Lock()
	defer s.agentPluginsMu.Unlock()
	s.teardownAgentPluginsLocked()
	return s.loadAndSwapLocked()
}

// teardownAgentPluginsLocked 注销旧插件集注入的 skills/tools 并停止其运行时。
// 调用方必须持有 agentPluginsMu。
func (s *Server) teardownAgentPluginsLocked() {
	s.acquireServerMu("")
	old := s.agentPlugins
	s.releaseServerMu()
	if old == nil {
		return
	}
	s.unregisterAgentPluginArtifacts(old)
	agentplugin.StopAll(old)
}

// loadAndSwapLocked 重新扫描加载并换入 s.agentPlugins。调用方必须持有 agentPluginsMu。
func (s *Server) loadAndSwapLocked() map[string]*agentplugin.ManagedPlugin {
	_, plugins := s.loadAgentPlugins()
	s.acquireServerMu("")
	s.agentPlugins = plugins
	s.releaseServerMu()
	return plugins
}

// unregisterAgentPluginArtifacts 把旧插件集此前注入的 tools/skills 从
// 注册表中移除。skills 按 Source 匹配清理,不会误删同名非插件 skill。
// 必须在 StopAll 之前调用:UnregisterTools 依赖 runtime 记录的已注册工具名,
// 与连接状态无关,但语义上应与停用同批完成。
func (s *Server) unregisterAgentPluginArtifacts(plugins map[string]*agentplugin.ManagedPlugin) {
	if s.toolReg != nil {
		agentplugin.UnregisterAllTools(plugins, s.toolReg)
		// CLI 工具名可由插件定义直接推导,不需要运行时状态,单独清理一次。
		agentplugin.UnregisterCLITools(plugins, s.toolReg)
	}
	if s.skillMgr != nil {
		for _, mp := range plugins {
			if mp.Disabled || mp.Plugin == nil {
				continue // 禁用插件从未注入过 skills
			}
			for _, ref := range mp.Plugin.Skills {
				if ref.Skill != nil {
					s.skillMgr.UnregisterSkillIfSource(ref.Skill.Name, agentplugin.SkillSourceAgentPlugin)
				}
			}
		}
	}
}

// trimPluginsAPIPrefix 裁掉插件 API 前缀，兼容新旧两种：
// /api/plugins（新）与 /api/agent-plugins（旧，保留为兼容别名）。
// 命中任一前缀都返回前缀之后的子路径；都不命中则原样返回（调用方按空串处理为列表根）。
func trimPluginsAPIPrefix(p string) string {
	if rest := strings.TrimPrefix(p, "/api/plugins"); rest != p {
		return rest
	}
	return strings.TrimPrefix(p, "/api/agent-plugins")
}

// trimPluginsIconPrefix 裁掉图标 API 前缀，同样兼容新旧两种（见 trimPluginsAPIPrefix）。
func trimPluginsIconPrefix(p string) string {
	if rest := strings.TrimPrefix(p, "/api/plugins-icon/"); rest != p {
		return rest
	}
	return strings.TrimPrefix(p, "/api/agent-plugins-icon/")
}

// handleAgentPlugins 暴露 Agent Plugins 的列表与重载 API。
//
//	GET    /api/plugins              — 返回所有插件加载摘要
//	POST   /api/plugins/reload       — 重新扫描并加载(先停旧的 MCP)
//	GET    /api/plugins/catalog      — 目录插件包列表(kind=mcp|cli,含 installed)
//	POST   /api/plugins/catalog/install — 目录条目物化成插件(body {"kind","id","args"})
//	POST   /api/plugins/install      — 上传 zip 安装插件(multipart "file" + "name")
//	POST   /api/plugins/{name}/uninstall  — 卸载并删除插件目录
//	POST   /api/plugins/{name}/enable     — 启用插件
//	POST   /api/plugins/{name}/disable    — 禁用插件
func (s *Server) handleAgentPlugins(w http.ResponseWriter, r *http.Request) {
	// 子路由:/install、/{name}/uninstall|enable|disable
	rest := trimPluginsAPIPrefix(r.URL.Path)
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		s.acquireServerMu("")
		plugins := s.agentPlugins
		s.releaseServerMu()
		if plugins == nil {
			jsonResponse(w, []map[string]any{})
			return
		}
		list := agentplugin.Summary(plugins)
		// 补 ready / blockers：缺凭据、命令没装这两类"启用也用不了"的状态
		// 要在列表里就说清楚，而不是等用户点开详情看报错。
		s.decoratePluginSummaries(list, plugins)
		jsonResponse(w, list)
		return
	case rest == "reload" && r.Method == http.MethodPost:
		s.reloadAgentPlugins(w, r)
		return
	case rest == "catalog" && r.Method == http.MethodGet:
		s.listAgentPluginCatalog(w, r)
		return
	case rest == "catalog/install" && r.Method == http.MethodPost:
		s.installAgentPluginFromCatalog(w, r)
		return
	case rest == "cli/install" && r.Method == http.MethodPost:
		s.handleCLIInstallStart(w, r)
		return
	case strings.HasPrefix(rest, "cli/install/"):
		s.handleCLIInstallStatus(w, r, strings.TrimPrefix(rest, "cli/install/"))
		return
	case rest == "install" && r.Method == http.MethodPost:
		s.installAgentPlugin(w, r)
		return
	case strings.Contains(rest, "/"):
		parts := strings.SplitN(rest, "/", 2)
		name, action := parts[0], parts[1]
		// 注意 action 是 "/" 之后的**整段**（SplitN(rest, "/", 2)），
		// {name}/mcp/{server}/... 进来时 action = "mcp/{server}/..."。
		// 早先只在 switch 里写了 case "mcp"，导致 connect 路由永远匹配不到
		// （前端"重试"一直 405），OAuth 路由同理，这里改成前缀判断。
		if strings.HasPrefix(action, "mcp/") {
			segs := strings.Split(rest, "/")
			// {name}/mcp/{server}/connect — 重连单个条目。
			if len(segs) == 4 && segs[3] == "connect" && r.Method == http.MethodPost {
				s.handlePluginServerConnect(w, r, segs[0], segs[2])
				return
			}
			// {name}/mcp/{server}/oauth/{start|status|signout} — OAuth 授权。
			if len(segs) == 5 && segs[3] == "oauth" {
				s.handlePluginEntryOAuth(w, r, segs[0], segs[2], segs[4])
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// {name}/cli/{connector}/oauth/{start|status|signout} — 插件内 CLI 的
		// 设备码授权。与上面的 cli/install 不冲突：那条是 rest 级前缀，
		// 这里是 {name}/ 之后的第二段。
		if strings.HasPrefix(action, "cli/") {
			segs := strings.Split(rest, "/")
			if len(segs) == 5 && segs[3] == "oauth" {
				s.handlePluginCLIOAuth(w, r, segs[0], segs[2], segs[4])
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch action {
		case "uninstall":
			s.uninstallAgentPlugin(w, r, name)
			return
		case "enable":
			s.toggleAgentPlugin(w, r, name, false)
			return
		case "disable":
			s.toggleAgentPlugin(w, r, name, true)
			return
		case "credentials":
			s.handlePluginCredentials(w, r, name)
			return
		case "credentials/auto":
			s.handlePluginCredentialsAuto(w, r, name)
			return
		}
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// reloadAgentPlugins 停止现有 MCP 运行时并重新加载(串行化)。
func (s *Server) reloadAgentPlugins(w http.ResponseWriter, r *http.Request) {
	plugins := s.swapAgentPlugins()

	skillCount := 0
	for _, g := range agentplugin.AllSkills(plugins) {
		skillCount += len(g.Skills)
	}
	jsonResponse(w, map[string]any{
		"ok":     true,
		"count":  len(plugins),
		"dir":    agentplugin.DefaultScanDir(),
		"skills": skillCount,
	})
}

// ---- 单个插件的增量生命周期 ----
//
// 早先 install/enable/disable/uninstall 全部走 swapAgentPlugins：注销**所有**
// 插件注入 + StopAll + 重新 LoadAll + StartAll。装一个带 npx server 的插件，
// 会把其它插件的所有 stdio 子进程一起杀掉（丢会话状态、npx 冷启动重来），
// 而改凭据却做了精确的 ReconnectEntry——同一类操作两种成本。
//
// 现在只有用户显式"重新扫描"（reload）才全量重建；增删改单个插件只动它自己。

// pluginListSnapshot 返回当前插件表的只读快照（copy-on-write 语义：
// 表本身只在写锁内整体替换，读者拿到的 map 不会被后续写入改动）。
func (s *Server) pluginListSnapshot() map[string]*agentplugin.ManagedPlugin {
	s.acquireServerMu("")
	defer s.releaseServerMu()
	return s.agentPlugins
}

// pluginEntry 取单个插件。
func (s *Server) pluginEntry(name string) (*agentplugin.ManagedPlugin, bool) {
	s.acquireServerMu("")
	defer s.releaseServerMu()
	mp, ok := s.agentPlugins[name]
	return mp, ok
}

// putPluginEntry 写入/替换单个插件条目（复制整表后替换指针，读者不受影响）。
func (s *Server) putPluginEntry(name string, mp *agentplugin.ManagedPlugin) {
	s.acquireServerMu("")
	defer s.releaseServerMu()
	next := make(map[string]*agentplugin.ManagedPlugin, len(s.agentPlugins)+1)
	for k, v := range s.agentPlugins {
		next[k] = v
	}
	next[name] = mp
	s.agentPlugins = next
}

// dropPluginEntry 移除单个插件条目。
func (s *Server) dropPluginEntry(name string) {
	s.acquireServerMu("")
	defer s.releaseServerMu()
	if _, ok := s.agentPlugins[name]; !ok {
		return
	}
	next := make(map[string]*agentplugin.ManagedPlugin, len(s.agentPlugins))
	for k, v := range s.agentPlugins {
		if k == name {
			continue
		}
		next[k] = v
	}
	s.agentPlugins = next
}

// isPluginDisabled 报告插件是否在禁用列表里。
func (s *Server) isPluginDisabled(name string) bool {
	return s.disabledAgentPlugins()[name]
}

// findByDirName 按目录名查找已加载插件（目录名与 manifest.name 可能不同）。
func (s *Server) findByDirName(dirName string) (string, *agentplugin.ManagedPlugin) {
	for name, mp := range s.pluginListSnapshot() {
		if mp != nil && mp.DirName == dirName {
			return name, mp
		}
	}
	return "", nil
}

// activatePlugin 让一个刚解析出来的插件生效：注入 skills、注册 CLI 工具、
// 启动 MCP、注册 MCP 工具。只影响这一个插件。
func (s *Server) activatePlugin(mp *agentplugin.ManagedPlugin) {
	if mp == nil || mp.Plugin == nil {
		return
	}
	s.preparePluginRuntime(mp)

	if !mp.Disabled && s.skillMgr != nil {
		for _, ref := range mp.Plugin.Skills {
			if ref.Skill == nil {
				continue
			}
			if ref.Skill.Source == "" {
				ref.Skill.Source = agentplugin.SkillSourceAgentPlugin
			}
			s.skillMgr.RegisterSkill(ref.Skill)
		}
	}
	if mp.Disabled {
		return
	}

	one := map[string]*agentplugin.ManagedPlugin{mp.Name(): mp}
	if s.toolReg != nil {
		agentplugin.RegisterCLIToolsWithOptions(one, s.toolReg, s.cliRegisterOptions())
	}
	agentplugin.StartPlugin(mp)
	if s.toolReg != nil && mp.Runtime != nil {
		mp.Runtime.RegisterTools(s.toolReg, mp.Name())
	}
}

// deactivatePlugin 反向操作：注销该插件注入的 tools/skills，停止它的运行时。
// 其它插件的运行时与注册项不受影响。
func (s *Server) deactivatePlugin(mp *agentplugin.ManagedPlugin) {
	if mp == nil || mp.Plugin == nil {
		return
	}
	one := map[string]*agentplugin.ManagedPlugin{mp.Name(): mp}
	if s.toolReg != nil {
		agentplugin.UnregisterAllTools(one, s.toolReg)
		agentplugin.UnregisterCLITools(one, s.toolReg)
	}
	if s.skillMgr != nil {
		for _, ref := range mp.Plugin.Skills {
			if ref.Skill != nil {
				s.skillMgr.UnregisterSkillIfSource(ref.Skill.Name, agentplugin.SkillSourceAgentPlugin)
			}
		}
	}
	agentplugin.StopPlugin(mp)
}

// cleanupPluginState 清掉插件名下的所有持久状态：凭据、MCP 条目的 OAuth
// token、CLI 连接器的 OAuth token。
//
// 卸载不清这些会带来一个隐蔽问题：同名插件重新装回来时**静默继承**上一次
// 的凭据与授权——用户以为"重装=干净"，实际拿到的是旧身份；token 也会一直
// 留在磁盘上无人回收。
func (s *Server) cleanupPluginState(name string) {
	if name == "" {
		return
	}
	if s.pluginCreds != nil {
		s.pluginCreds.Delete(name)
	}
	if s.pluginMCPMgr != nil && s.pluginMCPMgr.OAuth() != nil {
		oauthMgr := s.pluginMCPMgr.OAuth()
		// MCP 条目：agentplugin:<plugin>/*
		oauthMgr.SignOutPrefix("agentplugin:" + name + "/")
		// CLI 连接器：cliconn:agentplugin/<plugin>/*
		oauthMgr.SignOutPrefix("cliconn:agentplugin/" + name + "/")
	}
}

// errPluginNotFound 表示插件目录不存在。
var errPluginNotFound = errors.New("plugin not found")

// removePlugin 卸载一个插件：停运行时 → 注销注入 → 删目录（可选清数据）→
// 清凭据与 token → 从禁用表移除。成功返回被删目录。
//
// 调用方必须持有 agentPluginsMu（与并发 install/reload 互斥）。
func (s *Server) removePlugin(name string, purge bool) (removedDir string, err error) {
	mp, ok := s.pluginEntry(name)

	dirName := name
	if ok && mp != nil && mp.DirName != "" {
		dirName = mp.DirName
	}
	if _, exists := agentplugin.InstalledDir(dirName); !exists {
		return "", errPluginNotFound
	}

	// 先停运行时再删目录：stdio 子进程的 cwd 在插件根内，Windows 上会因
	// 目录句柄占用导致 RemoveAll 失败。
	if ok {
		s.deactivatePlugin(mp)
		s.dropPluginEntry(name)
		// 工具缓存/懒连接登记也要清：否则下次启动会为一个已不存在的
		// server 注册一批点不动的影子工具。
		if s.pluginMCPMgr != nil && mp != nil && mp.Plugin != nil {
			for _, ms := range mp.Plugin.MCPServers {
				s.pluginMCPMgr.Forget(agentplugin.OAuthKey(name, ms.Name))
			}
		}
	}
	dir, rmErr := agentplugin.Uninstall(dirName, purge)
	if rmErr != nil {
		// 目录删不掉：把插件恢复回来，避免"运行时已停但列表还在"的半卸载态。
		if ok {
			s.putPluginEntry(name, mp)
			s.activatePlugin(mp)
		}
		return "", rmErr
	}

	s.cleanupPluginState(name)
	if dirName != name {
		// 目录名与 manifest 名不同时，两套键历史上都可能被写过。
		s.cleanupPluginState(dirName)
	}
	// 从禁用列表移除（禁用列表以 manifest.name 为键）。
	s.setPluginDisabled(name, false)
	return dir, nil
}

// installAgentPlugin 接收 zip 上传并安装（或升级）插件。
//
// 期望 multipart 表单:
//
//	file:    插件 zip 文件(必填)
//	name:    插件目录名(可选;缺省时取清单里的 manifest.name)
//	upgrade: "0"/"false" 时禁止覆盖已安装的同名插件（默认允许，即升级）
//
// 升级是**原子替换**：新内容先解压到隐藏临时目录，校验通过后整体换名；
// PLUGIN_DATA 在 plugins-data/ 下，不在替换范围内，因此升级不丢数据
// （这也正是"升级 = 卸载 + 重装"最不能接受的副作用）。
func (s *Server) installAgentPlugin(w http.ResponseWriter, r *http.Request) {
	// 32MB 内存缓冲,超出写临时文件。
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "invalid multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing 'file' field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 写到临时文件再交给 InstallFromZip(zip 需要 *os.File / path)。
	tmp, err := os.CreateTemp("", "agent-plugin-*.zip")
	if err != nil {
		http.Error(w, "create temp file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		http.Error(w, "save upload: "+err.Error(), http.StatusInternalServerError)
		return
	}
	tmp.Close()

	// 目录名：优先取表单，其次取清单（与 CLI 的 `--name` 默认行为一致）。
	// 早先 Web 用上传文件名，于是 "My-Plugin.zip" 会因大写字母被
	// validatePluginName 直接 400，而 CLI 装同一个包却成功。
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		m, merr := agentplugin.InspectArchiveManifest(tmpPath)
		if merr != nil {
			http.Error(w, "invalid plugin: "+merr.Error(), http.StatusBadRequest)
			return
		}
		name = m.Name
	}
	if header != nil && header.Filename != "" {
		log.Debugf("[AgentPlugin] installing %s as %q", header.Filename, name)
	}

	upgrade := true
	switch strings.ToLower(strings.TrimSpace(r.FormValue("upgrade"))) {
	case "0", "false", "no":
		upgrade = false
	}

	// 生命周期串行：安装/升级与并发的 reload/uninstall 交错会产生孤儿进程。
	s.agentPluginsMu.Lock()
	defer s.agentPluginsMu.Unlock()

	// 目标是"替换同名插件"时，先停掉旧运行时再动目录（见 removePlugin 注释）。
	var previous *agentplugin.ManagedPlugin
	var previousName string
	if old, ok := s.pluginEntry(name); ok {
		previous, previousName = old, name
	} else if oldName, old := s.findByDirName(name); old != nil {
		previous, previousName = old, oldName
	}
	if previous != nil {
		s.deactivatePlugin(previous)
		s.dropPluginEntry(previousName)
	}

	dir, manifest, err := agentplugin.InstallFromZip(tmpPath, agentplugin.InstallOptions{Name: name, Upgrade: upgrade})
	if err != nil {
		// 安装失败：把旧版本重新加载回来（如果刚才摘掉过）。
		if previous != nil {
			s.putPluginEntry(previousName, previous)
			s.activatePlugin(previous)
		}
		if errors.Is(err, agentplugin.ErrPluginExists) {
			jsonError(w, http.StatusConflict, "plugin already installed: pass upgrade=1 to replace it")
			return
		}
		http.Error(w, "install failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	reloaded := agentplugin.LoadDir(dir)
	if s.isPluginDisabled(reloaded.Name()) {
		reloaded.Disabled = true
		reloaded.Runtime = nil
	}
	s.putPluginEntry(reloaded.Name(), reloaded)
	s.activatePlugin(reloaded)

	resp := map[string]any{
		"ok":      true,
		"name":    reloaded.Name(),
		"dir":     dir,
		"version": manifest.Version,
		"upgrade": previous != nil,
	}
	var warnings []string
	if filepath.Base(dir) != manifest.Name {
		warnings = append(warnings, fmt.Sprintf(
			"directory name %q differs from manifest.name %q; credentials and disable state are stored under the manifest name",
			filepath.Base(dir), manifest.Name))
	}
	if reloaded.Rejected {
		warnings = append(warnings, "plugin manifest is invalid and the plugin will not load: "+reloaded.Plugin.FatalError)
	}
	if len(warnings) > 0 {
		resp["warnings"] = warnings
	}
	jsonResponse(w, resp)
}

// uninstallAgentPlugin 卸载插件：按名字取已加载插件，用它记录的目录名删目录
// （规范允许目录名 ≠ manifest.name；靠 Root 反推在清单被拒时只能拿到空串），
// 并清掉凭据与 OAuth token。
//
//	POST /api/plugins/{name}/uninstall?purge=1
func (s *Server) uninstallAgentPlugin(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	purge := false
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("purge"))) {
	case "1", "true", "yes":
		purge = true
	}

	s.agentPluginsMu.Lock()
	defer s.agentPluginsMu.Unlock()

	dir, err := s.removePlugin(name, purge)
	if err != nil {
		if errors.Is(err, errPluginNotFound) {
			http.Error(w, "plugin not found", http.StatusNotFound)
			return
		}
		http.Error(w, "uninstall failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	resp := map[string]any{"ok": true, "name": name, "dir": dir, "purged": purge}
	if !purge {
		// 明确告诉用户数据还在：默认保留 PLUGIN_DATA 是刻意的（重装/换版本
		// 不该丢索引），但也必须让人知道它在哪、怎么清掉。
		resp["data_kept"] = filepath.Join(config.GetMagicHome(), "plugins-data")
	}
	jsonResponse(w, resp)
}

// toggleAgentPlugin 启用/禁用插件。
//
// 只重建这一个插件的运行时：禁用 = 注销注入 + 停运行时；启用 = 从磁盘重新
// 解析（禁用期间文件可能被改过）后再注入。disabled=true 表示禁用。
func (s *Server) toggleAgentPlugin(w http.ResponseWriter, r *http.Request, name string, disabled bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mp, ok := s.pluginEntry(name)
	if !ok || mp == nil {
		http.Error(w, "plugin not found", http.StatusNotFound)
		return
	}

	// 启用前先看就绪度：缺凭据 / 命令没装时启用也一定调不通工具。
	// 回 409 + blockers，前端据此弹"去授权 / 去安装 / 仍然启用"；
	// 用户确认过就走 ?force=1 直接启用（比如命令会在下次终端会话里出现）。
	if !disabled {
		force := r.URL.Query().Get("force")
		if force != "1" && force != "true" {
			if blockers := s.pluginBlockers(name, mp); len(blockers) > 0 {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":    "plugin not ready",
					"plugin":   name,
					"blockers": blockers,
				})
				return
			}
		}
	}

	s.agentPluginsMu.Lock()
	defer s.agentPluginsMu.Unlock()

	s.setPluginDisabled(name, disabled)

	if disabled {
		s.deactivatePlugin(mp)
		// 用一个新结构替换旧值，而不是原地改：其它 goroutine 可能正持有
		// 旧指针读状态（原地改就是数据竞态）。
		off := *mp
		off.Disabled = true
		off.Runtime = nil
		s.putPluginEntry(name, &off)
	} else {
		dirName := mp.DirName
		if dirName == "" && mp.Plugin != nil {
			dirName = filepath.Base(mp.Plugin.Root)
		}
		dir, found := agentplugin.InstalledDir(dirName)
		if !found {
			http.Error(w, "plugin directory not found", http.StatusNotFound)
			return
		}
		reloaded := agentplugin.LoadDir(dir)
		reloaded.Disabled = false
		s.putPluginEntry(name, reloaded)
		s.activatePlugin(reloaded)
	}

	jsonResponse(w, map[string]any{
		"ok":       true,
		"name":     name,
		"disabled": disabled,
	})
}

// handlePluginCredentials 查看与保存插件的用户凭据(env key → value)。
//
//	GET  /api/plugins/{name}/credentials — 掩码视图(值一律 ***)+
//	       required_env(每个 MCP 条目 mcp.json 里值为空的 key,即待填占位)。
//	POST /api/plugins/{name}/credentials — body {"env":{KEY:VALUE}}。
//	       保存后立即合并进运行时并重连该插件所有条目,凭据补齐即刻生效。
//
// 掩码约定与 mcp_servers 的 env 一致:值只以 *** 回显,回传 *** 视为
// "保持原值",空串表示删除该 key。
func (s *Server) handlePluginCredentials(w http.ResponseWriter, r *http.Request, name string) {
	s.acquireServerMu("")
	plugins := s.agentPlugins
	s.releaseServerMu()
	mp, ok := plugins[name]
	if !ok || mp == nil || mp.Plugin == nil {
		http.Error(w, "plugin not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		stored := s.agentPluginCredentials(name)
		env := map[string]string{}
		for k := range stored {
			env[k] = maskedEnvValue
		}
		// 待填占位:值为空的 env 与 args/headers 里的 ${CRED:NAME} 都要出现在表单里。
		required := map[string][]string{}
		for _, ms := range mp.Plugin.MCPServers {
			keys := agentplugin.SpecCredentialKeys(ms.Spec)
			for _, k := range keys {
				if _, seen := env[k]; !seen {
					env[k] = ""
				}
			}
			required[ms.Name] = keys
		}
		// CLI 组件的凭据占位用 "cli:<connector>" 作分组键:
		// 与 MCP 条目同名时会覆盖后者的表单分组,前缀把两类分开。
		for _, cs := range mp.Plugin.CLIConnectors {
			keys := agentplugin.RequiredCLIEnv(cs.Connector)
			for _, k := range keys {
				if _, seen := env[k]; !seen {
					env[k] = ""
				}
			}
			if len(keys) > 0 {
				required[agentplugin.CLICredentialGroup(cs.Name)] = keys
			}
		}
		// 分组 → OAuth 可用性：能授权的分组直接给"一键授权"，用户不必去
		// 厂商页面搬 token。分组键与 fields / required_env 保持一致。
		oauthGroups := map[string]map[string]any{}
		if mp.Runtime != nil {
			for _, st := range mp.Runtime.EntryStatuses() {
				if !st.OAuthAvailable {
					continue
				}
				item := map[string]any{"available": true, "state": st.OAuthState}
				if st.OAuthError != "" {
					item["error"] = st.OAuthError
				}
				oauthGroups[st.Name] = item
			}
		}
		for _, cs := range mp.Plugin.CLIConnectors {
			spec := cs.Connector.OAuth
			// 没配 client_id/scopes 的声明不构成"可授权"：设备码流程
			// 起不来，这种分组仍走手填。
			if spec == nil || !spec.Configured() {
				continue
			}
			st := s.pluginCLIConnectorOAuthStatus(name, cs.Name)
			item := map[string]any{"available": true, "state": st.State}
			if st.Error != "" {
				item["error"] = st.Error
			}
			oauthGroups[agentplugin.CLICredentialGroup(cs.Name)] = item
		}

		jsonResponse(w, map[string]any{
			"plugin":       name,
			"env":          env,
			"required_env": required,
			// 每个 key 的 label / 获取地址：前端把有 help_url 的渲染成
			// "去获取"直链，用户点一下就到厂商的 token 页面。
			"fields": agentplugin.CredentialFields(mp.Plugin),
			// 分组 → OAuth 可用性：能授权的分组优先展示"一键授权"。
			"oauth": oauthGroups,
		})
		return

	case http.MethodPost:
		var body struct {
			Env map[string]string `json:"env"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			http.Error(w, "invalid json body: "+err.Error(), http.StatusBadRequest)
			return
		}

		// 掩码还原 + 空值删除,得到最终落盘的 key→value。
		stored := s.agentPluginCredentials(name)
		merged := map[string]string{}
		for k, v := range stored {
			merged[k] = v
		}
		for k, v := range body.Env {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if v == maskedEnvValue {
				continue // *** = 保持原值
			}
			if strings.TrimSpace(v) == "" {
				delete(merged, k) // 空串 = 清除
				continue
			}
			merged[k] = v
		}

		// 持久化:与 setPluginDisabled 同一把写锁路径。
		if err := s.setPluginCredentials(name, merged); err != nil {
			http.Error(w, "persist credentials: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// 注入运行时并重连全部条目:单条目失败不阻断其它条目,
		// 结果(含分类错误)在刷新后的 Summary 里可见。
		if mp.Runtime != nil {
			mp.Runtime.SetCredentials(merged)
		}
		results := s.reconnectPluginEntries(mp)
		// CLI 工具在注册时就把凭据烘进连接器配置了(不像 MCP 每次连接才合并),
		// 所以改完凭据必须重注册一遍,否则新 token 要等重启才生效。
		s.refreshPluginCLITools(name, mp)

		jsonResponse(w, map[string]any{
			"ok":      true,
			"plugin":  name,
			"entries": results,
		})
		return

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePluginCredentialsAuto 自动授权：能探测到的凭据直接填好并立即生效。
//
//	POST /api/plugins/{name}/credentials/auto
//
// 探测顺序：本机环境变量 → 常见别名（GITHUB_TOKEN ↔ GH_TOKEN …）→
// 已登录的官方 CLI（gh auth token）→ 其他插件里填过的同名凭据。
// 探测不到的 key 原样放在 missing 里，前端只为这几项弹表单——很多插件
// 在这一步就是 0 项，用户一次都不用填。
//
// filled 与 missing 同时为空（凭据早已齐全，或插件不声明静态凭据）时
// 不是"授权完成"，而是这次调用什么都没填：把条目重连一遍并返回真实
// 状态，前端据此如实报告（见下方 entries 分支的注释）。
//
// 安全：探测到的值只在服务端流转，响应里只有 key 与来源，没有值。
func (s *Server) handlePluginCredentialsAuto(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.acquireServerMu("")
	plugins := s.agentPlugins
	s.releaseServerMu()
	mp, ok := plugins[name]
	if !ok || mp == nil || mp.Plugin == nil {
		http.Error(w, "plugin not found", http.StatusNotFound)
		return
	}

	stored := s.agentPluginCredentials(name)
	keys := agentplugin.RequiredCredentialKeys(mp.Plugin)
	missing := make([]string, 0, len(keys))
	for _, k := range keys {
		if strings.TrimSpace(stored[k]) == "" {
			missing = append(missing, k)
		}
	}

	detected := agentplugin.DetectCredentials(missing)
	reuse := s.credentialReuse(name)

	merged := make(map[string]string, len(stored)+len(missing))
	for k, v := range stored {
		merged[k] = v
	}
	filled := make([]map[string]string, 0, len(missing))
	for _, k := range missing {
		if d, hit := detected[k]; hit && strings.TrimSpace(d.Value) != "" {
			merged[k] = d.Value
			filled = append(filled, map[string]string{"key": k, "source": d.Source})
			continue
		}
		if v, src, hit := reuse(k); hit {
			merged[k] = v
			filled = append(filled, map[string]string{"key": k, "source": src})
		}
	}

	entries := []map[string]any{}
	if len(filled) > 0 {
		if err := s.setPluginCredentials(name, merged); err != nil {
			http.Error(w, "persist credentials: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if mp.Runtime != nil {
			mp.Runtime.SetCredentials(merged)
		}
		// CLI 工具在注册时把凭据烘进连接器，改完必须重注册（与手动授权同路径）。
		s.refreshPluginCLITools(name, mp)
	}

	rest := make([]string, 0, len(keys))
	for _, k := range keys {
		if strings.TrimSpace(merged[k]) == "" {
			rest = append(rest, k)
		}
	}
	// filled 与 rest 同时为空有两种情况：所有声明的 key 早就填好了，或者
	// 该插件根本不声明静态凭据（如走 OAuth 的 mcp-remote 条目）。此时这次
	// 调用对凭据而言是个 no-op——绝不能就此让前端报"授权已自动完成"，
	// 必须把条目重连一遍，拿真实连接状态回给前端如实报告。对 mcp-remote
	// 这类 stdio 条目，重连恰好就是真正的授权入口：进程拉起后自己弹浏览器
	// 走 OAuth，用户点完"允许"条目才连得上。
	if len(filled) == 0 && len(rest) == 0 {
		entries = s.reconnectPluginEntries(mp)
	}
	jsonResponse(w, map[string]any{
		"ok":      true,
		"plugin":  name,
		"filled":  filled,
		"missing": rest,
		"entries": entries,
	})
}

// credentialReuse 返回"其他插件填过的同名凭据"的查找函数。
//
// 同一个 token 经常被多个插件共用（GitHub 的 MCP 插件与 gh CLI 插件都要
// GITHUB_TOKEN），填过一次就不该再填第二次。按 key 全名匹配，不做模糊映射。
func (s *Server) credentialReuse(self string) func(key string) (string, string, bool) {
	type hit struct{ plugin, value string }
	pool := map[string]hit{}
	if s.pluginCreds != nil {
		for _, pname := range s.pluginCreds.Plugins() {
			if pname == self {
				continue
			}
			for k, v := range s.pluginCreds.Load(pname) {
				if strings.TrimSpace(v) == "" {
					continue
				}
				if _, exists := pool[k]; !exists {
					pool[k] = hit{plugin: pname, value: v}
				}
			}
		}
	}
	return func(key string) (string, string, bool) {
		h, ok := pool[key]
		if !ok {
			return "", "", false
		}
		return h.value, "plugin:" + h.plugin, true
	}
}

// handlePluginServerConnect 重连插件的单个 MCP 条目(凭据之外的"重试"入口)。
func (s *Server) handlePluginServerConnect(w http.ResponseWriter, r *http.Request, name, server string) {
	s.acquireServerMu("")
	plugins := s.agentPlugins
	s.releaseServerMu()
	mp, ok := plugins[name]
	if !ok || mp == nil || mp.Plugin == nil {
		http.Error(w, "plugin not found", http.StatusNotFound)
		return
	}
	if mp.Runtime == nil {
		http.Error(w, "plugin has no MCP runtime (disabled or no mcp.json)", http.StatusConflict)
		return
	}
	if err := mp.Runtime.ReconnectEntry(server); err != nil {
		jsonResponse(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "plugin": name, "server": server})
}

// reconnectPluginEntries 重连插件的所有 MCP 条目,返回逐条目结果。
// 不持锁:ReconnectEntry 内部自管并发,且 stdio Close 可能等子进程退出,
// 攥着 server 锁做会挡住全部 API。
func (s *Server) reconnectPluginEntries(mp *agentplugin.ManagedPlugin) []map[string]any {
	if mp.Runtime == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(mp.Plugin.MCPServers))
	statuses := mp.Runtime.EntryStatuses()
	for _, ms := range statuses {
		err := mp.Runtime.ReconnectEntry(ms.Name)
		item := map[string]any{"name": ms.Name, "ok": err == nil}
		// 重连后状态从锁内快照取：连接在别的 goroutine 里也可能改这些字段。
		if st, ok := mp.Runtime.EntryStatus(ms.Name); ok {
			item["connected"] = st.Connected
			if err != nil {
				item["error_kind"] = st.ErrorKind
			}
		}
		if err != nil {
			item["error"] = err.Error()
		}
		out = append(out, item)
	}
	return out
}

// refreshPluginCLITools 用最新凭据重注册某个插件的 CLI 工具。
//
// 凭据在注册时被填进连接器配置(cliconn.Tool 构造时值拷贝),
// 与 MCP 的"每次连接才合并"不同,所以凭据变更必须重注册。
// 凭据快照在持 s.mu 之前取:cfgSnapshot 走 cfgMu,持 s.mu 再进它是反序。
func (s *Server) refreshPluginCLITools(pluginName string, mp *agentplugin.ManagedPlugin) {
	if s.toolReg == nil || mp == nil || mp.Plugin == nil {
		return
	}
	creds := s.agentPluginCredentials(pluginName)
	one := map[string]*agentplugin.ManagedPlugin{pluginName: mp}

	s.acquireServerMu("refreshPluginCLITools")
	defer s.releaseServerMu()

	agentplugin.UnregisterCLITools(one, s.toolReg)
	opts := s.cliRegisterOptions()
	opts.Creds = func(string) map[string]string { return creds }
	agentplugin.RegisterCLIToolsWithOptions(one, s.toolReg, opts)
}

// allowedIconExts 是可作插件图标的扩展名白名单:严格限定,防止借 icon 路径
// 读到凭据文件或任意文件。
var allowedIconExts = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml",
}

// handlePluginIcon 服务插件清单声明的图标文件。
//
//	GET /api/plugins-icon/{name}
//
// 刻意不套 requireAuth:<img> 标签带不上 Authorization 头(与 /api/fs/serve
// 同理)。风险可控:路径必须落在该插件根目录内、扩展名在图片白名单内、
// 响应限流 4MB,最坏也只是泄露一张插件作者自己打包进去的图片。
func (s *Server) handlePluginIcon(w http.ResponseWriter, r *http.Request) {
	name := trimPluginsIconPrefix(r.URL.Path)
	name = strings.Trim(name, "/")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		http.Error(w, "invalid plugin name", http.StatusBadRequest)
		return
	}

	s.acquireServerMu("")
	plugins := s.agentPlugins
	s.releaseServerMu()
	mp, ok := plugins[name]
	if !ok || mp == nil || mp.Plugin == nil || mp.Plugin.Manifest == nil || mp.Plugin.Manifest.Icon == "" {
		http.Error(w, "icon not found", http.StatusNotFound)
		return
	}

	icon := filepath.Clean(mp.Plugin.Manifest.Icon)
	if filepath.IsAbs(icon) || strings.HasPrefix(icon, "..") {
		http.Error(w, "invalid icon path", http.StatusBadRequest)
		return
	}
	ext := strings.ToLower(filepath.Ext(icon))
	ct, ok := allowedIconExts[ext]
	if !ok {
		http.Error(w, "icon must be an image file (png/jpg/jpeg/gif/webp/svg)", http.StatusBadRequest)
		return
	}
	full := filepath.Join(mp.Plugin.Root, icon)
	// 最终校验:解析后必须仍在插件根内(防符号链接/.. 变体逃逸)。
	if !strings.HasPrefix(full, filepath.Clean(mp.Plugin.Root)+string(filepath.Separator)) {
		http.Error(w, "icon escapes plugin root", http.StatusBadRequest)
		return
	}

	f, err := os.Open(full)
	if err != nil {
		http.Error(w, "icon not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 4<<20 {
		http.Error(w, "icon unreadable or too large", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeContent(w, r, filepath.Base(full), st.ModTime(), f)
}
