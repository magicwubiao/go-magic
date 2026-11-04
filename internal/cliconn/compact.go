package cliconn

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CompactToolName 是 compact 模式调度工具的保留短名（全名 cli_<connector>_call）。
const CompactToolName = "call"

// CompactTool 把一个连接器的全部声明工具收成**一个**调度入口。
//
// 为什么需要它：每个声明工具在模型上下文里都是一份完整的 name + description
// + JSON Schema。一个连接器声明 5 条子命令，装三个连接器就是十几个定义常驻
// prompt——这是 CLI 连接器相对 MCP 最不对称的一处成本（MCP 至少能用 deferred
// 把"连接"推后，CLI 没有连接可推，能省的只有定义本身）。
//
// compact 把 N 份 schema 压成 1 份：`tool` 用 enum 列出可用子命令，`params`
// 是一个自由对象，签名写在描述里（`limit:integer*` 这种紧凑形式）。代价是
// JSON Schema 对参数的约束变弱——所以它在执行阶段仍然走同一套 ResolveParams
// （必填、类型、enum 全部照旧校验），只是错误从"模型侧 schema 拦截"变成
// "工具返回一句明确的报错"。这是显式 opt-in，默认关闭。
type CompactTool struct {
	connector string
	conn      Connector
	order     []string
	specs     map[string]ToolSpec
	// requireApproval 是"任一子命令需要审批"的保守结论。
	//
	// 审批判定发生在工具选择**之前**（RequiresApproval 不带参数），所以无法
	// 做到"只读子命令免审批、写子命令要审批"这种粒度。默认宁可多问一次：
	// compact 是省上下文的开关，不是放宽审批的开关。
	requireApproval bool
	timeout         time.Duration
}

// NewCompactTool 用连接器的声明工具构造调度工具（值拷贝，与 NewTool 同一纪律）。
func NewCompactTool(connector string, conn Connector) *CompactTool {
	t := &CompactTool{
		connector: connector,
		conn:      conn,
		specs:     make(map[string]ToolSpec, len(conn.Tools)),
	}
	for _, spec := range conn.Tools {
		if _, dup := t.specs[spec.Name]; dup {
			continue // Validate 已拦重名；这里防御性跳过
		}
		t.order = append(t.order, spec.Name)
		t.specs[spec.Name] = spec
		if d := time.Duration(conn.EffectiveTimeout(spec)) * time.Second; d > t.timeout {
			t.timeout = d
		}
		if conn.ApprovalRequired(spec.ReadOnly) {
			t.requireApproval = true
		}
	}
	if t.timeout <= 0 {
		t.timeout = time.Duration(conn.EffectiveTimeout(ToolSpec{})) * time.Second
	}
	return t
}

// Name 返回 cli_<connector>_call。
func (t *CompactTool) Name() string { return ToolName(t.connector, CompactToolName) }

// Description 是模型看到的说明：连接器前缀 + 每条子命令的一行签名。
func (t *CompactTool) Description() string {
	label := strings.TrimSpace(t.conn.Description)
	if label == "" {
		label = t.conn.Command + " CLI"
	}

	var b strings.Builder
	b.WriteString(label)
	fmt.Fprintf(&b, "\nSingle entry point for the %d subcommands declared for this CLI: "+
		"pick one in `tool` and pass its arguments in `params`. "+
		"Signature form: name(type); `*` marks required.", len(t.order))
	for _, name := range t.order {
		spec := t.specs[name]
		fmt.Fprintf(&b, "\n- %s(%s): %s", name, compactSignature(spec), oneLine(spec.Description))
	}
	b.WriteString("\nExample: {\"tool\":\"" + firstOr(t.order, "name") + "\",\"params\":{...}}")
	if shape := compactShape(t.conn); shape != "" {
		b.WriteString("\nRuns: " + shape)
	}
	if !t.requireApproval {
		b.WriteString("\nRead-only and pre-approved: prefer this over running the same command in a shell.")
	}
	return b.String()
}

// Schema 生成"选择子命令 + 自由参数"的 JSON Schema。
func (t *CompactTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tool": map[string]interface{}{
				"type":        "string",
				"enum":        t.order,
				"description": "Which declared subcommand to run. See the tool description for each one's parameters.",
			},
			"params": map[string]interface{}{
				"type":                 "object",
				"description":          "Arguments for the selected subcommand (see its signature in the tool description). Missing required arguments are reported as an error.",
				"additionalProperties": true,
			},
		},
		"required": []string{"tool"},
	}
}

