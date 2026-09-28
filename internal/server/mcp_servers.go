package server

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/mcp"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// MCP 服务器管理接口。
//
//	GET    /api/mcp/servers                      列出已配置/已连接的服务器（env 掩码）
//	POST   /api/mcp/servers                      新增（支持 JSON 批量导入）
//	GET    /api/mcp/servers/{name}               单个服务器
//	PUT    /api/mcp/servers/{name}               upsert 单个服务器
//	DELETE /api/mcp/servers/{name}               断开并从 config.json 删除
//	POST   /api/mcp/servers/{name}/connect       连接（已配置即可）
//	POST   /api/mcp/servers/{name}/disconnect    断开（保留配置）
//	POST   /api/mcp/servers/{name}/reconnect     重连
//	POST   /api/mcp/servers/{name}/health        健康检查
//	GET|POST /api/mcp/servers/{name}/tools[/refresh]
//
// 两条重要约定：
//
//  1. 新增/编辑会写回 config.json 的 mcp.servers。"加完重启就没了"对一个管理
//     页面来说是最容易被当成 bug 的行为，而 magic mcp connect（CLI）本来就是
//     落盘的 —— 两条入口必须一致。
//  2. env 的值只以 *** 返回（沿用 bot env 的做法）。MCP 服务器的 env 里常年放着
//     API token，而任何一个已登录的浏览器都能读到这个接口。
//
// maxMCPImportBody 限制导入请求体大小：一屏 JSON 片段远小于 1MB。
const maxMCPImportBody = 1 << 20

