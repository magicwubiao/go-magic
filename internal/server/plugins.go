package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/plugins"
)

// 插件（plugin.json）接口。
//
//	GET    /api/plugins              已安装 + 本地目录里可安装的插件
//	POST   /api/plugins/install      安装（name 或 path 二选一）
//	DELETE /api/plugins/{name}       卸载
//
// 插件不是新的运行时概念：安装 = 把清单里的 connectors / mcp_servers 写进
// config.json，再走各自既有的注册与连接路径。所以这里没有任何"插件运行器"，
// 只有"把声明展开成配置"。
//
// 不做远端 marketplace：没有分发基建时那只会是半成品。目录来源是
// <magicHome>/plugins/<name>/plugin.json，用户自己放进去（或从仓库拷）。

// pluginView 是插件在列表里的展示形态。
type pluginView struct {
	plugins.Manifest
	// Installed 表示 config.json 里已经有这份清单写进去的东西。
	Installed bool `json:"installed"`
	// Source 是 plugin.json 的路径（已安装但目录被删时为空）。
	Source      string `json:"source,omitempty"`
	InstalledAt string `json:"installed_at,omitempty"`
	// Conflicts 是与现有配置重名的连接器（cli:<name> / mcp:<name>）。
	Conflicts []string `json:"conflicts,omitempty"`
	// MissingEnv 是声明了但环境里没有的变量。
	MissingEnv []string `json:"missing_env,omitempty"`
	// MissingSkills 是声明了但本机没找到的技能。
	MissingSkills []string `json:"missing_skills,omitempty"`
}

func (s *Server) pluginDir() string {
	return filepath.Join(s.magicHome, "plugins")
}

func (s *Server) pluginStore() *plugins.Store {
	return plugins.NewStore(filepath.Join(s.pluginDir(), "installed.json"))
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	discovered, errs := plugins.Discover(s.pluginDir())
	store := s.pluginStore()
	records, err := store.List()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byName := make(map[string]plugins.Record, len(records))
	for _, rec := range records {
		byName[rec.Name] = rec
	}

	cliCfgs := s.cliConnectorConfigs()
	mcpCfgs := s.mcpServerConfigs()

	out := make([]pluginView, 0, len(discovered)+len(records))
	seen := make(map[string]struct{}, len(discovered))
	for _, m := range discovered {
		seen[m.Name] = struct{}{}
		view := s.buildPluginView(m, cliCfgs, mcpCfgs, byName)
		out = append(out, view)
	}
	// 已安装但目录已被删掉的：仍然列出来，否则用户再也看不到怎么卸载。
	for _, rec := range records {
		if _, ok := seen[rec.Name]; ok {
			continue
		}
		view := s.buildPluginView(rec.Manifest, cliCfgs, mcpCfgs, byName)
		view.Source = rec.Source
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	resp := map[string]interface{}{
		"dir":     s.pluginDir(),
		"plugins": out,
	}
	if len(errs) > 0 {
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		resp["invalid"] = msgs
	}
	jsonResponse(w, resp)
}

func (s *Server) buildPluginView(
	m plugins.Manifest,
	cliCfgs map[string]cliconn.Connector,
	mcpCfgs map[string]mcp.ServerConfig,
	records map[string]plugins.Record,
) pluginView {
	view := pluginView{
		Manifest:      m,
		Conflicts:     m.Conflicts(cliCfgs, mcpCfgs),
		MissingEnv:    m.MissingEnv(),
		MissingSkills: s.missingSkills(m.Skills),
	}
	if rec, ok := records[m.Name]; ok {
		view.Installed = true
		view.InstalledAt = rec.InstalledAt.Format("2006-01-02 15:04:05")
	}
	return view
}

// missingSkills 用文件存在性判断技能是否可用。
//
// 不去实例化 skills.Manager：那会把整套技能目录扫一遍，而这里只需要回答
// "这个名字有没有东西"。真要用的时候 skills 侧自己有更准确的加载逻辑。
func (s *Server) missingSkills(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	var missing []string
	for _, name := range names {
		dir := filepath.Join(s.magicHome, "skills", name)
		if _, err := os.Stat(dir); err == nil {
			continue
		}
		// 也接受 <name>.md 这种扁平形态
		if _, err := os.Stat(filepath.Join(s.magicHome, "skills", name+".md")); err == nil {
			continue
		}
		missing = append(missing, name)
	}
	return missing
}

func (s *Server) handlePluginInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		// Name 从 <pluginDir>/<name>/plugin.json 找；Path 直接指向文件。
		Name string `json:"name"`
		Path string `json:"path"`
		// Overwrite 允许覆盖同名连接器。默认 409，避免把用户改过的配置冲掉。
		Overwrite bool `json:"overwrite"`
	}
	if err := decodeCLIBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	manifest, err := s.resolvePluginManifest(body.Name, body.Path)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	store := s.pluginStore()
	records, err := store.List()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	installed := make(map[string]bool, len(records))
	for _, rec := range records {
		installed[rec.Name] = true
	}
	var missingDeps []string
	for _, dep := range manifest.Requires {
		if !installed[dep] {
			missingDeps = append(missingDeps, dep)
		}
	}
	if len(missingDeps) > 0 {
		jsonError(w, http.StatusBadRequest,
			"missing required plugin(s): "+strings.Join(missingDeps, ", "))
		return
	}

	if missing := manifest.MissingEnv(); len(missing) > 0 {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf(
			"environment variable(s) not set: %s — export them (or start the server with them) before installing, then reference them as ${%s} in the config",
			strings.Join(missing, ", "), missing[0]))
		return
	}

	cliCfgs := s.cliConnectorConfigs()
	mcpCfgs := s.mcpServerConfigs()
	if conflicts := manifest.Conflicts(cliCfgs, mcpCfgs); len(conflicts) > 0 && !body.Overwrite {
		jsonError(w, http.StatusConflict, fmt.Sprintf(
			"connector(s) already exist: %s (pass overwrite:true to replace them)",
			strings.Join(conflicts, ", ")))
		return
	}

	// CLI 连接器：直接沿用列表接口的持久化与重建路径。
	if len(manifest.Connectors) > 0 {
		if err := s.persistCLIConnectors(manifest.Connectors, nil); err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save CLI connectors: %v", err))
			return
		}
		s.applyCLIConnectors()
	}

	// MCP 连接器：落盘后走 ConnectConfigured，让它自己决定"连"还是"懒"。
	if len(manifest.MCPServers) > 0 {
		if err := s.persistMCPServers(manifest.MCPServers, nil); err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save MCP servers: %v", err))
			return
		}
		s.refreshMCPToolVisibility()
		if s.mcpMgr != nil {
			servers := make(map[string]mcp.ServerConfig, len(manifest.MCPServers))
			for name, cfg := range manifest.MCPServers {
				servers[name] = cfg
			}
			go func() {
				defer func() {
					if rec := recover(); rec != nil {
						log.Errorf("[Plugins] panic while connecting MCP servers: %v", rec)
					}
				}()
				if err := s.mcpMgr.ConnectConfigured(servers); err != nil {
					log.Warnf("[Plugins] MCP servers of %q partially failed: %v", manifest.Name, err)
				}
				for name := range servers {
					s.syncMCPServerToRegistry(name)
				}
			}()
		}
	}

	source := strings.TrimSpace(body.Path)
	if source == "" {
		source = filepath.Join(s.pluginDir(), manifest.Name, plugins.ManifestFile)
	}
	if err := store.Put(plugins.Record{Manifest: manifest, Source: source}); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("plugin applied but could not be recorded: %v", err))
		return
	}

	resp := map[string]interface{}{
		"success":        true,
		"name":           manifest.Name,
		"skills":         manifest.Skills,
		"missing_skills": s.missingSkills(manifest.Skills),
	}
	names := make([]string, 0, len(manifest.Connectors))
	for name := range manifest.Connectors {
		names = append(names, "cli:"+name)
	}
	for name := range manifest.MCPServers {
		names = append(names, "mcp:"+name)
	}
	sort.Strings(names)
	resp["connectors"] = names
	jsonResponse(w, resp)
}

