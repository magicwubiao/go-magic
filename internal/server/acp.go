package server

// ACP（Agent Client Protocol）外部 agent 的 HTTP 管理面。
//
//	GET    /api/acp/agents                      列出 acp.json 里的 agent（env 掩码）
//	POST   /api/acp/agents                      新增（重名 409）
//	GET    /api/acp/agents/{name}               单个 agent
//	PUT    /api/acp/agents/{name}               修改（body.name 与路径不同时改名）
//	DELETE /api/acp/agents/{name}               删除
//	POST   /api/acp/agents/{name}/test          建连并返回协商结果（能力/会话/目录）
//	POST   /api/acp/agents/{name}/reset         丢弃会话上下文（新建 session）
//	POST   /api/acp/agents/{name}/disconnect    关闭缓存连接（保留配置）
//
// 与 MCP 连接器的区别：ACP 的配置在自己的 acp.json 里（internal/acp 负责读写），
// 不经过 config.json， поэтому这里没有 persistConfig / syncApprovalToMainConfig
// 那一套；每个请求都是"读磁盘 → 改 → 原子写回"。
//
// 两条约定：
//  1. env 值只以 *** 返回（与 mcp.json / cli.json 一致）。ACP 的 env 里放的是
//     ANTHROPIC_API_KEY / GEMINI_API_KEY 这类凭据，列表接口任何已登录浏览器都能读。
//  2. 值写成 `${VAR}` 的**不掩码**——它本身就是"值从哪来"，掩掉反而让人以为
//     配置被改了（见 pkg/envref 的说明）。真值在建连时才展开。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/acp"
	"github.com/magicwubiao/go-magic/pkg/envref"
)

// maxACPAgentBody 限制请求体大小：一个 agent 配置远小于 1MB。
const maxACPAgentBody = 1 << 20

// acpMaskedEnvValue 是 env 值的掩码占位符；回传该值时沿用磁盘上的旧值。
const acpMaskedEnvValue = "***"

// acpConfigMu 串行化 acp.json 的读-改-写。
//
// 不串行化就会丢改动：两个并发请求各自读到同一份旧配置，后写的把先写的抹掉
// （表现为"加完一个 agent，另一个就没了"）。包级互斥足够——只有本进程写这个文件。
var acpConfigMu sync.Mutex

// acpManager 返回 ACP 连接管理器。
//
// 生产路径用进程级单例 acp.Default()——工具层的 acp_* 用的也是它，
// 所以管理页看到的"已连接"与模型手里的连接是同一份。
// 测试可注入 s.acpMgr 指向临时配置，避免污染真实 acp.json。
func (s *Server) acpManager() *acp.Manager {
	if s != nil && s.acpMgr != nil {
		return s.acpMgr
	}
	return acp.Default()
}

// acpAgentView 是列表/详情条目形状。字段与 acp.AgentConfig 一一对应，
// 唯一差别是 env 值被掩码、多一个 connected 状态。
type acpAgentView struct {
	Name           string   `json:"name"`
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	Env            []string `json:"env,omitempty"`
	Cwd            string   `json:"cwd,omitempty"`
	Workspace      string   `json:"workspace,omitempty"`
	AdditionalDirs []string `json:"additional_dirs,omitempty"`
	Description    string   `json:"description,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	AllowWrite     bool     `json:"allow_write"`
	AutoApprove    []string `json:"auto_approve,omitempty"`
	Enabled        bool     `json:"enabled"`
	Connected      bool     `json:"connected"`
}

// acpAgentPayload 是写接口的请求体。
//
// Enabled 用 *bool：省略时按 true 处理（与配置文件的 nil=true 语义对齐），
// 直接用 bool 会让"老前端没带这个字段"变成静默关闭 agent。
type acpAgentPayload struct {
	Name           string   `json:"name"`
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	Env            []string `json:"env"`
	Cwd            string   `json:"cwd"`
	Workspace      string   `json:"workspace"`
	AdditionalDirs []string `json:"additional_dirs"`
	Description    string   `json:"description"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	AllowWrite     bool     `json:"allow_write"`
	AutoApprove    []string `json:"auto_approve"`
	Enabled        *bool    `json:"enabled"`
}

