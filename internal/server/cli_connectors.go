package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/cliconn"
	"github.com/magicwubiao/go-magic/internal/cliconnbridge"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// CLI 连接器管理接口。
//
//	GET    /api/cli/connectors                列出全部连接器（env 掩码）
//	POST   /api/cli/connectors                新增/替换（body 里带 name）
//	GET    /api/cli/connectors/{name}         单个连接器
//	PUT    /api/cli/connectors/{name}         upsert
//	DELETE /api/cli/connectors/{name}         删除
//	POST   /api/cli/test                      按提交的定义试跑一次（不保存）
//
// 与 /api/mcp/servers 的三条约定保持一致：
//  1. 写操作落盘 config.json（"加完重启就没了"是最容易被当成 bug 的行为）；
//  2. env 的值一律以 *** 返回，回传的 *** 在保存时还原成磁盘真值；
//  3. 校验类错误同步返回 4xx —— 缺 command、模板引用未声明参数这类问题能立刻判定。

const maxCLIConnectorBody = 512 << 10

// cliConnectorView 是列表条目：定义 + 由定义推导出的注册名，方便前端展示。
type cliConnectorView struct {
	Name string `json:"name"`
	cliconn.Connector
	// ToolNames 是展开后的工具注册名（cli_<connector>_<tool>），
	// 前端拿它直接和工具页/日志里的名字对上号。
	ToolNames []string `json:"tool_names"`
	// Active 表示当前分身下是否生效（profiles / enabled 过滤后的结果）。
	Active bool `json:"active"`
}

func (s *Server) handleCLIConnectors(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, s.listCLIConnectors())
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
			cliconn.Connector
		}
		if err := decodeCLIBody(r, &body); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			jsonError(w, http.StatusBadRequest, `"name" is required`)
			return
		}
		s.upsertCLIConnector(w, body.Name, body.Connector)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCLIConnectorByID(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/cli/connectors/")
	name = strings.TrimSuffix(name, "/")
	if name == "" {
		http.Error(w, "Connector name is required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		for _, view := range s.listCLIConnectors() {
			if view.Name == name {
				jsonResponse(w, view)
				return
			}
		}
		jsonError(w, http.StatusNotFound, fmt.Sprintf("CLI connector %q not found", name))
	case http.MethodPut:
		var body cliconn.Connector
		if err := decodeCLIBody(r, &body); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.upsertCLIConnector(w, name, body)
	case http.MethodDelete:
		s.deleteCLIConnector(w, name)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCLITest 按提交的定义试跑一次，不改配置、不进注册表。
//
// 存在的理由：CLI 连接器最容易错的地方是"参数模板写歪了"——保存时无法发现，
// 只能等模型第一次调用才发现。这里让用户在表单里当场看到真实输出。
func (s *Server) handleCLITest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		// Name 是"已保存连接器"的名字（新建时为空）。带上它才能把表单回传的
		// *** 还原成磁盘上的真值——否则拿 *** 当 token 去跑，试跑必然 401。
		Name      string            `json:"name"`
		Connector cliconn.Connector `json:"connector"`
		Tool      string            `json:"tool"`
		Params    map[string]any    `json:"params"`
	}
	if err := decodeCLIBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if name := strings.TrimSpace(body.Name); name != "" {
		if stored, ok := s.cliConnectorConfigs()[name]; ok {
			body.Connector.Env = mergeMaskedServerEnv(stored.Env, body.Connector.Env)
		}
	} else {
		body.Connector.Env = mergeMaskedServerEnv(nil, body.Connector.Env)
	}
	if err := body.Connector.Validate(); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	var spec *cliconn.ToolSpec
	for i := range body.Connector.Tools {
		if body.Connector.Tools[i].Name == body.Tool {
			spec = &body.Connector.Tools[i]
			break
		}
	}
	if spec == nil {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("tool %q is not defined in this connector", body.Tool))
		return
	}

	timeout := time.Duration(body.Connector.EffectiveTimeout(*spec)) * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	start := time.Now()
	tool := cliconn.NewTool("test", body.Connector, *spec)
	out, err := tool.Execute(ctx, body.Params)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		// 试跑失败是正常的业务结果（命令没装、参数不合法），HTTP 仍 200，
		// 由 ok 字段表达结论 —— 与 /api/mcp/servers/{name}/test 一致。
		jsonResponse(w, map[string]interface{}{
			"ok":          false,
			"error":       err.Error(),
			"elapsed_ms":  elapsed,
			"timeout_sec": int(timeout.Seconds()),
		})
		return
	}

	ok := true
	if m, isMap := out.(map[string]interface{}); isMap {
		if code, isInt := m["exit_code"].(int); isInt && code != 0 {
			ok = false
		}
	}
	jsonResponse(w, map[string]interface{}{
		"ok":          ok,
		"elapsed_ms":  elapsed,
		"timeout_sec": int(timeout.Seconds()),
		"result":      out,
	})
}

// upsertCLIConnector 是新增/编辑的公共执行体：校验 → 落盘 → 更新内存 → 同步注册表。
func (s *Server) upsertCLIConnector(w http.ResponseWriter, name string, c cliconn.Connector) {
	if err := cliconn.ValidateConnectorName(name); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 掩码还原：表单回填过的 *** 不能当成真值写回去，否则用户改个超时时间
	// 就把 token 抹掉了。
	if stored, ok := s.cliConnectorConfigs()[name]; ok {
		c.Env = mergeMaskedServerEnv(stored.Env, c.Env)
	} else {
		c.Env = mergeMaskedServerEnv(nil, c.Env)
	}
	if err := c.Validate(); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.persistCLIConnectors(map[string]cliconn.Connector{name: c}, nil); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save config: %v", err))
		return
	}
	s.applyCLIConnectors()
	jsonResponse(w, map[string]interface{}{"success": true, "name": name})
}