func (s *Server) handlePluginByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/plugins/")
	name = strings.TrimSuffix(name, "/")
	if name == "" {
		http.Error(w, "plugin name is required", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	store := s.pluginStore()
	rec, ok, err := store.Get(name)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("plugin %q is not installed", name))
		return
	}

	cliCfgs := s.cliConnectorConfigs()
	mcpCfgs := s.mcpServerConfigs()

	// 只删"还是插件当初写进去的那一份"：用户改过的配置留下，并在响应里说明。
	// 盲目按名字删会把用户的修改一起带走，这是卸载最容易造成数据损失的地方。
	var removed, kept []string
	removeCLI := make(map[string]cliconn.Connector)
	for cname, wanted := range rec.Connectors {
		current, exists := cliCfgs[cname]
		if !exists {
			continue
		}
		if reflect.DeepEqual(current, wanted) {
			removeCLI[cname] = current
			removed = append(removed, "cli:"+cname)
			continue
		}
		kept = append(kept, "cli:"+cname)
	}
	var removeMCP []string
	for sname, wanted := range rec.MCPServers {
		current, exists := mcpCfgs[sname]
		if !exists {
			continue
		}
		if reflect.DeepEqual(current, wanted) {
			removeMCP = append(removeMCP, sname)
			removed = append(removed, "mcp:"+sname)
			continue
		}
		kept = append(kept, "mcp:"+sname)
	}

	if len(removeCLI) > 0 || len(removeMCP) > 0 {
		if err := s.persistCLIConnectors(nil, keysOf(removeCLI)); err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to remove CLI connectors: %v", err))
			return
		}
		s.applyCLIConnectors()
		if err := s.persistMCPServers(nil, removeMCP); err != nil {
			jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to remove MCP servers: %v", err))
			return
		}
		for _, sname := range removeMCP {
			if s.mcpMgr != nil {
				_ = s.mcpMgr.Disconnect(sname)
			}
			s.removeMCPServerTools(sname)
		}
	}

	if _, _, err := store.Delete(name); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sort.Strings(removed)
	sort.Strings(kept)
	jsonResponse(w, map[string]interface{}{
		"success": true,
		"name":    name,
		"removed": removed,
		"kept":    kept,
	})
}

func (s *Server) resolvePluginManifest(name, path string) (plugins.Manifest, error) {
	if p := strings.TrimSpace(path); p != "" {
		m, err := plugins.LoadFile(p)
		if err != nil {
			return plugins.Manifest{}, err
		}
		if n := strings.TrimSpace(name); n != "" && n != m.Name {
			return plugins.Manifest{}, fmt.Errorf("manifest name %q does not match %q", m.Name, n)
		}
		return m, nil
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return plugins.Manifest{}, fmt.Errorf(`"name" or "path" is required`)
	}
	if err := plugins.ValidateName(name); err != nil {
		return plugins.Manifest{}, err
	}
	file := filepath.Join(s.pluginDir(), name, plugins.ManifestFile)
	if _, err := os.Stat(file); err != nil {
		return plugins.Manifest{}, fmt.Errorf("plugin %q not found in %s", name, s.pluginDir())
	}
	return plugins.LoadFile(file)
}

func keysOf(m map[string]cliconn.Connector) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
