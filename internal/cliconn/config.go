// Package cliconn 把"只有命令行接口、没有 MCP"的外部软件接进工具层。
//
// 与 internal/mcp 的区别在于协议：MCP 连接器要求对端实现 JSON-RPC 握手，
// 而绝大多数桌面软件/运维工具只提供 CLI（gh、kubectl、ffmpeg、内部脚本）。
// 这里把「一个可执行文件 + 若干子命令」声明成连接器，每个子命令展开成一个
// 模型可调用的工具（cli_<connector>_<tool>），执行时直接拉起子进程。
//
// 三条硬约束：
//  1. **不经过 shell**：argv 由数组直接交给 exec，参数替换只做字符串填槽，
//     不做分词、不做引号解析。因此参数值里带 `;` `&&` `|` 也不会被解释。
//  2. 参数只允许在声明过的地方出现：模板里的 {{name}} 必须对应 ToolSpec.Params
//     中的一项，模型给的额外键一律忽略。
//  3. 默认要人工确认：命令行能做的事没有边界，除非工具显式声明 read_only，
//     否则每次调用都进审批链路。
package cliconn

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// NamespacePrefix 是 CLI 连接器工具在注册表里的统一前缀。
	NamespacePrefix = "cli_"

	// DefaultTimeoutSeconds 是单次命令的默认超时。CLI 工具有的会跑很久
	// （构建、同步、批量拉取），但无上限的等待会把整个回合拖到 turn timeout，
	// 所以给一个可覆盖的默认值。
	DefaultTimeoutSeconds = 60
	// MaxTimeoutSeconds 是超时上限，避免配置成几小时把回合挂死。
	//
	// 必须与 internal/tool.MaxTimeout（300s）保持一致：执行器会把 ctx 再包一层，
	// 超过它的配置会被静默截断——用户在表单里填 600 却只跑 300，比拒绝填写更糟。
	// 这里不能直接引用那个常量（internal/tool → pkg/config → 本包，会成环）。
	MaxTimeoutSeconds = 300

	// DefaultMaxOutputBytes 是单次命令回传给模型的最大输出（32KB）。
	// 超出的部分被截断并标注，避免一次 `kubectl get pods -A` 把上下文吃光。
	DefaultMaxOutputBytes = 32 << 10
)

var (
	connectorNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	toolNameRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,47}$`)
	paramNameRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,47}$`)
)

// Config 是 config.json 中 "cli" 段的结构。
type Config struct {
	Connectors map[string]Connector `json:"connectors,omitempty"`
}

// Connector 描述一个外部 CLI 程序，以及要从它身上暴露哪些子命令。
type Connector struct {
	// Command 是可执行文件名或绝对路径（PATH 里能找到即可）。
	Command string `json:"command"`
	// Args 是每个工具都会带上的公共参数（如 `-o json`），排在工具自身
	// Args 之前，同样支持 {{param}} 模板。
	Args []string `json:"args,omitempty"`
	// Description 会拼进每个工具的描述前缀，告诉模型这是哪个软件的 CLI。
	Description string `json:"description,omitempty"`
	// WorkingDir 是子进程的工作目录，支持 ~ 开头；空 = 继承服务进程。
	WorkingDir string `json:"working_dir,omitempty"`
	// Env 是 "KEY=VALUE" 形式的环境变量，用来放 token。值在 API 层按 *** 掩码。
	Env []string `json:"env,omitempty"`
	// TimeoutSeconds 是该连接器的默认超时，工具级可覆盖。
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// Enabled 控制该连接器是否注册工具。nil = true（老配置没有该字段，
	// 语义必须是"照旧生效"）。
	Enabled *bool `json:"enabled,omitempty"`
	// Profiles 限定只在指定分身里生效；空 = 所有分身。
	Profiles []string `json:"profiles,omitempty"`
	// RequireApproval 强制该连接器下每个工具都走人工确认。
	// nil = 按工具自己的 read_only 决定（read_only 才免确认）。
	RequireApproval *bool `json:"require_approval,omitempty"`
	// Tools 是从该程序暴露出去的子命令。
	Tools []ToolSpec `json:"tools,omitempty"`
}

