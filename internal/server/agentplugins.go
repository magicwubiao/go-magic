package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/magicwubiao/go-magic/internal/agentplugin"
)

// loadAgentPlugins 加载默认扫描目录下的所有 Agent Plugins,启动 MCP 运行时,
// 并把插件 skills / MCP tools 注入到 skills 管理器与工具注册表。
//
// 返回 (manager, plugins):manager 用于后续 reload,plugins 用于 API 摘要。
// 任何插件失败都不阻断整体加载(插件级失败隔离)。
// 被禁用的插件(配置 AgentPlugins.Disabled)不启动 MCP、不注入 skills/tools。
func (s *Server) loadAgentPlugins() (*agentplugin.Manager, map[string]*agentplugin.ManagedPlugin) {
	_ = agentplugin.EnsureDefaultScanDir()

	disabled := s.disabledAgentPlugins()
	mgr := agentplugin.NewManager([]string{agentplugin.DefaultScanDir()}, disabled)
	plugins := mgr.LoadAll()

	// 启动 MCP 连接(单 server 失败仅记录,不阻塞;禁用插件跳过)。
	agentplugin.StartAll(plugins)

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

	// 注入 MCP tools 到工具注册表(*tool.Registry 满足 agentplugin.ToolRegistrar)。
	if s.toolReg != nil {
		agentplugin.RegisterAllTools(plugins, s.toolReg)
	}

	return mgr, plugins
}

// disabledAgentPlugins 从配置读取被禁用的插件名集合。
func (s *Server) disabledAgentPlugins() map[string]bool {
	out := make(map[string]bool)
	if s.cfg == nil {
		return out
	}
	for _, n := range s.cfg.AgentPlugins.Disabled {
		out[n] = true
	}
	return out
}

// setPluginDisabled 更新配置中的禁用状态并持久化。
// disabled=true → 加入禁用列表;false → 从禁用列表移除。
func (s *Server) setPluginDisabled(name string, disabled bool) {
	if s.cfg == nil {
		return
	}
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
	_ = s.cfg.Save()
}

// handleAgentPlugins 暴露 Agent Plugins 的列表与重载 API。
//
//	GET    /api/agent-plugins              — 返回所有插件加载摘要
//	POST   /api/agent-plugins/reload       — 重新扫描并加载(先停旧的 MCP)
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
		s.mu.RLock()
		plugins := s.agentPlugins
		s.mu.RUnlock()
		if plugins == nil {
			jsonResponse(w, []map[string]any{})
			return
		}
		jsonResponse(w, agentplugin.Summary(plugins))
		return
	case rest == "reload" && r.Method == http.MethodPost:
		s.reloadAgentPlugins(w, r)
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
		}
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// reloadAgentPlugins 停止现有 MCP 运行时并重新加载。
func (s *Server) reloadAgentPlugins(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	old := s.agentPlugins
	s.mu.Unlock()
	if old != nil {
		agentplugin.StopAll(old)
	}

	_, plugins := s.loadAgentPlugins()

	s.mu.Lock()
	s.agentPlugins = plugins
	s.mu.Unlock()

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
		name = strings.TrimSuffix(header.Filename, ".zip")
		name = strings.TrimSuffix(name, ".ZIP")
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

	// 重新加载以激活新插件。
	s.mu.Lock()
	old := s.agentPlugins
	s.mu.Unlock()
	if old != nil {
		agentplugin.StopAll(old)
	}
	_, plugins := s.loadAgentPlugins()
	s.mu.Lock()
	s.agentPlugins = plugins
	s.mu.Unlock()

	jsonResponse(w, map[string]any{
		"ok":   true,
		"name": name,
		"dir":  dir,
	})
}

// uninstallAgentPlugin 卸载插件:先停其运行时,再删目录,再 reload。
func (s *Server) uninstallAgentPlugin(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 先从运行时映射中停止该插件的 MCP(若有)。
	s.mu.Lock()
	old := s.agentPlugins
	s.mu.Unlock()
	if old != nil {
		if mp, ok := old[name]; ok && mp.Runtime != nil {
			mp.Runtime.Stop()
		}
	}

	dir, err := agentplugin.Uninstall(name)
	if err != nil {
		http.Error(w, "uninstall failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 从禁用列表移除(已卸载的插件不应留在禁用列表)。
	s.setPluginDisabled(name, false)

	// 重新加载。
	if old != nil {
		agentplugin.StopAll(old)
	}
	_, plugins := s.loadAgentPlugins()
	s.mu.Lock()
	s.agentPlugins = plugins
	s.mu.Unlock()

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
	s.mu.RLock()
	plugins := s.agentPlugins
	s.mu.RUnlock()
	if plugins == nil {
		http.Error(w, "plugins not loaded", http.StatusServiceUnavailable)
		return
	}
	if _, ok := plugins[name]; !ok {
		http.Error(w, errors.New("plugin not found").Error(), http.StatusNotFound)
		return
	}

	s.setPluginDisabled(name, disabled)

	// 停止旧运行时并重新加载(禁用/启用需重建运行时)。
	s.mu.Lock()
	old := s.agentPlugins
	s.mu.Unlock()
	if old != nil {
		agentplugin.StopAll(old)
	}
	_, newPlugins := s.loadAgentPlugins()
	s.mu.Lock()
	s.agentPlugins = newPlugins
	s.mu.Unlock()

	jsonResponse(w, map[string]any{
		"ok":       true,
		"name":     name,
		"disabled": disabled,
	})
}
