package cliconn

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Tool 是 CLI 子命令在工具注册表里的适配器。
//
// 不实现 tool.Tool 的显式断言（那会给本包引入 internal/tool 依赖，而
// pkg/config 又要 import 本包，会成环）：Go 的接口是隐式的，注册表拿到的是
// 本类型指针，方法集对得上就满足 tool.Tool。
type Tool struct {
	connector       string
	conn            Connector
	spec            ToolSpec
	requireApproval bool
}

// NewTool 用连接器配置构造一个工具。
//
// 连接器与工具规范都在构造时做值拷贝：Manager 后续被替换/编辑时，已经在
// 注册表里的旧工具不会突然读到半新半旧的配置（下一次 sync 会整体换掉它们）。
func NewTool(connector string, conn Connector, spec ToolSpec) *Tool {
	return &Tool{
		connector:       connector,
		conn:            conn,
		spec:            spec,
		requireApproval: conn.ApprovalRequired(spec.ReadOnly),
	}
}

// Name 返回注册名 cli_<connector>_<tool>。
func (t *Tool) Name() string { return ToolName(t.connector, t.spec.Name) }

// Description 是模型看到的工具说明。
//
// 前缀带上"这是哪个程序的 CLI"，后缀带上实际执行的命令形状：模型据此判断
// 该用哪个工具，也能从错误里对上号。
func (t *Tool) Description() string {
	parts := make([]string, 0, 3)
	if label := strings.TrimSpace(t.conn.Description); label != "" {
		parts = append(parts, label)
	} else {
		parts = append(parts, t.conn.Command+" CLI")
	}
	if d := strings.TrimSpace(t.spec.Description); d != "" {
		parts = append(parts, d)
	}
	if shape := t.commandShape(); shape != "" {
		parts = append(parts, "Runs: "+shape)
	}
	return strings.Join(parts, "\n")
}

// commandShape 生成"命令长什么样"的说明（模板原样保留，模型能看出可填的参数）。
func (t *Tool) commandShape() string {
	argv := make([]string, 0, len(t.conn.Args)+len(t.spec.Args)+1)
	argv = append(argv, t.conn.Command)
	argv = append(argv, t.conn.Args...)
	argv = append(argv, t.spec.Args...)
	return truncate(strings.Join(argv, " "), 300)
}