type acpAgentList struct {
	ConfigPath string         `json:"config_path"`
	Agents     []acpAgentView `json:"agents"`
	// ToolsAvailable 表示三个 acp_* 工具当前是否对模型可见（没有任何启用的
	// agent 时为 false）。前端据此提示"加一个 agent 才有工具"。
	ToolsAvailable bool `json:"tools_available"`
}

func (s *Server) handleACPAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.acpAgentListNow()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, list)
	case http.MethodPost:
		var payload acpAgentPayload
		if err := decodeACPAgentBody(r, &payload); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		name := strings.TrimSpace(payload.Name)
		if name == "" {
			jsonError(w, http.StatusBadRequest, "agent name is required")
			return
		}

		var conflict bool
		err := s.saveACPAgentConfig(func(cfg *acp.Config) error {
			if _, exists := cfg.Agents[name]; exists {
				conflict = true
				return fmt.Errorf("acp agent %q already exists", name)
			}
			agent := payload.toConfig(acp.AgentConfig{})
			if err := agent.Validate(name); err != nil {
				return err
			}
			cfg.Agents[name] = agent
			return nil
		})
		if err != nil {
			if conflict {
				jsonError(w, http.StatusConflict, err.Error())
				return
			}
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}

		s.acpManager().DropAgent(name)
		jsonResponse(w, map[string]interface{}{"ok": true, "name": name})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleACPAgentByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/acp/agents/")
	parts := strings.Split(path, "/")

	name, err := url.PathUnescape(parts[0])
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid agent name")
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		jsonError(w, http.StatusBadRequest, "agent name is required")
		return
	}

	// 子动作：只接受 POST。
	if len(parts) >= 2 {
		action := parts[1]
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch action {
		case "test":
			s.handleACPAgentTest(w, r, name)
		case "reset":
			s.handleACPAgentReset(w, r, name)
		case "disconnect":
			s.acpManager().DropAgent(name)
			jsonResponse(w, map[string]interface{}{"ok": true, "connected": false})
		default:
			jsonError(w, http.StatusNotFound, "unknown action: "+action)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		view, err := s.acpAgentViewByName(name)
		if err != nil {
			jsonError(w, http.StatusNotFound, err.Error())
			return
		}
		jsonResponse(w, view)
	case http.MethodPut:
		s.handleACPAgentUpdate(w, r, name)
	case http.MethodDelete:
		if err := s.saveACPAgentConfig(func(cfg *acp.Config) error {
			if _, ok := cfg.Agents[name]; !ok {
				return fmt.Errorf("acp agent %q not found", name)
			}
			delete(cfg.Agents, name)
			return nil
		}); err != nil {
			jsonError(w, http.StatusNotFound, err.Error())
			return
		}
		s.acpManager().DropAgent(name)
		jsonResponse(w, map[string]interface{}{"ok": true})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleACPAgentUpdate 覆盖式更新（表单提交完整配置）。
//
// body.name 与路径不同 = 改名：先删旧键再写新键，两个名字的缓存连接都要丢，
// 否则"改完名字旧连接还在跑、新名字又要连一次"。
func (s *Server) handleACPAgentUpdate(w http.ResponseWriter, r *http.Request, name string) {
	var payload acpAgentPayload
	if err := decodeACPAgentBody(r, &payload); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	newName := strings.TrimSpace(payload.Name)
	if newName == "" {
		newName = name
	}

	renamed := newName != name
	err := s.saveACPAgentConfig(func(cfg *acp.Config) error {
		old, ok := cfg.Agents[name]
		if !ok {
			return fmt.Errorf("acp agent %q not found", name)
		}
		if renamed {
			if _, exists := cfg.Agents[newName]; exists {
				return fmt.Errorf("acp agent %q already exists", newName)
			}
		}
		agent := payload.toConfig(old)
		if err := agent.Validate(newName); err != nil {
			return err
		}
		if renamed {
			delete(cfg.Agents, name)
		}
		cfg.Agents[newName] = agent
		return nil
	})
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		jsonError(w, status, err.Error())
		return
	}

	s.acpManager().DropAgent(name)
	if renamed {
		s.acpManager().DropAgent(newName)
	}
	jsonResponse(w, map[string]interface{}{"ok": true, "name": newName})
}

// handleACPAgentTest 建连并回传协商结果。
//
// 这是"真的连一次"，不是探活：拉子进程、initialize、session/new 全跑一遍，
// 成功后连接留在管理器里，下一次 acp_prompt 直接复用（不用等两次冷启动）。
func (s *Server) handleACPAgentTest(w http.ResponseWriter, r *http.Request, name string) {
	mgr := s.acpManager()

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	start := time.Now()
	client, err := mgr.Client(ctx, name)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}

	info := client.Info()
	agentInfo := map[string]interface{}{}
	if info.AgentInfo != nil {
		agentInfo = map[string]interface{}{
			"name":    info.AgentInfo.Name,
			"title":   info.AgentInfo.Title,
			"version": info.AgentInfo.Version,
		}
	}
	authMethods := make([]string, 0, len(info.AuthMethods))
	for _, m := range info.AuthMethods {
		authMethods = append(authMethods, m.ID)
	}
	caps := info.AgentCapabilities

	jsonResponse(w, map[string]interface{}{
		"ok":               true,
		"agent":            name,
		"elapsed_ms":       time.Since(start).Milliseconds(),
		"protocol_version": info.ProtocolVersion,
		"session_id":       client.SessionID(),
		"roots":            client.Roots(),
		"agent_info":       agentInfo,
		"auth_methods":     authMethods,
		"capabilities": map[string]interface{}{
			"load_session":     caps.LoadSession,
			"image":            caps.PromptCapabilities.Image,
			"audio":            caps.PromptCapabilities.Audio,
			"embedded_context": caps.PromptCapabilities.EmbeddedContext,
			"mcp_http":         caps.MCPCapabilities.HTTP,
			"mcp_sse":          caps.MCPCapabilities.SSE,
		},
	})
}