// ToolSpec 描述一个具体的子命令，展开成一个工具。
type ToolSpec struct {
	// Name 是工具短名，最终注册名是 cli_<connector>_<name>。
	Name string `json:"name"`
	// Description 直接进模型看到的工具描述，要写清"这个命令做什么、返回什么"。
	Description string `json:"description"`
	// Args 是参数模板。规则见 ExpandArgs：
	//   - {{name}}            → 替换成参数值；整个 arg 只有一个占位符且值为空时省略该 arg
	//   - 以 {{?name}} 开头    → 该参数为真时保留其后的字面量，否则整条 arg 丢弃
	// 例：["pr", "list", "--limit", "{{limit}}", "{{?verbose}}--verbose"]
	Args []string `json:"args,omitempty"`
	// StdinParam 指定把哪个参数的值写进子进程 stdin（比如喂一段 diff、一份 JSON）。
	StdinParam string `json:"stdin_param,omitempty"`
	// Params 声明模型可以填的参数；模板里出现的名字必须在这里声明。
	Params []ParamSpec `json:"params,omitempty"`
	// TimeoutSeconds 覆盖连接器默认超时。
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// ReadOnly 声明该命令不修改任何状态（只查询）。只有声明了它，
	// 且连接器没强制 require_approval，才会免人工确认。
	ReadOnly bool `json:"read_only,omitempty"`
}

// ParamSpec 是工具的一个参数。
type ParamSpec struct {
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"` // string | integer | number | boolean，默认 string
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// AutoConnect 报告该连接器是否应该注册工具。
func (c Connector) AutoConnect() bool { return c.Enabled == nil || *c.Enabled }

// AppliesToProfile 报告该连接器是否在给定分身里生效。
func (c Connector) AppliesToProfile(profile string) bool {
	if len(c.Profiles) == 0 {
		return true
	}
	for _, p := range c.Profiles {
		if strings.EqualFold(strings.TrimSpace(p), strings.TrimSpace(profile)) {
			return true
		}
	}
	return false
}

// ApprovalRequired 判定某个工具是否需要人工确认。
//
// readOnly 只是"声明"，不是信任根：连接器级 require_approval 优先。
func (c Connector) ApprovalRequired(readOnly bool) bool {
	if c.RequireApproval != nil {
		return *c.RequireApproval
	}
	return !readOnly
}

// EffectiveTimeout 返回某个工具的实际超时秒数。
func (c Connector) EffectiveTimeout(spec ToolSpec) int {
	sec := spec.TimeoutSeconds
	if sec <= 0 {
		sec = c.TimeoutSeconds
	}
	if sec <= 0 {
		sec = DefaultTimeoutSeconds
	}
	if sec > MaxTimeoutSeconds {
		sec = MaxTimeoutSeconds
	}
	return sec
}

// ToolName 返回工具在注册表里的全名。
//
// 分隔符沿用 mcp_ 的约定（下划线），但连接器名/工具名本身不允许下划线以外的
// 怪异字符，因此 RemoveConnectorTools 用前缀匹配是安全的：不会出现
// cli_ab_ 同时匹配 cli_abc_x 的情况（前缀里带结尾下划线）。
func ToolName(connector, tool string) string {
	return NamespacePrefix + connector + "_" + tool
}

// ToolPrefix 返回某连接器下所有工具的名字前缀。
func ToolPrefix(connector string) string {
	return NamespacePrefix + connector + "_"
}

// ValidateConnectorName 校验连接器名（同时是配置键与工具名的一部分）。
func ValidateConnectorName(name string) error {
	if !connectorNameRe.MatchString(strings.TrimSpace(name)) {
		return fmt.Errorf("invalid connector name %q: use letters, digits, '_', '-', '.' (must start with a letter or digit)", name)
	}
	return nil
}

// Validate 校验连接器配置，返回第一个致命问题。
//
// 校验在保存时同步执行：这些错误（缺 command、工具重名、模板引用了没声明的
// 参数）都能立即判定，没有理由留给运行时的第一次调用才暴露。
func (c Connector) Validate() error {
	if strings.TrimSpace(c.Command) == "" {
		return fmt.Errorf("command is required")
	}
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > MaxTimeoutSeconds {
		return fmt.Errorf("timeout_seconds must be between 0 and %d", MaxTimeoutSeconds)
	}
	for _, kv := range c.Env {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return fmt.Errorf("invalid env entry %q: expected KEY=VALUE", kv)
		}
	}
	if len(c.Tools) == 0 {
		return fmt.Errorf("at least one tool is required")
	}

	seen := make(map[string]bool, len(c.Tools))
	for i, spec := range c.Tools {
		if err := spec.Validate(); err != nil {
			return fmt.Errorf("tools[%d]: %w", i, err)
		}
		if seen[spec.Name] {
			return fmt.Errorf("duplicate tool name %q", spec.Name)
		}
		seen[spec.Name] = true
	}

	// 公共 Args 里引用的参数在每个工具里都必须有声明，否则展开时静默变空值。
	for i, spec := range c.Tools {
		declared := spec.paramSet()
		for _, arg := range c.Args {
			for _, name := range Placeholders(arg).Values {
				if !declared[name] {
					return fmt.Errorf("tools[%d] %q: args refer to undeclared parameter %q", i, spec.Name, name)
				}
			}
		}
	}
	return nil
}