// mcpServerView 是列表条目形状。
//
// 同时暴露"已配置"与"已连接"两个维度：连接失败或手动断开过的服务器仍然要出现
// 在列表里（否则用户看不到自己刚加的服务器，也没法编辑/重连），但它们并不在
// mcp.Manager 的 clients 里。
type mcpServerView struct {
	Name            string            `json:"name"`
	Transport       string            `json:"transport"`
	Connected       bool              `json:"connected"`
	Configured      bool              `json:"configured"`
	ToolCount       int               `json:"tool_count"`
	LastHealthCheck string            `json:"last_health_check,omitempty"`
	Command         string            `json:"command,omitempty"`
	Args            []string          `json:"args,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	URL             string            `json:"url,omitempty"`
}

func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, s.listMCPServers())
	case http.MethodPost:
		s.handleMCPAddServers(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMCPServerByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mcp/servers/")
	parts := strings.Split(path, "/")
	name := parts[0]

	if s.mcpMgr == nil {
		http.Error(w, "MCP manager not initialized", http.StatusInternalServerError)
		return
	}
	if name == "" {
		http.Error(w, "Server name is required", http.StatusBadRequest)
		return
	}

	if len(parts) >= 2 {
		switch parts[1] {
		case "connect", "reconnect":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleMCPConnect(w, name)
			return
		case "disconnect":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := s.mcpMgr.Disconnect(name); err != nil {
				jsonError(w, http.StatusInternalServerError, err.Error())
				return
			}
			// 桥接:断开后同步移除该 server 的 mcp_* 工具,避免模型调用失效工具。
			s.removeMCPServerTools(name)
			jsonResponse(w, map[string]bool{"success": true})
			return
		case "health":
			if r.Method == http.MethodPost {
				err := s.mcpMgr.HealthCheck(name)
				healthy := err == nil
				result := map[string]interface{}{
					"name":    name,
					"healthy": healthy,
				}
				if err != nil {
					result["error"] = err.Error()
				}
				jsonResponse(w, []map[string]interface{}{result})
				return
			}
			info, _ := s.mcpMgr.GetServerInfo(name)
			connected := info != nil && info["connected"] == true
			result := map[string]interface{}{
				"name":    name,
				"healthy": connected,
			}
			jsonResponse(w, []map[string]interface{}{result})
			return
		case "tools":
			if r.Method == http.MethodPost && len(parts) >= 3 && parts[2] == "refresh" {
				if err := s.mcpMgr.RefreshTools(name); err != nil {
					jsonError(w, http.StatusInternalServerError, err.Error())
					return
				}
				// 桥接:tools 清单变化后刷新注册表中的 mcp_* 工具。
				s.syncMCPServerToRegistry(name)
				tools, _ := s.mcpMgr.ListToolsByServer(name)
				jsonResponse(w, tools)
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			tools, err := s.mcpMgr.ListToolsByServer(name)
			if err != nil {
				jsonError(w, http.StatusInternalServerError, err.Error())
				return
			}
			jsonResponse(w, tools)
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		if view, ok := s.mcpServerViewByName(name); ok {
			jsonResponse(w, view)
			return
		}
		http.Error(w, "Server not found", http.StatusNotFound)
	case http.MethodPut:
		s.handleMCPUpsert(w, r, name)
	case http.MethodDelete:
		s.handleMCPDelete(w, name)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMCPHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	results := []map[string]interface{}{}
	if s.mcpMgr != nil {
		for _, name := range s.mcpMgr.ListServers() {
			err := s.mcpMgr.HealthCheck(name)
			healthy := err == nil
			result := map[string]interface{}{
				"name":    name,
				"healthy": healthy,
			}
			if err != nil {
				result["error"] = err.Error()
			}
			results = append(results, result)
		}
	}
	jsonResponse(w, results)
}

// handleMCPAddServers 处理 POST：接受批量 JSON 片段，也接受单个服务器的字段式
// body（{"name","transport","command","args","env","url"}）。
func (s *Server) handleMCPAddServers(w http.ResponseWriter, r *http.Request) {
	if s.mcpMgr == nil {
		jsonError(w, http.StatusInternalServerError, "MCP manager not initialized")
		return
	}
	body, err := readMCPBody(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	servers, err := mcp.ParseServersJSON(body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.applyMCPServers(w, servers)
}

// handleMCPUpsert 处理 PUT /api/mcp/servers/{name}。
//
// 这条路由以前完全没实现（落到 405），也就是说 Web 页面的"添加/保存"按钮其实
// 一直是坏的 —— 这里补齐，并让它承担 JSON 导入里"单个服务器"的语义。
func (s *Server) handleMCPUpsert(w http.ResponseWriter, r *http.Request, name string) {
	body, err := readMCPBody(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	bodyName, cfg, err := mcp.ParseServerConfigJSON(body, name)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if bodyName != name {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("server name %q in the body does not match the URL (%q)", bodyName, name))
		return
	}
	s.applyMCPServers(w, []mcp.NamedServerConfig{{Name: name, Config: cfg}})
}

func (s *Server) handleMCPDelete(w http.ResponseWriter, name string) {
	_, configured := s.mcpServerConfigs()[name]
	connected := false
	if s.mcpMgr != nil {
		if _, err := s.mcpMgr.GetServerInfo(name); err == nil {
			connected = true
		}
	}
	if !configured && !connected {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("MCP server %q not found", name))
		return
	}

	if connected {
		if err := s.mcpMgr.Disconnect(name); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	// 桥接:删除 server 后同步移除其 mcp_* 工具。
	s.removeMCPServerTools(name)

	if err := s.persistMCPServers(nil, []string{name}); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save config: %v", err))
		return
	}
	jsonResponse(w, map[string]interface{}{"status": "deleted", "name": name})
}

// handleMCPConnect 连接/重连：只要 config.json 里有配置就能连（以前 connect 直接
// 调 Reconnect，未连接过的服务器只会得到 "not found"）。
func (s *Server) handleMCPConnect(w http.ResponseWriter, name string) {
	cfg, configured := s.mcpServerConfigs()[name]
	if !configured {
		// 未落盘的连接（历史遗留/插件注入）仍然允许按原配置重连。
		if err := s.mcpMgr.Reconnect(name); err != nil {
			jsonError(w, http.StatusNotFound, fmt.Sprintf("MCP server %q is not configured: %v", name, err))
			return
		}
		s.syncMCPServerToRegistry(name)
		jsonResponse(w, map[string]bool{"success": true})
		return
	}
	if err := s.connectMCPServer(name, cfg); err != nil {
		// 连接失败是运行时问题（外部进程起不来/网络不通），保留 5xx 语义。
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]bool{"success": true})
}

// applyMCPServers 是新增/编辑的公共执行体：落盘 → 连接 → 同步注册表 → 报告结果。
//
// 顺序是刻意的：先落盘再连接。连接失败（npx 没装、URL 打错）时配置依然被保存，
// 用户能在列表里看到它并直接改正/重连；否则一次手滑就得从头填一遍。
func (s *Server) applyMCPServers(w http.ResponseWriter, servers []mcp.NamedServerConfig) {
	stored := s.mcpServerConfigs()

	upsert := make(map[string]mcp.ServerConfig, len(servers))
	for _, item := range servers {
		cfg := item.Config
		// 掩码还原：表单回填过的 *** 不能被当成真值写回去。
		cfg.Env = mergeMaskedServerEnv(stored[item.Name].Env, cfg.Env)
		upsert[item.Name] = cfg
	}

	resp := map[string]interface{}{}
	if err := s.persistMCPServers(upsert, nil); err != nil {
		// 仍然继续连接：这一次会话里能用，只是重启后要重新加一遍。
		resp["warning"] = fmt.Sprintf("servers could not be saved to config.json (they will be lost on restart): %v", err)
		log.Warnf("[MCP] failed to persist server config: %v", err)
	}

	added := make([]string, 0, len(servers))
	failed := make([]map[string]string, 0)
	for _, item := range servers {
		if err := s.connectMCPServer(item.Name, upsert[item.Name]); err != nil {
			failed = append(failed, map[string]string{"name": item.Name, "error": err.Error()})
			continue
		}
		added = append(added, item.Name)
	}

	// 单个服务器添加失败 → 沿用原有的"非 2xx + 错误原因"契约。这里刻意用 400
	// 而不是 500：前端 request() 会对 5xx 自动重试三次，而每次重试都会再 spawn
	// 一个 MCP 子进程，只会把一条明确的配置错误放大成三倍延迟。
	if len(servers) == 1 && len(failed) == 1 {
		jsonError(w, http.StatusBadRequest, failed[0]["error"])
		return
	}

	resp["success"] = len(failed) == 0
	resp["added"] = added
	resp["failed"] = failed
	if len(servers) == 1 {
		resp["name"] = servers[0].Name
	}
	resp["servers"] = s.listMCPServers()
	jsonResponse(w, resp)
}

// connectMCPServer 用给定配置连接（或重连）一个服务器，并把它的工具桥接进注册表。
func (s *Server) connectMCPServer(name string, cfg mcp.ServerConfig) error {
	if s.mcpMgr == nil {
		return fmt.Errorf("MCP manager not initialized")
	}
	// 同名连接先断开：ConnectStdio/ConnectSSE 对已存在的名字会直接报
	// "already connected"，而编辑配置后重连正是这条路径最常见的用法。
	// 未连接的返回 "not found"，属于正常情况。
	_ = s.mcpMgr.Disconnect(name)

	var err error
	if cfg.Transport == mcp.TransportSSE {
		err = s.mcpMgr.ConnectSSE(name, cfg)
	} else {
		err = s.mcpMgr.ConnectStdio(name, cfg)
	}
	if err != nil {
		s.removeMCPServerTools(name)
		return err
	}
	s.syncMCPServerToRegistry(name)
	return nil
}

// persistMCPServers 把 upsert 写入 config.json 的 mcp.servers，并删除 remove 里的名字。
//
// 先 reloadConfig：网关进程与 CLI 会写同一个 config.json，persistConfig 落的是
// 整份 s.cfg，直接拿内存快照全量覆盖会把它们的改动回退掉。
func (s *Server) persistMCPServers(upsert map[string]mcp.ServerConfig, remove []string) error {
	if s == nil {
		return nil
	}
	s.reloadConfig()
	s.mu.Lock()
	if s.cfg == nil {
		s.cfg = &appconfig.Config{}
	}
	if s.cfg.MCP == nil {
		s.cfg.MCP = &appconfig.MCPConfig{}
	}
	if s.cfg.MCP.Servers == nil {
		s.cfg.MCP.Servers = make(map[string]mcp.ServerConfig)
	}
	for name, cfg := range upsert {
		s.cfg.MCP.Servers[name] = cfg
	}
	for _, name := range remove {
		delete(s.cfg.MCP.Servers, name)
	}
	s.mu.Unlock()
	return s.persistConfig(true)
}

// mcpServerConfigs 返回 config.json 里配置的服务器（副本，调用方可随意改）。
func (s *Server) mcpServerConfigs() map[string]mcp.ServerConfig {
	out := make(map[string]mcp.ServerConfig)
	if s == nil {
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg == nil || s.cfg.MCP == nil {
		return out
	}
	for name, cfg := range s.cfg.MCP.Servers {
		out[name] = cfg
	}
	return out
}

func (s *Server) mcpServerViewByName(name string) (mcpServerView, bool) {
	for _, view := range s.listMCPServers() {
		if view.Name == name {
			return view, true
		}
	}
	return mcpServerView{}, false
}

// listMCPServers 合并"配置里有的"与"当前连着的"两类服务器，按名字排序
// （ListServers 走的是 map，顺序随机，列表每次刷新都会跳）。
func (s *Server) listMCPServers() []mcpServerView {
	configured := s.mcpServerConfigs()

	live := map[string]map[string]interface{}{}
	names := map[string]bool{}
	for name := range configured {
		names[name] = true
	}
	if s.mcpMgr != nil {
		for _, name := range s.mcpMgr.ListServers() {
			info, err := s.mcpMgr.GetServerInfo(name)
			if err != nil || info == nil {
				continue
			}
			live[name] = info
			names[name] = true
		}
	}

	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	out := make([]mcpServerView, 0, len(ordered))
	for _, name := range ordered {
		cfg, isConfigured := configured[name]
		view := mcpServerView{
			Name:       name,
			Configured: isConfigured,
			Transport:  cfg.Transport,
			Command:    cfg.Command,
			Args:       cfg.Args,
			Env:        maskServerEnv(cfg.Env),
			URL:        cfg.URL,
		}
		if view.Transport == "" {
			// 兜底：按字段推断，避免前端拿不到传输方式。
			switch {
			case cfg.Command != "":
				view.Transport = mcp.TransportStdio
			case cfg.URL != "":
				view.Transport = mcp.TransportSSE
			}
		}

		if info, ok := live[name]; ok {
			view.Connected, _ = info["connected"].(bool)
			if count, ok := info["tool_count"].(int); ok {
				view.ToolCount = count
			}
			if transport, ok := info["transport"].(string); ok && transport != "" {
				view.Transport = transport
			}
			if last, ok := info["last_health_check"].(time.Time); ok && !last.IsZero() {
				view.LastHealthCheck = last.Format(time.RFC3339)
			}
		}
		out = append(out, view)
	}
	return out
}

// maskServerEnv 只暴露 key，值一律替换成 ***（与 bot env 同一套约定）。
// 副作用是：如果某个环境变量的真实值恰好就是 "***"，编辑保存后会被还原成旧值。
func maskServerEnv(env []string) map[string]string {
	m := mcp.EnvSliceToMap(env)
	if len(m) == 0 {
		return nil
	}
	masked := make(map[string]string, len(m))
	for k := range m {
		masked[k] = maskedEnvValue
	}
	return masked
}

// mergeMaskedServerEnv 把回传的 *** 还原成磁盘上的原值；空输入表示"清空 env"。
func mergeMaskedServerEnv(stored, incoming []string) []string {
	if len(incoming) == 0 {
		return nil
	}
	storedMap := mcp.EnvSliceToMap(stored)

	out := make([]string, 0, len(incoming))
	for _, kv := range incoming {
		key, value, ok := strings.Cut(kv, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		if value == maskedEnvValue {
			if old, ok := storedMap[key]; ok {
				value = old
			}
		}
		out = append(out, key+"="+value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func readMCPBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxMCPImportBody+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	if len(body) > maxMCPImportBody {
		return nil, fmt.Errorf("request body too large (max %d bytes)", maxMCPImportBody)
	}
	return body, nil
}