func (s *Server) deleteCLIConnector(w http.ResponseWriter, name string) {
	if _, ok := s.cliConnectorConfigs()[name]; !ok {
		jsonError(w, http.StatusNotFound, fmt.Sprintf("CLI connector %q not found", name))
		return
	}
	if err := s.persistCLIConnectors(nil, []string{name}); err != nil {
		jsonError(w, http.StatusInternalServerError, fmt.Sprintf("failed to save config: %v", err))
		return
	}
	s.applyCLIConnectors()
	// applyCLIConnectors 已经把 cli_* 工具整体重同步过一遍（被删的不会重建），
	// 这里不再单独 RemoveConnectorTools —— 两次操作同一批工具只会让人误以为
	// 清理有两套逻辑。
	jsonResponse(w, map[string]interface{}{"status": "deleted", "name": name})
}

// applyCLIConnectors 用当前配置重建 Manager 并把工具同步进注册表。
//
// 整体替换而不是增量改：CLI 定义之间没有共享状态，重建成本是"拷一个 map"，
// 换来的是"任何一次改动后注册表必然等于配置"，不会漏掉被删/被禁用的工具。
func (s *Server) applyCLIConnectors() {
	if s == nil || s.toolReg == nil {
		return
	}
	defs := s.cliConnectorConfigs()
	mgr := cliconn.New(defs)

	// 两个页签同时保存、或保存与启动并发时，cliMgr 的赋值与注册表同步必须整体
	// 串行：否则 A 先把新工具注册进去、B 再用旧定义覆盖 cliMgr，注册表与
	// 内存定义就会长期不一致（要等下一次保存才自愈）。
	s.acquireServerMu("applyCLIConnectors")
	s.cliMgr = mgr
	count := cliconnbridge.SyncToRegistry(mgr, s.toolReg, s.activeProfile())
	s.releaseServerMu()

	log.Infof("[CLI] connectors synced: %d connectors, %d tools", len(defs), count)
}

// initCLIConnectors 在启动阶段加载 config.json 的 cli 段。
//
// 与 MCP 不同，这里没有握手/子进程，纯内存注册，因此同步执行即可。
func (s *Server) initCLIConnectors() {
	if s == nil || s.toolReg == nil {
		return
	}
	defs := s.cliConnectorConfigs()
	if len(defs) == 0 {
		return
	}
	s.applyCLIConnectors()
}

// cliConnectorConfigs 返回配置里的连接器定义（副本）。
func (s *Server) cliConnectorConfigs() map[string]cliconn.Connector {
	out := make(map[string]cliconn.Connector)
	cfg := s.cfgSnapshot()
	if cfg == nil || cfg.Cli == nil {
		return out
	}
	for name, c := range cfg.Cli.Connectors {
		out[name] = c
	}
	return out
}

// persistCLIConnectors 把 upsert 写进 config.json 的 cli.connectors，并删除 remove。
//
// 与 persistMCPServers 同一套处理：先 reloadConfig，避免把其他进程（网关/CLI）
// 刚写进去的改动整份覆盖掉。
func (s *Server) persistCLIConnectors(upsert map[string]cliconn.Connector, remove []string) error {
	if s == nil {
		return nil
	}
	s.reloadConfig()
	s.acquireServerMu("")
	defer s.releaseServerMu()
	s.lockCfgForWrite()
	defer s.unlockCfgForWrite()
	if s.cfg.Cli == nil {
		s.cfg.Cli = &cliconn.Config{}
	}
	if s.cfg.Cli.Connectors == nil {
		s.cfg.Cli.Connectors = make(map[string]cliconn.Connector)
	}
	for name, c := range upsert {
		s.cfg.Cli.Connectors[name] = c
	}
	for _, name := range remove {
		delete(s.cfg.Cli.Connectors, name)
	}
	// 空段清理：全部删完后不留一个 "cli": {} 的空壳。
	if len(s.cfg.Cli.Connectors) == 0 {
		s.cfg.Cli = nil
	}
	return s.persistConfig(true)
}

// listCLIConnectors 按名字排序返回视图（env 已掩码）。
func (s *Server) listCLIConnectors() []cliConnectorView {
	defs := s.cliConnectorConfigs()
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)

	profile := s.activeProfile()
	out := make([]cliConnectorView, 0, len(names))
	for _, name := range names {
		c := defs[name]
		c.Env = maskCLIEnv(c.Env)
		toolNames := make([]string, 0, len(c.Tools))
		for _, spec := range c.Tools {
			toolNames = append(toolNames, cliconn.ToolName(name, spec.Name))
		}
		out = append(out, cliConnectorView{
			Name:      name,
			Connector: c,
			ToolNames: toolNames,
			Active:    c.AutoConnect() && c.AppliesToProfile(profile),
		})
	}
	return out
}

// maskCLIEnv 只回显 key，值一律 ***（env 里放的是 token）。
func maskCLIEnv(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(key)+"="+maskedEnvValue)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeCLIBody(r *http.Request, dst interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCLIConnectorBody+1))
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	if len(body) > maxCLIConnectorBody {
		return fmt.Errorf("request body too large (max %d bytes)", maxCLIConnectorBody)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return fmt.Errorf("request body is empty")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}