func (s *Server) handleACPAgentReset(w http.ResponseWriter, r *http.Request, name string) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	if err := s.acpManager().ResetSession(ctx, name); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, map[string]interface{}{"ok": true})
}

// ---------- 内部工具 ----------

func (s *Server) acpAgentListNow() (acpAgentList, error) {
	mgr := s.acpManager()
	cfg, err := mgr.Config()
	if err != nil {
		return acpAgentList{}, err
	}
	summaries, err := mgr.Agents()
	if err != nil {
		return acpAgentList{}, err
	}

	connected := make(map[string]bool, len(summaries))
	for _, s := range summaries {
		connected[s.Name] = s.Connected
	}

	names := make([]string, 0, len(cfg.Agents))
	for name := range cfg.Agents {
		names = append(names, name)
	}
	// 稳定顺序：UI 不做二次排序，列表顺序即展示顺序。
	sort.Strings(names)

	out := acpAgentList{
		ConfigPath: mgr.ConfigPath(),
		Agents:     make([]acpAgentView, 0, len(names)),
	}
	for _, name := range names {
		agent := cfg.Agents[name]
		out.Agents = append(out.Agents, newACPAgentView(name, agent, connected[name]))
		if agent.EnabledValue() {
			out.ToolsAvailable = true
		}
	}
	return out, nil
}

func (s *Server) acpAgentViewByName(name string) (acpAgentView, error) {
	mgr := s.acpManager()
	cfg, err := mgr.Config()
	if err != nil {
		return acpAgentView{}, err
	}
	agent, ok := cfg.Agents[name]
	if !ok {
		return acpAgentView{}, fmt.Errorf("acp agent %q not found", name)
	}

	connected := false
	summaries, err := mgr.Agents()
	if err == nil {
		for _, s := range summaries {
			if s.Name == name {
				connected = s.Connected
				break
			}
		}
	}
	return newACPAgentView(name, agent, connected), nil
}

