package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// config.go —— acp.json 的读写与校验。
//
// 为什么单独一个文件而不是塞进 config.json：ACP agent 的增删是高频动作，
// 和"改一次用半年"的 provider/审批配置生命周期不同；单独文件也让
// `magic acp` 子命令不必加载整份 config（那会连带拉进 privacy/voice/目录）。
// 这与 mcp.json / cli.json 的拆分理由一致。

const (
	// ConfigFileName 是 magicHome 下的 ACP 配置文件名。
	ConfigFileName = "acp.json"

	// EnvConfigPath 可整体覆盖配置文件位置（测试与多环境隔离用）。
	EnvConfigPath = "GO_MAGIC_ACP_CONFIG"

	// DefaultTimeoutSeconds 是单轮 prompt 的默认超时（10 分钟）。
	DefaultTimeoutSeconds = 600
	// MaxTimeoutSeconds 是超时硬上限，避免配成几小时把回合挂死。
	MaxTimeoutSeconds = 3600
)

var agentNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// validKinds 是 auto_approve 允许出现的工具种类。
var validKinds = map[string]struct{}{
	ToolKindRead:       {},
	ToolKindEdit:       {},
	ToolKindDelete:     {},
	ToolKindMove:       {},
	ToolKindSearch:     {},
	ToolKindExecute:    {},
	ToolKindThink:      {},
	ToolKindFetch:      {},
	ToolKindSwitchMode: {},
	ToolKindOther:      {},
}

// Config 是 acp.json 的文档结构。
type Config struct {
	Agents map[string]AgentConfig `json:"agents"`
}

// AgentConfig 描述一个外部 ACP agent 的启动方式与会话策略。
type AgentConfig struct {
	// Command 是可执行文件（PATH 里的名字或绝对路径）。
	Command string `json:"command"`
	// Args 是启动参数，原样按 argv 传给子进程（不经过 shell）。
	Args []string `json:"args,omitempty"`
	// Env 是 "KEY=VALUE" 形式的环境变量，用来放 API Key 之类的凭据。
	// 值不会回显给模型，也不会写进工具输出。
	Env []string `json:"env,omitempty"`
	// Cwd 是子进程自身的工作目录（空 = 继承宿主）。
	Cwd string `json:"cwd,omitempty"`
	// Workspace 是会话 cwd（session/new 的 cwd），也是 fs 访问的根。
	// 空 = 当前进程工作目录。必须是绝对路径。
	Workspace string `json:"workspace,omitempty"`
	// AdditionalDirs 是会话额外可访问目录（必须是绝对路径）。
	AdditionalDirs []string `json:"additional_dirs,omitempty"`
	// Description 会拼进工具描述，告诉模型这个 agent 擅长什么。
	Description string `json:"description,omitempty"`
	// TimeoutSeconds 是单轮 prompt 超时，0 = 600，上限 3600。
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// AllowWrite 允许 agent 通过 fs/write_text_file 改文件。
	// 默认 false：写盘比读文件危险一个数量级，要显式打开。
	AllowWrite bool `json:"allow_write,omitempty"`
	// AutoApprove 是自动批准的工具种类（tool kind）白名单。
	// 省略 = 只放行 DefaultAllowedKinds（只读类）；显式 "all" = 全放行。
	AutoApprove []string `json:"auto_approve,omitempty"`
	// Enabled 控制该 agent 是否注册工具。nil = true（老配置无此字段必须照旧生效）。
	Enabled *bool `json:"enabled,omitempty"`
}

// EnabledValue 返回是否启用（nil 视为 true）。
func (a AgentConfig) EnabledValue() bool {
	return a.Enabled == nil || *a.Enabled
}

// Timeout 返回单轮超时，已做上下限收敛。
func (a AgentConfig) Timeout() time.Duration {
	sec := a.TimeoutSeconds
	if sec <= 0 {
		sec = DefaultTimeoutSeconds
	}
	if sec > MaxTimeoutSeconds {
		sec = MaxTimeoutSeconds
	}
	return time.Duration(sec) * time.Second
}

