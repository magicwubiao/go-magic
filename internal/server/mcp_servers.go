package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/mcp"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/connectors"
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
//	PUT    /api/mcp/servers/{name}/tools/{tool}   单独启用/屏蔽工具
//	POST   /api/mcp/servers/{name}/test          试连（不改启用状态）
//	GET    /api/mcp/catalog                      内置连接器目录
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
	LastError       string            `json:"last_error,omitempty"`
	Command         string            `json:"command,omitempty"`
	Args            []string          `json:"args,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	URL             string            `json:"url,omitempty"`

	// State 是给 UI 用的状态机：connected / unauthorized / error /
	// disconnected / configured。相比 connected 布尔，它能让界面说出
	// "凭据有问题"而不是笼统的"连接失败"。
	State      string `json:"state"`
	ErrorKind  string `json:"error_kind,omitempty"`
	ErrorHint  string `json:"error_hint,omitempty"`
	ServerName string `json:"server_name,omitempty"`
	ServerVer  string `json:"server_version,omitempty"`
	Protocol   string `json:"protocol,omitempty"`

	// Enabled 表示启动时是否自动连接；nil 在 JSON 里序列化为 true 的等价物
	// （视图层总是给出明确布尔值）。
	Enabled         bool          `json:"enabled"`
	Profiles        []string      `json:"profiles,omitempty"`
	DisabledTools   []string      `json:"disabled_tools,omitempty"`
	RequireApproval bool          `json:"require_approval,omitempty"`
	OAuth           *mcpOAuthView `json:"oauth,omitempty"`
}

// mcpOAuthView 是连接器的 OAuth 状态视图。
//
// 客户端密钥（client_secret）**永远不会**出现在这里：列表接口会被任何已
// 登录的浏览器读到，密钥只在保存时以 *** 掩码参与合并。
type mcpOAuthView struct {
	// Enabled 标记该连接器配置了 OAuth（决定 UI 是否渲染登录按钮）。
	Enabled bool `json:"enabled"`
	// ClientID 只回显预配置的 id（非敏感）；动态注册出来的 id 不在此列。
	ClientID string   `json:"client_id,omitempty"`
	Scopes   []string `json:"scopes,omitempty"`
	// ClientSecret 掩码回显（*** 或空）。编辑表单原样回传时由
	// mergeMaskedOAuth 还原成磁盘真值——这与 env/headers 的约定一致，
	// 否则用户改个 scope 就会把密钥抹掉。
	ClientSecret string `json:"client_secret,omitempty"`
	// HasToken 表示本地是否存有该连接器的 OAuth token（不含值）。
	HasToken bool `json:"has_token"`
	// State 为 OAuthFlowStatus.State：authorized / awaiting_authorization /
	// failed / expired / none。
	State string `json:"state"`
	Error string `json:"error,omitempty"`
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
		case "test":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleMCPTest(w, name)
			return
		case "oauth":
			if len(parts) < 3 {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			switch parts[2] {
			case "start":
				if r.Method != http.MethodPost {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
					return
				}
				s.handleMCPOAuthStart(w, name)
				return
			case "status":
				if r.Method != http.MethodGet {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
					return
				}
				s.handleMCPOAuthStatus(w, name)
				return
			case "signout":
				if r.Method != http.MethodPost {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
					return
				}
				s.handleMCPOAuthSignout(w, name)
				return
			default:
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
		case "stats":
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			// 调用统计：让用户看出"这个连接器到底在不在用"。
			// 进程内累计，不落盘——重启后归零是可接受的，它不是审计数据。
			jsonResponse(w, s.mcpMgr.ToolStats(name))
			return
		case "tools":
			if r.Method == http.MethodPut && len(parts) >= 3 && parts[2] != "refresh" {
				// 工具名可能含空格/特殊字符（MCP 允许），路径段是转义过的。
				toolName, err := url.PathUnescape(parts[2])
				if err != nil {
					toolName = parts[2]
				}
				s.handleMCPToolToggle(w, r, name, toolName)
				return
			}
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

// applyMCPServers 是新增/编辑的公共执行体：落盘 → 后台连接 → 立即返回。
//
// 顺序是刻意的：先落盘再连接。连接失败（npx 没装、URL 打错）时配置依然被保存，
// 用户能在列表里看到它并直接改正/重连；否则一次手滑就得从头填一遍。
//
// **连接不再同步等待**。旧实现落盘后直接 connectMCPServer，而后者会阻塞在
// 第三方握手上（stdio 拉起子进程 / SSE 的 HTTP 往返），最坏贴满 initialize
// 的预算。前端 request() 有 30s AbortController，两端时间接近时用户看到的就是
// "点了保存一直转圈"。现在：
//
//   - 配置落盘后立刻返回，弹窗即时关闭、列表即时刷新（新 server 以"未连接"
//     状态出现，错误原因随后由后台连接写进 failures 表）；
//   - 连接在后台 goroutine 里进行，结果通过 failures 表 / 状态查询暴露，
//     与启动期 initStandaloneMCP 的异步模型保持一致。
//
// 唯一的例外是**校验类错误**（JSON 不合法、传输方式不支持）仍然同步返回 4xx，
// 因为那些能立刻判定，没有理由让用户等。
func (s *Server) applyMCPServers(w http.ResponseWriter, servers []mcp.NamedServerConfig) {
	stored := s.mcpServerConfigs()

	upsert := make(map[string]mcp.ServerConfig, len(servers))
	for _, item := range servers {
		cfg := item.Config
		// 掩码还原：表单回填过的 *** 不能被当成真值写回去。
		cfg.Env = mergeMaskedServerEnv(stored[item.Name].Env, cfg.Env)
		cfg.Headers = mergeMaskedServerHeaders(stored[item.Name].Headers, cfg.Headers)
		cfg.OAuth = mergeMaskedOAuth(stored[item.Name].OAuth, cfg.OAuth)
		upsert[item.Name] = cfg
	}

	resp := map[string]interface{}{}
	if err := s.persistMCPServers(upsert, nil); err != nil {
		// 仍然继续连接：这一次会话里能用，只是重启后要重新加一遍。
		resp["warning"] = fmt.Sprintf("servers could not be saved to config.json (they will be lost on restart): %v", err)
		log.Warnf("[MCP] failed to persist server config: %v", err)
	}

	// 后台连接：每个 server 一个 goroutine，互不阻塞。
	// 先刷新工具可见性：连接完成后的 registry 同步要按最新的 disabled_tools 过滤。
	s.refreshMCPToolVisibility()
	added := make([]string, 0, len(servers))
	for _, item := range servers {
		added = append(added, item.Name)
		s.connectMCPServerAsync(item.Name, upsert[item.Name])
	}

	resp["success"] = true
	resp["added"] = added
	resp["failed"] = []map[string]string{}
	resp["connecting"] = added
	if len(servers) == 1 {
		resp["name"] = servers[0].Name
	}
	resp["servers"] = s.listMCPServers()
	jsonResponse(w, resp)
}

// connectMCPServerAsync 在后台连接一个 server，并把结果同步到工具注册表。
//
// 失败不返回给本次调用者（HTTP 响应早已发出），而是通过两条既有渠道暴露：
// Manager 的 failures 表（列表接口的 last_error 字段读它）与 WARN 日志。
func (s *Server) connectMCPServerAsync(name string, cfg mcp.ServerConfig) {
	if s == nil || s.mcpMgr == nil {
		return
	}
	go func() {
		defer func() {
			// 后台 goroutine 的 panic 会带走整个进程，这里兜住。
			if r := recover(); r != nil {
				log.Errorf("[MCP] panic while connecting server %q: %v", name, r)
			}
		}()
		if err := s.connectMCPServer(name, cfg); err != nil {
			log.Warnf("[MCP] background connect failed for %q: %v", name, err)
			return
		}
		log.Infof("[MCP] background connect succeeded for %q", name)
	}()
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
	// http 与 sse 共用同一份远程传输实现（ConnectSSE 按响应 Content-Type
	// 分流），这里必须两个都放行——只认 sse 会把 transport:"http" 的配置
	// 掉进 stdio 分支，报 "exec: no command"。
	if cfg.Transport == mcp.TransportSSE || cfg.Transport == mcp.TransportHTTP {
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
	s.acquireServerMu("")
	defer s.releaseServerMu()
	s.lockCfgForWrite()
	defer s.unlockCfgForWrite()
	// lockCfgForWrite 保证 s.cfg 非 nil。
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
	return s.persistConfig(true)
}

// mcpServerConfigs 返回 config.json 里配置的服务器（副本，调用方可随意改）。
func (s *Server) mcpServerConfigs() map[string]mcp.ServerConfig {
	out := make(map[string]mcp.ServerConfig)
	if s == nil {
		return out
	}
	// 走快照而非"用 s.mu 包裸读"：s.cfg 的写入侧（setCfg）由 cfgMu 保护，
	// 与 s.mu 互不相干（CI `-race` 会报）。快照是不可变的，可自由遍历。
	cfg := s.cfgSnapshot()
	if cfg == nil || cfg.MCP == nil {
		return out
	}
	for name, c := range cfg.MCP.Servers {
		out[name] = c
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
			Name:            name,
			Configured:      isConfigured,
			Transport:       cfg.Transport,
			Command:         cfg.Command,
			Args:            cfg.Args,
			Env:             maskServerEnv(cfg.Env),
			Headers:         maskServerHeaders(cfg.Headers),
			URL:             cfg.URL,
			Enabled:         cfg.AutoConnect(),
			Profiles:        cfg.Profiles,
			DisabledTools:   cfg.DisabledTools,
			RequireApproval: cfg.RequireApproval,
			State:           mcp.StateConfigured,
		}
		if cfg.OAuth != nil {
			view.OAuth = s.buildMCPOAuthView(name, cfg.OAuth)
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
			if errMsg, ok := info["last_error"].(string); ok {
				view.LastError = errMsg
			}
			if st, ok := info["state"].(string); ok && st != "" {
				view.State = st
			}
			if kind, ok := info["error_kind"].(string); ok {
				view.ErrorKind = kind
			}
			if hint, ok := info["error_hint"].(string); ok {
				view.ErrorHint = hint
			}
			if sn, ok := info["server_name"].(string); ok {
				view.ServerName = sn
			}
			if sv, ok := info["server_version"].(string); ok {
				view.ServerVer = sv
			}
			if pv, ok := info["protocol"].(string); ok {
				view.Protocol = pv
			}
		}

		// 未连接的服务器没有 live client，失败原因记录在 Manager 的 failures 表里
		// （连接尝试失败时写入）。这样列表就能直接说清"为什么连不上"。
		if !view.Connected && view.LastError == "" && s.mcpMgr != nil {
			if rec := s.mcpMgr.LastFailureInfo(name); rec.Message != "" {
				view.LastError = rec.Message
				if view.ErrorKind == "" {
					view.ErrorKind = string(rec.Kind)
				}
				if view.ErrorHint == "" {
					view.ErrorHint = mcp.ErrorKindHint(rec.Kind)
				}
				view.State = stateFromKind(rec.Kind)
			}
		}

		// 已配置但从未尝试连接（或被手动断开）：状态是 configured/disconnected，
		// 不该显示成"错误"。
		if view.State == "" {
			view.State = mcp.StateConfigured
		}

		out = append(out, view)
	}
	return out
}

// stateFromKind 把错误分类映射成连接器状态。
func stateFromKind(kind mcp.ErrorKind) string {
	switch kind {
	case mcp.ErrKindAuth:
		return mcp.StateUnauthorized
	case "":
		return mcp.StateConfigured
	default:
		return mcp.StateError
	}
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

// mergeMaskedOAuth 把回传的 OAuth 配置里的 *** 密钥还原成磁盘真值。
//
// 与 headers 同一套约定：编辑表单回显时密钥被替换为 ***，用户没改就原样
// 回传，必须还原；incoming 为 nil 表示用户关闭了 OAuth（清掉整段配置）。
func mergeMaskedOAuth(stored, incoming *mcp.OAuthConfig) *mcp.OAuthConfig {
	if incoming == nil {
		return nil
	}
	// cloneOAuth 是 mcp 包的私有方法，这里手工浅拷贝 + 拷贝 scopes slice。
	cp := *incoming
	if len(incoming.Scopes) > 0 {
		cp.Scopes = append([]string(nil), incoming.Scopes...)
	}
	if cp.ClientSecret == maskedEnvValue {
		if stored != nil {
			cp.ClientSecret = stored.ClientSecret
		} else {
			// 之前没存过密钥，*** 是前端凭空填的，清掉。
			cp.ClientSecret = ""
		}
	}
	return &cp
}

// maskServerSecret 把 OAuth client_secret 掩码成 ***；空值返回空串，
// 前端靠空/非空区分"配置过密钥"和"没配过"。
func maskServerSecret(secret string) string {
	if secret == "" {
		return ""
	}
	return maskedEnvValue
}

// maskServerHeaders 与 maskServerEnv 同一套约定：只回显 key，值一律 ***。
// 认证头里放的是 token，列表接口会被任何已登录的浏览器读到。
func maskServerHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	masked := make(map[string]string, len(headers))
	for k := range headers {
		masked[k] = maskedEnvValue
	}
	return masked
}

// mergeMaskedServerHeaders 把回传的 *** 还原成磁盘上的原值。
//
// 与 env 不同，headers 是 map：这里按 key 逐个判断，回传里没有的 key 视为
// 已删除（用户点了那个 × ）。空 map / nil 表示清空全部自定义头。
func mergeMaskedServerHeaders(stored, incoming map[string]string) map[string]string {
	if len(incoming) == 0 {
		return nil
	}
	out := make(map[string]string, len(incoming))
	for k, v := range incoming {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if v == maskedEnvValue {
			if old, ok := stored[key]; ok {
				v = old
			}
		}
		out[key] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// handleMCPTest 试连一个已保存的连接器，不改变它的"启用"状态。
//
// 存在的理由：保存后是后台连接，失败原因要等列表刷新才出现；用户点"保存"
// 时并不知道凭据对不对。这个接口把"握手 + 工具发现"跑一遍并当场给出结果
// （工具数 / 耗时 / 错误分类），失败也算正常的业务结果——HTTP 始终 200，
// 由 ok 字段表达结论。
func (s *Server) handleMCPTest(w http.ResponseWriter, name string) {
	if s.mcpMgr == nil {
		jsonError(w, http.StatusInternalServerError, "MCP manager not initialized")
		return
	}
	cfg, configured := s.mcpServerConfigs()[name]
	if !configured {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("MCP server %q is not configured", name))
		return
	}

	start := time.Now()
	result := map[string]interface{}{"name": name}

	// 已连接的：只用一次 ping 验证链路，不动现有连接与工具表。
	if _, err := s.mcpMgr.GetServerInfo(name); err == nil {
		err := s.mcpMgr.HealthCheck(name)
		result["ok"] = err == nil
		result["elapsed_ms"] = time.Since(start).Milliseconds()
		if err != nil {
			result["error"] = err.Error()
			result["error_kind"] = string(mcp.KindOf(err))
			result["error_hint"] = mcp.ErrorKindHint(mcp.KindOf(err))
		} else {
			tools, _ := s.mcpMgr.ListToolsByServer(name)
			result["tool_count"] = len(tools)
		}
		jsonResponse(w, result)
		return
	}

	// 未连接：临时连一次，拿到结果后立刻拆掉，避免"测一下"把连接器
	// 意外点亮（用户以为只是试了试）。
	err := s.connectMCPServer(name, cfg)
	result["elapsed_ms"] = time.Since(start).Milliseconds()
	if err != nil {
		result["ok"] = false
		result["error"] = err.Error()
		result["error_kind"] = string(mcp.KindOf(err))
		result["error_hint"] = mcp.ErrorKindHint(mcp.KindOf(err))
		jsonResponse(w, result)
		return
	}
	tools, _ := s.mcpMgr.ListToolsByServer(name)
	_ = s.mcpMgr.Disconnect(name)
	s.removeMCPServerTools(name)
	result["ok"] = true
	result["tool_count"] = len(tools)
	jsonResponse(w, result)
}

// handleMCPOAuthStart 发起 OAuth 2.1 授权，返回浏览器要打开的 authorize URL。
//
// 前端拿到 URL 后 window.open，浏览器在本机完成授权，回调 listener 收到
// code 换 token，授权完成回调会自动重连该连接器。前端随后轮询
// oauth/status 把"等待授权"翻成"已授权"。
func (s *Server) handleMCPOAuthStart(w http.ResponseWriter, name string) {
	if s.mcpMgr == nil {
		jsonError(w, http.StatusInternalServerError, "MCP manager not initialized")
		return
	}
	cfg, configured := s.mcpServerConfigs()[name]
	if !configured {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("MCP server %q is not configured", name))
		return
	}
	mgr := s.mcpMgr.OAuth()
	if mgr == nil {
		jsonError(w, http.StatusInternalServerError, "OAuth token store is not initialized")
		return
	}

	// StartOAuthManual 而不是 StartOAuth：这是用户在 UI 里手动点"登录"的
	// 路径，前端自己会 window.open authorize URL；后端 onFlowStarted 弹
	// 浏览器的行为是给"连接时自动发起"的路径准备的，两边都开会双开标签页。
	authorizeURL, err := mgr.StartOAuthManual(name, cfg)
	if err != nil {
		// 授权失败不是 5xx：发现/注册失败都有明确的修复动作（改 URL / 手填
		// client_id），按 422 交给 UI 原样展示。
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	resp := map[string]interface{}{"status": mgr.Status(name)}
	if authorizeURL != "" {
		resp["authorize_url"] = authorizeURL
		resp["already_authorized"] = false
	} else {
		// 空串 + nil 错误 = 已有有效 token，幂等调用。
		resp["already_authorized"] = true
	}
	jsonResponse(w, resp)
}

// handleMCPOAuthStatus 供前端轮询。响应只含状态与错误信息，绝不含 token。
func (s *Server) handleMCPOAuthStatus(w http.ResponseWriter, name string) {
	mgr := s.mcpMgr.OAuth()
	if mgr == nil {
		jsonResponse(w, mcp.OAuthFlowStatus{State: "none"})
		return
	}
	jsonResponse(w, mgr.Status(name))
}

// handleMCPOAuthSignout 删除 token 并终止进行中的流程（登出）。
func (s *Server) handleMCPOAuthSignout(w http.ResponseWriter, name string) {
	mgr := s.mcpMgr.OAuth()
	if mgr == nil {
		jsonError(w, http.StatusInternalServerError, "OAuth token store is not initialized")
		return
	}
	if err := mgr.SignOut(name); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]interface{}{"status": mcp.OAuthFlowStatus{State: "none"}})
}

// buildMCPOAuthView 汇总连接器的 OAuth 状态（列表视图用）。
//
// 状态以 Manager 持有的 OAuthManager 为准（token 存储在那里）；测试环境
// Manager 的 oauth 为 nil 时退化为 none / 无 token。
func (s *Server) buildMCPOAuthView(name string, ocfg *mcp.OAuthConfig) *mcpOAuthView {
	view := &mcpOAuthView{
		Enabled:      true,
		ClientID:     ocfg.ClientID,
		Scopes:       ocfg.Scopes,
		ClientSecret: maskServerSecret(ocfg.ClientSecret),
		State:        "none",
	}
	if s.mcpMgr == nil {
		return view
	}
	mgr := s.mcpMgr.OAuth()
	if mgr == nil {
		return view
	}
	status := mgr.Status(name)
	view.State = status.State
	view.Error = status.Error
	// has_token 通过"状态是否 authorized/expired"间接体现：
	// authorized 一定有 token，expired 是有 token 但过期，其余为无。
	view.HasToken = status.State == mcp.OAuthStateAuthorized || status.State == "expired"
	return view
}

// handleMCPToolToggle 单独启用/屏蔽某个工具（PUT {enabled:bool}）。
//
// 为什么需要它：一个 server 动辄上百个工具，全部进 prompt 会把上下文吃掉
// 一大截。以前只能靠全局 tools.disabled 手写工具名，连接器页看不到入口。
func (s *Server) handleMCPToolToggle(w http.ResponseWriter, r *http.Request, name, tool string) {
	cfg, configured := s.mcpServerConfigs()[name]
	if !configured {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("MCP server %q is not configured", name))
		return
	}
	if strings.TrimSpace(tool) == "" {
		jsonError(w, http.StatusBadRequest, "tool name is required")
		return
	}

	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON body: %v", err))
		return
	}
	if body.Enabled == nil {
		jsonError(w, http.StatusBadRequest, `"enabled" is required`)
		return
	}

	disabled := make([]string, 0, len(cfg.DisabledTools))
	for _, t := range cfg.DisabledTools {
		if t != tool {
			disabled = append(disabled, t)
		}
	}
	if !*body.Enabled {
		disabled = append(disabled, tool)
	}
	cfg.DisabledTools = disabled

	if err := s.persistMCPServers(map[string]mcp.ServerConfig{name: cfg}, nil); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save config: %v", err))
		return
	}
	s.refreshMCPToolVisibility()
	s.syncMCPServerToRegistry(name)

	jsonResponse(w, map[string]interface{}{
		"success":        true,
		"name":           name,
		"tool":           tool,
		"enabled":        *body.Enabled,
		"disabled_tools": cfg.DisabledTools,
	})
}

// refreshMCPToolVisibility 把 config 里的 disabled_tools 汇总进 Manager。
//
// Manager 只负责"某个工具是否该隐藏"的判定，注册表侧（mcpbridge）在每次
// 同步时查询它，因此这里更新完必须让调用方跟着 sync 一次注册表。
func (s *Server) refreshMCPToolVisibility() {
	if s == nil || s.mcpMgr == nil {
		return
	}
	all := s.mcpServerConfigs()
	byServer := make(map[string][]string, len(all))
	for name, cfg := range all {
		if len(cfg.DisabledTools) > 0 {
			byServer[name] = cfg.DisabledTools
		}
	}
	s.mcpMgr.SetDisabledTools(byServer)
}

// handleMCPCatalog 返回内置连接器目录（可搜索）。
//
//	GET /api/mcp/catalog?q=github&category=dev
//
// 目录只是"模板"：点安装时前端把模板展开成一份 ServerConfig 提交到
// POST /api/mcp/servers，认证字段（env/headers）由用户填。这样目录不必
// 关心凭据，也不会把任何 token 固化在代码里。
func (s *Server) handleMCPCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	jsonResponse(w, map[string]interface{}{
		"connectors": connectors.Search(q, category),
		"categories": connectors.Categories(),
	})
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