func newACPAgentView(name string, agent acp.AgentConfig, connected bool) acpAgentView {
	return acpAgentView{
		Name:           name,
		Command:        agent.Command,
		Args:           agent.Args,
		Env:            maskACPEnv(agent.Env),
		Cwd:            agent.Cwd,
		Workspace:      agent.Workspace,
		AdditionalDirs: agent.AdditionalDirs,
		Description:    agent.Description,
		TimeoutSeconds: agent.TimeoutSeconds,
		AllowWrite:     agent.AllowWrite,
		AutoApprove:    agent.AutoApprove,
		Enabled:        agent.EnabledValue(),
		Connected:      connected,
	}
}

// maskACPEnv 把 env 值换成 ***。
// 纯 `${VAR}` 引用不掩码：那正是我们想让用户看到的形态（值不在磁盘上）。
func maskACPEnv(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(value) == "" || envref.IsReference(value) {
			out = append(out, entry)
			continue
		}
		out = append(out, key+"="+acpMaskedEnvValue)
	}
	return out
}

// mergeACPEnv 把客户端回传的 *** 还原成磁盘上的旧值。
//
// 不还原的话，用户只改超时就会把所有凭据写成字面量 "***"，
// 下次建连直接 401（MCP 的 env/headers 是同一套约定）。
func mergeACPEnv(incoming, old []string) []string {
	if len(incoming) == 0 {
		return nil
	}
	oldValues := make(map[string]string, len(old))
	for _, entry := range old {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			oldValues[strings.TrimSpace(key)] = value
		}
	}

	out := make([]string, 0, len(incoming))
	for _, entry := range incoming {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			out = append(out, entry)
			continue
		}
		key = strings.TrimSpace(key)
		if strings.TrimSpace(value) == acpMaskedEnvValue {
			prev, exists := oldValues[key]
			if !exists {
				// 磁盘上也没有这个键：写一个空值等于"声明了但没值"，
				// 直接丢掉更诚实（Validate 只要求 KEY=VALUE）。
				continue
			}
			out = append(out, key+"="+prev)
			continue
		}
		out = append(out, key+"="+value)
	}
	return out
}

func (p acpAgentPayload) toConfig(old acp.AgentConfig) acp.AgentConfig {
	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	return acp.AgentConfig{
		Command:        strings.TrimSpace(p.Command),
		Args:           trimSpaceList(p.Args),
		Env:            mergeACPEnv(p.Env, old.Env),
		Cwd:            strings.TrimSpace(p.Cwd),
		Workspace:      strings.TrimSpace(p.Workspace),
		AdditionalDirs: trimSpaceList(p.AdditionalDirs),
		Description:    strings.TrimSpace(p.Description),
		TimeoutSeconds: p.TimeoutSeconds,
		AllowWrite:     p.AllowWrite,
		AutoApprove:    trimSpaceList(p.AutoApprove),
		Enabled:        &enabled,
	}
}

// saveACPAgentConfig 在锁内完成"读磁盘 → 改 → 原子写回"。
func (s *Server) saveACPAgentConfig(mutate func(*acp.Config) error) error {
	acpConfigMu.Lock()
	defer acpConfigMu.Unlock()

	mgr := s.acpManager()
	cfg, err := mgr.Config()
	if err != nil {
		return err
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	return acp.SaveConfig(mgr.ConfigPath(), cfg)
}

func decodeACPAgentBody(r *http.Request, dst interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxACPAgentBody+1))
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	if len(body) > maxACPAgentBody {
		return fmt.Errorf("request body too large (max %d bytes)", maxACPAgentBody)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return fmt.Errorf("request body is empty")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

// trimSpaceList 去掉每项首尾空白并丢弃空项（表单里的多行文本常带空行）。
func trimSpaceList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