// Timeout 返回各子命令里最长的那个超时。
//
// 注册表只能按工具设一个超时；调度工具选最长值，单条子命令的真正上限在
// Execute 里用 context.WithTimeout 收窄。
func (t *CompactTool) Timeout() time.Duration { return t.timeout }

// Execute 按 `tool` 分发到对应的声明工具，链路与 Tool.Execute 完全一致。
func (t *CompactTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	name, _ := params["tool"].(string)
	name = strings.TrimSpace(name)
	spec, ok := t.specs[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q; available: %s", name, strings.Join(t.order, ", "))
	}

	args := map[string]interface{}{}
	switch raw := params["params"].(type) {
	case nil:
	case map[string]interface{}:
		args = raw
	default:
		return nil, fmt.Errorf("parameter %q must be an object", "params")
	}

	timeout := time.Duration(t.conn.EffectiveTimeout(spec)) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := Run(runCtx, t.conn, spec, args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return resultOutput(res, timeout), nil
}

// RequiresApproval 见 requireApproval 字段的注释（保守取并集）。
func (t *CompactTool) RequiresApproval() bool { return t.requireApproval }

// ApprovalSummary 给审批卡片一句人类可读的动作描述。
func (t *CompactTool) ApprovalSummary() string {
	return fmt.Sprintf("CLI %s → dispatcher (%s)", t.connector, strings.Join(t.order, ", "))
}

// ApprovalSummaryWith 展示"这次具体会跑哪条命令"（已脱敏）。
//
// 调度工具比普通工具更依赖这一行：工具名 `cli_gh_call` 本身不含任何信息，
// 用户只能从命令里看出这次是 `pr list` 还是 `repo delete`。
func (t *CompactTool) ApprovalSummaryWith(params map[string]interface{}) string {
	name, _ := params["tool"].(string)
	name = strings.TrimSpace(name)
	spec, ok := t.specs[name]
	if !ok {
		return t.ApprovalSummary()
	}
	args, _ := params["params"].(map[string]interface{})
	if cmd := PreviewCommand(t.conn, spec, args); cmd != "" {
		return cmd
	}
	// 参数不合法（缺必填等）：至少让用户看出选的是哪条子命令。
	return fmt.Sprintf("%s [tool=%s]", t.ApprovalSummary(), name)
}

// ConnectorName 返回所属连接器名（与 Tool/RawTool 保持一致的查询口）。
func (t *CompactTool) ConnectorName() string { return t.connector }

// Specs 返回本调度工具覆盖的工具声明（值拷贝，供测试/UI 反查）。
func (t *CompactTool) Specs() []ToolSpec {
	out := make([]ToolSpec, 0, len(t.order))
	for _, name := range t.order {
		out = append(out, t.specs[name])
	}
	return out
}

// compactSignature 生成一条紧凑签名：`repo, limit:integer*, json:boolean`。
func compactSignature(spec ToolSpec) string {
	parts := make([]string, 0, len(spec.Params))
	for _, p := range spec.Params {
		typ := p.Type
		if typ == "" {
			typ = "string"
		}
		s := p.Name
		if typ != "string" {
			s += ":" + typ
		}
		if len(p.Enum) > 0 {
			s += "[" + strings.Join(p.Enum, "|") + "]"
		}
		if p.Required {
			s += "*"
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return "no arguments"
	}
	return strings.Join(parts, ", ")
}

// compactShape 用"名字 + 全部子命令名"表达可执行文件与公共参数，
// 避免把每条子命令的完整模板都摊进描述（那等于没省）。
func compactShape(conn Connector) string {
	argv := make([]string, 0, len(conn.Args)+2)
	argv = append(argv, conn.Command)
	argv = append(argv, conn.Args...)
	if len(conn.Tools) > 0 {
		argv = append(argv, "<tool>")
	}
	return truncate(strings.Join(argv, " "), 200)
}

// oneLine 把描述压成一行并截断：compact 的目的是省上下文，
// 单条子命令的描述不该把省下来的额度又吃回去。
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, 160)
}

func firstOr(list []string, fallback string) string {
	if len(list) > 0 {
		return list[0]
	}
	return fallback
}

// 编译期断言：调度工具必须满足注册表契约（agent 侧的审批接口断言
// 与 Tool/RawTool 一样放在测试里，见 cliconn_test.go）。
var _ RegisteredTool = (*CompactTool)(nil)