// PermissionPolicy 由 auto_approve 生成授权策略。
func (a AgentConfig) PermissionPolicy() PermissionFunc {
	if len(a.AutoApprove) == 0 {
		return AllowKinds(DefaultAllowedKinds...)
	}
	for _, k := range a.AutoApprove {
		if strings.EqualFold(strings.TrimSpace(k), "all") {
			return func(ctx context.Context, req PermissionRequest) PermissionOutcome {
				return allowFirst(req.Options)
			}
		}
	}
	return AllowKinds(a.AutoApprove...)
}

// Validate 校验单个 agent 配置。
func (a AgentConfig) Validate(name string) error {
	if !agentNameRe.MatchString(name) {
		return fmt.Errorf("agent name %q is invalid (allowed: letters, digits, _ . -, must start alphanumeric, max 64)", name)
	}
	if strings.TrimSpace(a.Command) == "" {
		return fmt.Errorf("agent %q: command is required", name)
	}
	if a.Workspace != "" && !filepath.IsAbs(expandHome(a.Workspace)) {
		return fmt.Errorf("agent %q: workspace must be an absolute path: %s", name, a.Workspace)
	}
	for _, dir := range a.AdditionalDirs {
		if !filepath.IsAbs(expandHome(dir)) {
			return fmt.Errorf("agent %q: additional_dirs entries must be absolute: %s", name, dir)
		}
	}
	if a.TimeoutSeconds < 0 {
		return fmt.Errorf("agent %q: timeout_seconds must be positive", name)
	}
	for _, k := range a.AutoApprove {
		k = strings.TrimSpace(k)
		if k == "" || strings.EqualFold(k, "all") {
			continue
		}
		if _, ok := validKinds[k]; !ok {
			return fmt.Errorf("agent %q: unknown tool kind %q in auto_approve", name, k)
		}
	}
	for _, env := range a.Env {
		if !strings.Contains(env, "=") {
			return fmt.Errorf("agent %q: env entry %q must be KEY=VALUE", name, env)
		}
	}
	return nil
}

// DefaultConfigPath 返回配置文件路径。
//
// 自己解析 magicHome 而不 import pkg/config 是有意的：pkg/config 是所有
// 连接器配置的聚合层（它已经 import 了 internal/mcp 与 internal/cliconn），
// 一旦它将来纳入 ACP 配置就会与本包形成 import 环。
func DefaultConfigPath() string {
	if p := strings.TrimSpace(os.Getenv(EnvConfigPath)); p != "" {
		return p
	}
	home := strings.TrimSpace(os.Getenv("GO_MAGIC_HOME"))
	if home == "" {
		if h := strings.TrimSpace(os.Getenv("HOME")); h != "" {
			home = filepath.Join(h, ".magic")
		} else if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".magic")
		} else {
			home = ".magic"
		}
	}
	return filepath.Join(home, ConfigFileName)
}

// LoadConfig 读取配置。文件不存在时返回空配置而不是错误——
// 没配 ACP 是正常状态，不该让调用方在启动时报错。
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{Agents: map[string]AgentConfig{}}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return cfg, nil
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Agents == nil {
		cfg.Agents = map[string]AgentConfig{}
	}
	return cfg, nil
}

// SaveConfig 原子写回配置文件。
//
// 0600：env 里通常有 API Key，和 mcp.json 一样按凭据对待。
func SaveConfig(path string, cfg *Config) error {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.Agents == nil {
		cfg.Agents = map[string]AgentConfig{}
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// ValidateAll 校验全部 agent，返回按名字排序的错误信息。
func (c *Config) ValidateAll() []error {
	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	sort.Strings(names)

	var errs []error
	for _, name := range names {
		if err := c.Agents[name].Validate(name); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// AgentNames 返回启用的 agent 名字（排序后），供工具描述与 CLI 列表使用。
func (c *Config) AgentNames() []string {
	names := make([]string, 0, len(c.Agents))
	for name, agent := range c.Agents {
		if agent.EnabledValue() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// allowFirst 是"全放行"策略：优先 allow_once。
func allowFirst(options []PermissionOption) PermissionOutcome {
	if opt, ok := pickOption(options, PermissionAllowOnce, PermissionAllowAlways); ok {
		return PermissionOutcome{Outcome: OutcomeSelected, OptionID: opt}
	}
	return PermissionOutcome{Outcome: OutcomeCancelled}
}