// Schema 生成 OpenAI function calling 的 JSON Schema。
func (t *Tool) Schema() map[string]interface{} {
	props := make(map[string]interface{}, len(t.spec.Params))
	required := make([]string, 0, len(t.spec.Params))
	for _, p := range t.spec.Params {
		typ := p.Type
		if typ == "" {
			typ = "string"
		}
		prop := map[string]interface{}{"type": typ}
		if p.Description != "" {
			prop["description"] = p.Description
		}
		if len(p.Enum) > 0 {
			prop["enum"] = p.Enum
		}
		if p.Default != nil {
			prop["default"] = p.Default
		}
		props[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// Timeout 返回该工具的单次执行超时。
//
// 注册表要按工具设置超时（tool.Registry.SetTimeout），否则执行器会套上
// 默认的 60s —— 连接器里配的 timeout_seconds 就永远不生效。
func (t *Tool) Timeout() time.Duration {
	return time.Duration(t.conn.EffectiveTimeout(t.spec)) * time.Second
}

// Execute 执行子命令并返回结构化结果。
//
// 退出码非零不返回 error（见 Result 的注释），超时返回 124 + 提示，
// 只有"根本没跑起来"（命令不存在/无权限/参数校验失败）才是 error。
func (t *Tool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	timeout := t.Timeout()
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := Run(runCtx, t.conn, t.spec, params)
	if err != nil {
		return nil, err
	}

	out := map[string]interface{}{
		"command":     strings.Join(res.Argv, " "),
		"exit_code":   res.ExitCode,
		"stdout":      res.Stdout,
		"duration_ms": res.Duration.Milliseconds(),
	}
	if res.Stderr != "" {
		out["stderr"] = res.Stderr
	}
	if res.Truncated {
		out["truncated"] = true
		out["truncated_hint"] = fmt.Sprintf("output exceeded %d bytes and was truncated; narrow the query (filters, --limit, specific fields) instead of reading everything", DefaultMaxOutputBytes)
	}
	if res.StdinBytes > 0 {
		out["stdin_bytes"] = res.StdinBytes
	}
	if res.TimedOut {
		out["exit_code"] = 124
		out["timeout_seconds"] = int(timeout.Seconds())
		out["hint"] = "command timed out and was killed; add filters to narrow the work, or raise timeout_seconds in the connector config if this command is legitimately slow"
	}
	return out, nil
}

// RequiresApproval 实现 agent 侧的 approvalAware 接口（见下方 ApprovalSummary
// 的注释：两个方法必须成对实现）。
func (t *Tool) RequiresApproval() bool { return t.requireApproval }

// ApprovalSummary 给审批卡片一句人类可读的动作描述。
//
// **不要删这个方法**：agent 侧的 approvalAware 是
//
//	interface { RequiresApproval() bool; ApprovalSummary() string }
//
// 两个方法缺一个，t.(approvalAware) 就断言失败，agent 会认为"这工具不需要确认"
// 而直接放行——命令行能做的事没有边界，等于把审批链路整条绕过去。
// 这类失败是静默的（编译/单测都发现不了），所以 cliconn_test.go 里用同一形状的
// 接口做了编译期断言。
func (t *Tool) ApprovalSummary() string {
	return fmt.Sprintf("CLI %s → %s", t.connector, t.spec.Name)
}

// ConnectorName 返回所属连接器名（供测试接口/UI 反查）。
func (t *Tool) ConnectorName() string { return t.connector }

// Spec 返回工具声明（值拷贝）。
func (t *Tool) Spec() ToolSpec { return t.spec }

// Manager 持有 config.json 里的 CLI 连接器定义。
//
// 与 mcp.Manager 不同，这里没有"连接"这个概念：CLI 是无状态的，
// 每次调用都拉起一个新进程。因此 Manager 只负责定义的增删改与枚举，
// 不需要健康检查/重连/后台 goroutine。
type Manager struct {
	mu         sync.RWMutex
	connectors map[string]Connector
	// check 是准入白名单（nil = 全放行）。由外部注入，见 SetSecurityCheck。
	check func(name string, c Connector) error
	// blocked 记录因准入策略被挡下的连接器（name → 原因），供 UI/CLI 展示。
	blocked map[string]string
}

// New 用给定定义构造 Manager（nil/空都可以）。
func New(connectors map[string]Connector) *Manager {
	m := &Manager{connectors: make(map[string]Connector, len(connectors))}
	for name, c := range connectors {
		m.connectors[name] = c
	}
	return m
}

// Get 返回某个连接器的定义。
func (m *Manager) Get(name string) (Connector, bool) {
	if m == nil {
		return Connector{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.connectors[name]
	return c, ok
}

// Upsert 写入/替换一个连接器定义（先校验）。
func (m *Manager) Upsert(name string, c Connector) error {
	if err := ValidateConnectorName(name); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectors == nil {
		m.connectors = make(map[string]Connector)
	}
	m.connectors[name] = c
	return nil
}

// Delete 删除一个连接器定义，返回它此前是否存在。
func (m *Manager) Delete(name string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.connectors[name]
	delete(m.connectors, name)
	return ok
}

// SetSecurityCheck 注入准入校验（nil = 不校验）。
//
// 校验发生在"枚举工具"这一步，而不是每次执行：被策略拒绝的连接器根本不会
// 出现在模型可见的工具面里。让模型看见一个必然报错的工具，只会浪费回合。
func (m *Manager) SetSecurityCheck(fn func(name string, c Connector) error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.check = fn
	m.mu.Unlock()
}

// Blocked 返回被准入策略挡下的连接器（name → 原因）。
func (m *Manager) Blocked() map[string]string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.blocked) == 0 {
		return nil
	}
	out := make(map[string]string, len(m.blocked))
	for k, v := range m.blocked {
		out[k] = v
	}
	return out
}

// ToolsForProfile 枚举在给定分身下应当注册的全部工具（已按 enabled / profiles 过滤）。
func (m *Manager) ToolsForProfile(profile string) []*Tool {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]*Tool, 0, len(m.connectors))
	blocked := make(map[string]string)
	for _, name := range sortedKeys(m.connectors) {
		c := m.connectors[name]
		if !c.AutoConnect() || !c.AppliesToProfile(profile) {
			continue
		}
		if m.check != nil {
			if err := m.check(name, c); err != nil {
				blocked[name] = err.Error()
				continue
			}
		}
		for _, spec := range c.Tools {
			out = append(out, NewTool(name, c, spec))
		}
	}
	m.blocked = blocked
	return out
}

func sortedKeys(m map[string]Connector) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