// Validate 校验单个工具声明。
func (s ToolSpec) Validate() error {
	if !toolNameRe.MatchString(strings.TrimSpace(s.Name)) {
		return fmt.Errorf("invalid tool name %q: use letters, digits, '_', '-' (must start with a letter or digit)", s.Name)
	}
	if strings.TrimSpace(s.Description) == "" {
		return fmt.Errorf("tool %q: description is required", s.Name)
	}
	if s.TimeoutSeconds < 0 || s.TimeoutSeconds > MaxTimeoutSeconds {
		return fmt.Errorf("tool %q: timeout_seconds must be between 0 and %d", s.Name, MaxTimeoutSeconds)
	}
	if len(s.Args) == 0 {
		return fmt.Errorf("tool %q: args is required", s.Name)
	}

	declared := s.paramSet()
	seen := make(map[string]bool, len(s.Params))
	for i, p := range s.Params {
		if !paramNameRe.MatchString(p.Name) {
			return fmt.Errorf("tool %q: invalid parameter name %q", s.Name, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("tool %q: duplicate parameter %q", s.Name, p.Name)
		}
		seen[p.Name] = true
		if err := p.Validate(); err != nil {
			return fmt.Errorf("tool %q: params[%d]: %w", s.Name, i, err)
		}
	}

	// 模板里出现的每个名字都必须有声明——包括条件标记 {{?x}}。
	for _, arg := range s.Args {
		ph := Placeholders(arg)
		for _, name := range ph.Values {
			if !declared[name] {
				return fmt.Errorf("tool %q: arg %q refers to undeclared parameter %q", s.Name, arg, name)
			}
		}
		for _, name := range ph.Conditions {
			if !declared[name] {
				return fmt.Errorf("tool %q: arg %q refers to undeclared parameter %q", s.Name, arg, name)
			}
		}
	}
	if s.StdinParam != "" && !declared[s.StdinParam] {
		return fmt.Errorf("tool %q: stdin_param %q is not declared in params", s.Name, s.StdinParam)
	}
	return nil
}

// Validate 校验参数声明。
func (p ParamSpec) Validate() error {
	switch p.Type {
	case "", "string", "integer", "number", "boolean", "array", "object":
	default:
		return fmt.Errorf("parameter %q: unsupported type %q (string|integer|number|boolean)", p.Name, p.Type)
	}
	if p.Type == "boolean" && p.Default != nil {
		if _, ok := p.Default.(bool); !ok {
			return fmt.Errorf("parameter %q: default must be a boolean", p.Name)
		}
	}
	return nil
}

func (s ToolSpec) paramSet() map[string]bool {
	out := make(map[string]bool, len(s.Params))
	for _, p := range s.Params {
		out[p.Name] = true
	}
	return out
}

// ExpandWorkingDir 展开工作目录里的 ~ 前缀；空串原样返回（继承父进程）。
func ExpandWorkingDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			rest := strings.TrimLeft(strings.TrimPrefix(dir, "~"), `/\`)
			if rest == "" {
				return home
			}
			return filepath.Join(home, rest)
		}
	}
	return dir
}
