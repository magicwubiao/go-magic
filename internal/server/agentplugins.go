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

	// 注入用户凭据(必须在 StartAll 之前:连接时要把凭据合并进子进程环境)。
	for name, mp := range plugins {
		if mp.Runtime == nil {
			continue
		}
		if creds := s.agentPluginCredentials(name); len(creds) > 0 {
			mp.Runtime.SetCredentials(creds)
		}
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
	if s.toolReg != nil {
		agentplugin.RegisterCLITools(plugins, s.toolReg, s.agentPluginCredentials)
	}

	return mgr, plugins
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
func (s *Server) agentPluginCredentials(name string) map[string]string {
	cfg := s.cfgSnapshot()
	if cfg == nil {
		return nil
	}
	return cfg.AgentPlugins.Credentials[name]
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

// handleAgentPlugins 暴露 Agent Plugins 的列表与重载 API。
//
//	GET    /api/agent-plugins              — 返回所有插件加载摘要
//	POST   /api/agent-plugins/reload       — 重新扫描并加载(先停旧的 MCP)
//	GET    /api/agent-plugins/catalog      — 目录插件包列表(kind=mcp|cli,含 installed)
//	POST   /api/agent-plugins/catalog/install — 目录条目物化成插件(body {"kind","id","args"})
//	POST   /api/agent-plugins/install      — 上传 zip 安装插件(multipart "file" + "name")
//	POST   /api/agent-plugins/{name}/uninstall  — 卸载并删除插件目录
//	POST   /api/agent-plugins/{name}/enable     — 启用插件
//	POST   /api/agent-plugins/{name}/disable    — 禁用插件
func (s *Server) handleAgentPlugins(w http.ResponseWriter, r *http.Request) {
	// 子路由:/install、/{name}/uninstall|enable|disable
	rest := strings.TrimPrefix(r.URL.Path, "/api/agent-plugins")
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
		case "mcp":
			// 形如 {name}/mcp/{server}/connect:重连插件的单个 MCP 条目。
			segs := strings.Split(rest, "/")
			if len(segs) == 4 && segs[3] == "connect" && r.Method == http.MethodPost {
				s.handlePluginServerConnect(w, r, segs[0], segs[2])
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

// installAgentPlugin 接收 zip 上传并安装插件。
//
// 期望 multipart 表单:
//
//	file: 插件 zip 文件(必填)
//	name: 插件目录名(可选;缺省时用 zip 文件名去掉 .zip 后缀)
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

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = header.Filename
		// 大小写不敏感地去掉 .zip 后缀。
		if l := len(name); l > 4 && strings.EqualFold(name[l-4:], ".zip") {
			name = name[:l-4]
		}
	}
	if name == "" {
		http.Error(w, "plugin name is empty", http.StatusBadRequest)
		return
	}

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

	dir, err := agentplugin.InstallFromZip(tmpPath, name)
	if err != nil {
		http.Error(w, "install failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 一致性检查:目录名 ≠ manifest.name 时只告警不阻断(规范允许),
	// 但后续卸载/启停均按 manifest.name 解析,用户应知晓。
	var warnings []string
	if m, merr := agentplugin.InspectManifest(dir); merr == nil && m != nil && m.Name != filepath.Base(dir) {
		warnings = append(warnings, fmt.Sprintf(
			"directory name %q differs from manifest.name %q; uninstall/enable/disable resolve by manifest.name",
			filepath.Base(dir), m.Name))
	}

	// 重新加载以激活新插件(串行化)。
	s.swapAgentPlugins()

	resp := map[string]any{
		"ok":   true,
		"name": name,
		"dir":  dir,
	}
	if len(warnings) > 0 {
		resp["warnings"] = warnings
	}
	jsonResponse(w, resp)
}

// uninstallAgentPlugin 卸载插件:按 manifest.name 定位已加载插件,解析出真实
// 目录名后删除,再统一走串行 reload 清理注入并重载。
//
// 顺序上必须「先停运行时再删目录」:stdio MCP server 的默认 cwd 在插件根内,
// 子进程存活时 Windows 会因目录句柄占用导致 RemoveAll 失败。
func (s *Server) uninstallAgentPlugin(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 整个卸载流程持有生命周期锁,避免与并发 reload/install 交错。
	s.agentPluginsMu.Lock()
	defer s.agentPluginsMu.Unlock()

	s.acquireServerMu("")
	old := s.agentPlugins
	s.releaseServerMu()

	// plugins map 以 manifest.name 为键,目录名可能与之不同;从已加载插件的
	// Root 推导真实目录名。清单致命被拒的插件不会出现在 map 中,此时退回
	// 用请求里的名字当目录名(仍在 InstalledDir 做路径安全校验)。
	dirName := name
	if mp, ok := old[name]; ok && mp != nil && mp.Plugin != nil && mp.Plugin.Root != "" {
		dirName = filepath.Base(mp.Plugin.Root)
	}
	if _, exists := agentplugin.InstalledDir(dirName); !exists {
		http.Error(w, "plugin not found", http.StatusNotFound)
		return
	}

	s.teardownAgentPluginsLocked()

	dir, err := agentplugin.Uninstall(dirName)
	if err != nil {
		// 目录删除失败:恢复加载状态,避免插件"半卸载"(运行时已停但仍在列表)。
		s.loadAndSwapLocked()
		http.Error(w, "uninstall failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 从禁用列表移除(禁用列表以 manifest.name 为键;已卸载的插件不应残留)。
	s.setPluginDisabled(name, false)

	s.loadAndSwapLocked()

	jsonResponse(w, map[string]any{
		"ok":   true,
		"name": name,
		"dir":  dir,
	})
}

// toggleAgentPlugin 启用/禁用插件并重新加载。
// disabled=true 表示禁用,false 表示启用。
func (s *Server) toggleAgentPlugin(w http.ResponseWriter, r *http.Request, name string, disabled bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 确认插件存在。
	s.acquireServerMu("")
	plugins := s.agentPlugins
	s.releaseServerMu()
	if plugins == nil {
		http.Error(w, "plugins not loaded", http.StatusServiceUnavailable)
		return
	}
	if _, ok := plugins[name]; !ok {
		http.Error(w, errors.New("plugin not found").Error(), http.StatusNotFound)
		return
	}

	// 启用前先看就绪度：缺凭据 / 命令没装时启用也一定调不通工具。
	// 回 409 + blockers，前端据此弹"去授权 / 去安装 / 仍然启用"；
	// 用户确认过就走 ?force=1 直接启用（比如命令会在下次终端会话里出现）。
	if !disabled {
		force := r.URL.Query().Get("force")
		if force != "1" && force != "true" {
			if blockers := s.pluginBlockers(name, plugins[name]); len(blockers) > 0 {
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

	s.setPluginDisabled(name, disabled)

	// 串行化停旧运行时并重载(禁用/启用需重建运行时、清理注入)。
	s.swapAgentPlugins()

	jsonResponse(w, map[string]any{
		"ok":       true,
		"name":     name,
		"disabled": disabled,
	})
}

// handlePluginCredentials 查看与保存插件的用户凭据(env key → value)。
//
//	GET  /api/agent-plugins/{name}/credentials — 掩码视图(值一律 ***)+
//	       required_env(每个 MCP 条目 mcp.json 里值为空的 key,即待填占位)。
//	POST /api/agent-plugins/{name}/credentials — body {"env":{KEY:VALUE}}。
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
				required["cli:"+cs.Name] = keys
			}
		}
		jsonResponse(w, map[string]any{
			"plugin":       name,
			"env":          env,
			"required_env": required,
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
//	POST /api/agent-plugins/{name}/credentials/auto
//
// 探测顺序：本机环境变量 → 常见别名（GITHUB_TOKEN ↔ GH_TOKEN …）→
// 已登录的官方 CLI（gh auth token）→ 其他插件里填过的同名凭据。
// 探测不到的 key 原样放在 missing 里，前端只为这几项弹表单——很多插件
// 在这一步就是 0 项，用户一次都不用填。
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
		entries = s.reconnectPluginEntries(mp)
		// CLI 工具在注册时把凭据烘进连接器，改完必须重注册（与手动授权同路径）。
		s.refreshPluginCLITools(name, mp)
	}

	rest := make([]string, 0, len(keys))
	for _, k := range keys {
		if strings.TrimSpace(merged[k]) == "" {
			rest = append(rest, k)
		}
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
	if cfg := s.cfgSnapshot(); cfg != nil {
		for pname, creds := range cfg.AgentPlugins.Credentials {
			if pname == self {
				continue
			}
			for k, v := range creds {
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

// setPluginCredentials 把插件凭据写进配置并持久化。
// 与 setPluginDisabled 相同的锁路径:全程 s.mu + cfgMu,原地改写 s.cfg。
func (s *Server) setPluginCredentials(name string, creds map[string]string) error {
	s.acquireServerMu("")
	defer s.releaseServerMu()
	s.lockCfgForWrite()
	defer s.unlockCfgForWrite()
	if s.cfg.AgentPlugins.Credentials == nil {
		s.cfg.AgentPlugins.Credentials = map[string]map[string]string{}
	}
	if len(creds) == 0 {
		delete(s.cfg.AgentPlugins.Credentials, name)
	} else {
		s.cfg.AgentPlugins.Credentials[name] = creds
	}
	return s.persistConfig(true)
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
	agentplugin.RegisterCLITools(one, s.toolReg, func(string) map[string]string { return creds })
}

// allowedIconExts 是可作插件图标的扩展名白名单:严格限定,防止借 icon 路径
// 读到凭据文件或任意文件。
var allowedIconExts = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml",
}

// handlePluginIcon 服务插件清单声明的图标文件。
//
//	GET /api/agent-plugins-icon/{name}
//
// 刻意不套 requireAuth:<img> 标签带不上 Authorization 头(与 /api/fs/serve
// 同理)。风险可控:路径必须落在该插件根目录内、扩展名在图片白名单内、
// 响应限流 4MB,最坏也只是泄露一张插件作者自己打包进去的图片。
func (s *Server) handlePluginIcon(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/agent-plugins-icon/")
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
