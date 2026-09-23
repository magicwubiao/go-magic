package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/privacy"
	"github.com/magicwubiao/go-magic/internal/voice"
)

const (
	DefaultMagicHome = "~/.magic"
	ConfigFileName   = "config.json"
	// DefaultBrowserProfileDir 是自动化浏览器持久 profile 的默认目录。
	// 默认即持久：登录一次（cookie/localStorage）后续都用同一份，适合抓需要
	// 登录态的站点。想回到"每次全新临时 profile"就把它显式写成 ""。
	DefaultBrowserProfileDir = "~/.magic/browser-profile"
	// DefaultBrowserHeadless 是自动化浏览器是否无头运行的默认值。
	//
	// 默认 true（无头）：服务器的正常形态就是「没有显示服务」——容器、云主机、
	// CI、systemd 服务全都如此。在这些环境里跑「有头」Chrome 会直接死在
	// "cannot open display"，而 agent 又没有任何补救手段（它只能调工具、看不到
	// 报错原因），所以默认必须是无头。
	//
	// 无头不影响浏览器能力：页面照常渲染，截图（browser_vision）、点击、执行 JS
	// 全部正常，只是没有打到显示器上的窗口。仅当需要人工操作（首次登录扫码/输
	// 密码/过验证码）时才需要临时打开：设 BROWSER_HEADLESS=false，或把它写进
	// 配置文件的 browser_headless 字段。
	DefaultBrowserHeadless = true
	// DefaultTurnTimeoutMinutes 是单个会话回合在 chat queue 中的执行时限（分钟）。
	//
	// 这是"一轮对话能跑多久"的真正硬约束：超过它，回合被取消并按"被中断"
	// 收尾（已产出的部分照常落库，不会丢）。它与 agent.max_turns 是两道独立
	// 的闸门——max_turns 限的是"工具循环迭代几次"，本项限的是"总共跑多久"，
	// 谁先到谁生效。因此任务确实需要更长时间时，该调的是本项，而不是
	// max_turns（后者调多大都会被时间墙挡住）。
	DefaultTurnTimeoutMinutes = 30
)

func GetMagicHome() string {
	if magicHome := os.Getenv("GO_MAGIC_HOME"); magicHome != "" {
		return magicHome
	}

	// Try HOME env var first (most reliable on Linux)
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".magic")
	}

	// Fallback: use os.UserHomeDir()
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".magic")
	}

	// Last resort: read /etc/passwd for current user's home directory.
	// This handles cases where UserHomeDir() fails (e.g. root in containers).
	if home := getHomeFromPasswd(); home != "" {
		return filepath.Join(home, ".magic")
	}

	// Absolute last resort
	return "/tmp/.magic"
}

// getHomeFromPasswd reads /etc/passwd to find the home directory of
// the current user. This is more reliable than os.UserHomeDir() in
// containerized or unusual Linux environments.
func getHomeFromPasswd() string {
	uid := syscall.Getuid()
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Split(line, ":")
		if len(fields) < 6 {
			continue
		}
		if fields[2] == fmt.Sprintf("%d", uid) {
			return fields[5]
		}
	}
	return ""
}

// ErrNoConfig indicates that no config file exists (first run).
var ErrNoConfig = fmt.Errorf("config file not found")

// Config represents the application configuration
type Config struct {
	Profile      string                    `json:"profile"`
	MagicHome    string                    `json:"magic_home"`
	WorkingDir   string                    `json:"working_dir,omitempty"`
	Provider     string                    `json:"provider"`
	Model        string                    `json:"model"` // Deprecated: Use Providers[].Models[0] instead
	Providers    map[string]ProviderConfig `json:"providers"`
	Tools        ToolsConfig               `json:"tools"`
	Skills       SkillsConfig              `json:"skills"`
	Plugins      PluginsConfig             `json:"plugins"`
	AgentPlugins AgentPluginsConfig        `json:"agent_plugins"`
	Memory       MemoryConfig              `json:"memory"`
	Gateway      GatewayConfig             `json:"gateway"`
	Cortex       CortexConfig              `json:"cortex"`
	// Context：静态规则文件（AGENTS.md/CLAUDE.md/CONTEXT.md）自动加载。
	// 指针类型：nil（未配置）= 默认开启；显式 "enabled": false 才关闭。
	// 不用普通 bool 是因为 config.Load 对已存在的 config.json 直接反序列化
	// 到零值、不合并 defaultConfig——普通 bool 缺键会静默变 false。
	Context  *ContextConfig  `json:"context,omitempty"`
	MCP      *MCPConfig      `json:"mcp,omitempty"`
	SubAgent *SubAgentConfig `json:"subagent,omitempty"`
	Voice    *VoiceConfig    `json:"voice,omitempty"`
	// Bot Mode: named agent profiles with persistent canonical chats
	BotMode *BotModeConfig `json:"bot_mode,omitempty"`
	// Privacy / PII 脱敏配置，统一存储于 config.json（团队约定：一个配置管所有）。
	Privacy *privacy.Config `json:"privacy,omitempty"`
	// BrowserProfileDir 指定自动化浏览器的持久 profile 目录（Chrome --user-data-dir）。
	// 指针语义（同下方 Context）：nil（配置里没写）= 用默认值
	// DefaultBrowserProfileDir（~/.magic/browser-profile，登录态长期保留）；
	// 显式 "" = 每次启动用全新临时 profile（无 cookie/登录态）；
	// 其他值 = 该目录。路径支持 `~` 开头。BROWSER_PROFILE_DIR 环境变量可覆盖。
	// 用指针而非普通 string：Load 对已存在的 config.json 直接反序列化到零值、
	// 不合并 defaultConfig——普通 string 无法区分"没写"和"显式写空"。
	BrowserProfileDir *string `json:"browser_profile_dir,omitempty"`
	// BrowserHeadless 控制自动化浏览器是否无头运行。指针语义（同 BrowserProfileDir）：
	// nil（配置里没写）= 用默认值 DefaultBrowserHeadless（true，无头）；
	// 显式 false = 开有头窗口（需要显示服务：本机桌面，或 Xvfb/noVNC）；
	// 显式 true = 无头。BROWSER_HEADLESS 环境变量优先级最高（"false" 关、"true" 开）。
	//
	// 默认无头的理由见 DefaultBrowserHeadless 的注释：服务器上没有显示服务，
	// 有头 Chrome 会直接起不来。想人工登录时临时开窗即可。
	BrowserHeadless *bool         `json:"browser_headless,omitempty"`
	Display         DisplayConfig `json:"display,omitempty"`
	Server          ServerConfig  `json:"server,omitempty"`
	// Agent settings
	SecretRedaction bool   `json:"secret_redaction,omitempty"`
	Mode            string `json:"mode,omitempty"`      // chat, plan, act
	ChatMode        string `json:"chat_mode,omitempty"` // chat, coding - default mode for magic chat
	Agent           struct {
		// MaxTurns caps one conversation turn's tool-loop iterations for
		// server/bot agents (default 150). 0 keeps the built-in default.
		// Note TurnTimeoutMinutes below: at ~10-30s per iteration only
		// ~60-180 iterations fit inside the turn wall, so a larger cap
		// silently never takes effect.
		MaxTurns       int   `json:"max_turns,omitempty"`
		MaxIterations  int   `json:"max_iterations,omitempty"`   // steering cap; default 200
		MaxTokenBudget int64 `json:"max_token_budget,omitempty"` // steering token budget
		// TurnTimeoutMinutes caps how long a single conversation turn in the
		// chat queue may run (all LLM calls + tool executions). 0 = default
		// (30 min). When the deadline hits the turn is cancelled and settled
		// as "interrupted": partial output is persisted, not lost.
		//
		// Raising this is the right lever when a task legitimately needs more
		// than 30 minutes — raising MaxTurns instead does nothing, because the
		// clock wall (not the iteration cap) is what actually binds.
		TurnTimeoutMinutes int `json:"turn_timeout_minutes,omitempty"`
	} `json:"agent,omitempty"`
	// Approval settings
	Approval *ApprovalConfig `json:"approval,omitempty"`
}

// GetCurrentModel returns the current model from the configured provider's Models array
func (c *Config) GetCurrentModel() string {
	if c.Provider == "" {
		return ""
	}
	provCfg, ok := c.Providers[c.Provider]
	if !ok {
		return ""
	}
	if len(provCfg.Models) > 0 {
		return provCfg.Models[0]
	}
	return c.Model // Fallback to deprecated field for compatibility
}

// MemoryConfig represents memory configuration
type MemoryConfig struct {
	Enabled bool `json:"enabled"`

	// 以下为可选字段，零值（nil）表示使用默认值。
	// 与 internal/memory.MemoryConfig 对应，便于配置驱动 cortex.Manager 的记忆子系统。
	DBPath             *string `json:"db_path,omitempty" yaml:"db_path,omitempty"`                           // 记忆 SQLite 数据库路径
	MaxContentLength   *int    `json:"max_content_length,omitempty" yaml:"max_content_length,omitempty"`     // 单条记忆最大字符数
	MaxAgentMemLength  *int    `json:"max_agent_mem_length,omitempty" yaml:"max_agent_mem_length,omitempty"` // agent 记忆文件最大字符数
	MaxUserMemLength   *int    `json:"max_user_mem_length,omitempty" yaml:"max_user_mem_length,omitempty"`   // user 记忆文件最大字符数
	AutoSummarize      *bool   `json:"auto_summarize,omitempty" yaml:"auto_summarize,omitempty"`             // 是否开启自动摘要
	SummarizeThreshold *int    `json:"summarize_threshold,omitempty" yaml:"summarize_threshold,omitempty"`   // 触发摘要的字符阈值
	LLMProvider        *string `json:"llm_provider,omitempty" yaml:"llm_provider,omitempty"`                 // 摘要使用的 LLM provider
}

// CortexConfig represents Cortex AI configuration
type CortexConfig struct {
	Enabled             bool `json:"enabled"`                // Enable/disable Cortex system
	SkillMinPatternFreq int  `json:"skill_min_pattern_freq"` // Min frequency for skill pattern detection

	// 以下为可选字段，零值（nil）表示使用默认值。
	// 与 internal/cortex.ManagerConfig 对应，便于配置驱动 Cortex 调参。
	ReviewInterval                *time.Duration `json:"review_interval,omitempty" yaml:"review_interval,omitempty"`                                 // 后台评审间隔
	ReviewEnabled                 *bool          `json:"review_enabled,omitempty" yaml:"review_enabled,omitempty"`                                   // 是否启用后台评审
	NudgeInterval                 *time.Duration `json:"nudge_interval,omitempty" yaml:"nudge_interval,omitempty"`                                   // Nudge 间隔
	NudgeEnabled                  *bool          `json:"nudge_enabled,omitempty" yaml:"nudge_enabled,omitempty"`                                     // 是否启用 Nudge
	PerceptionConfidenceThreshold *float64       `json:"perception_confidence_threshold,omitempty" yaml:"perception_confidence_threshold,omitempty"` // 感知置信度阈值
	PerceptionMaxHistory          *int           `json:"perception_max_history,omitempty" yaml:"perception_max_history,omitempty"`                   // 感知最大历史条数
	PlanningMaxSteps              *int           `json:"planning_max_steps,omitempty" yaml:"planning_max_steps,omitempty"`                           // 规划最大步数
	PlanningTimeout               *time.Duration `json:"planning_timeout,omitempty" yaml:"planning_timeout,omitempty"`                               // 规划超时
}

// ContextConfig 控制静态规则文件自动加载。
// 会话设置了工作目录（work_dir_user_set）时，agent 从该目录向上逐级发现
// AGENTS.md / CLAUDE.md / CONTEXT.md 并注入 system（就近优先），实现
// 「同一目录的会话共享同一份规则共识」。Enabled 为 nil 时视为开启。
type ContextConfig struct {
	Enabled *bool `json:"enabled,omitempty"` // nil = 默认开启；false = 显式关闭
}

// ServerConfig represents server-related configuration
type ServerConfig struct {
	UploadURLPrefix   string   `json:"upload_url_prefix,omitempty"`      // Public URL prefix for uploaded files (e.g., "https://your-domain.com/uploads")
	FileStrategy      string   `json:"file_strategy,omitempty"`          // "auto" (default), "url", "base64"
	UploadMaxBytes    int64    `json:"upload_max_bytes,omitempty"`       // 单文件最大字节数；0 表示 100 MiB
	UploadBlockedExts []string `json:"upload_blocked_exts,omitempty"`    // 黑名单扩展名（含点），默认包含可执行类型
	UploadStreamMax   bool     `json:"upload_stream_max,omitempty"`      // （保留位）true 时不再接受 base64 走 stream
	UploadOrphanTTL   int      `json:"upload_orphan_ttl_days,omitempty"` // 孤儿文件清理阈值（天），0 默认 30
}

// GetUploadMaxBytes returns the upload size limit (default 100 MiB when unset).
func (s *ServerConfig) GetUploadMaxBytes() int64 {
	if s.UploadMaxBytes <= 0 {
		return 100 << 20
	}
	return s.UploadMaxBytes
}

// GetUploadBlockedExts returns the executable extension blacklist (lowercase, with leading dot).
func (s *ServerConfig) GetUploadBlockedExts() map[string]struct{} {
	defaults := []string{
		".exe", ".bat", ".cmd", ".com", ".msi", ".scr", ".pif",
		".vbs", ".vbe", ".js", ".jse", ".wsf", ".wsh", ".ps1", ".psm1",
		".sh", ".bash", ".zsh", ".csh", ".ksh", ".fish",
		".jar", ".py", ".pl", ".rb", ".php", ".asp", ".aspx",
		".dll", ".so", ".dylib", ".app", ".dmg", ".iso", ".elf",
	}
	merged := make([]string, 0, len(defaults)+len(s.UploadBlockedExts))
	merged = append(merged, defaults...)
	merged = append(merged, s.UploadBlockedExts...)
	out := make(map[string]struct{}, len(merged))
	for _, ext := range merged {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		out[ext] = struct{}{}
	}
	return out
}

// GetUploadOrphanTTL returns how long an orphan upload lives (default 30 days).
func (s *ServerConfig) GetUploadOrphanTTL() time.Duration {
	if s.UploadOrphanTTL <= 0 {
		return 30 * 24 * time.Hour
	}
	return time.Duration(s.UploadOrphanTTL) * 24 * time.Hour
}

// GetFileStrategy returns the file strategy, defaulting to "auto"
func (s *ServerConfig) GetFileStrategy() string {
	if s.FileStrategy == "" {
		return "auto"
	}
	return s.FileStrategy
}

// DisplayConfig represents display/UI configuration
type DisplayConfig struct {
	Skin        string `json:"skin,omitempty"`         // Active skin name
	NoColor     bool   `json:"no_color,omitempty"`     // Disable colors
	ShowBanner  bool   `json:"show_banner,omitempty"`  // Show startup banner
	ShowVersion bool   `json:"show_version,omitempty"` // Show version info
}

// ProviderConfig represents provider configuration
// Note: api_key uses omitempty to prevent accidentally overwriting with empty value on Save()
// Model is deprecated, use Models[0] as current model instead
type ProviderConfig struct {
	APIKey  string   `json:"api_key,omitempty"`
	BaseURL string   `json:"base_url,omitempty"`
	Model   string   `json:"model,omitempty"`  // Deprecated: use Models[0] instead, kept for backward compatibility
	Models  []string `json:"models,omitempty"` // List of supported models, first element is current model
	// Vision explicitly declares whether this provider's models accept
	// image_url parts. nil = auto-detect from the model name; true/false
	// overrides detection entirely (name-based guessing is best-effort and
	// inevitably lags new model releases, e.g. glm-4.1v-*).
	Vision *bool `json:"vision,omitempty"`
	// ExtraParams are transparent request-body params merged into every
	// outbound /chat/completions call (only for OpenAI-compatible
	// providers). Use case: enable reasoning output on gateways that hide
	// it behind a flag, e.g. {"include_reasoning": true} (OpenRouter-style),
	// {"enable_thinking": true} (DashScope-style) or
	// {"reasoning_effort": "high"}. Reserved core keys are refused.
	ExtraParams map[string]interface{} `json:"extra_params,omitempty"`
}

// GetCurrentModel returns the current model (first element of Models, fallback to Model field)
func (p *ProviderConfig) GetCurrentModel() string {
	if len(p.Models) > 0 {
		return p.Models[0]
	}
	return p.Model
}

// ToolsConfig represents tools configuration
type ToolsConfig struct {
	Enabled  []string `json:"enabled"`
	Disabled []string `json:"disabled"`
}

// SkillsConfig represents skills configuration
type SkillsConfig struct {
	Enabled    []string `json:"enabled"`
	Disabled   []string `json:"disabled"`
	DefaultDir string   `json:"default_dir,omitempty"` // Path to built-in default skills
	UserDir    string   `json:"user_dir,omitempty"`    // Path to user-installed skills
}

// PluginsConfig represents plugins configuration
type PluginsConfig struct {
	Enabled  []string `json:"enabled"`
	Disabled []string `json:"disabled"`
}

// AgentPluginsConfig 管理 OpenAI Agent Plugins 1.0.0 插件的禁用列表。
// 启用为默认状态,仅记录被显式禁用的插件名(即 plugin.json 的 name 字段)。
type AgentPluginsConfig struct {
	Disabled []string `json:"disabled"`
}

// GatewayConfig represents gateway configuration
type GatewayConfig struct {
	Enabled   bool                      `json:"enabled"`
	Platforms map[string]PlatformConfig `json:"platforms"`
	// Optional per-user rate limit and blocklist for all platforms.
	RateLimitPerUser int      `json:"rate_limit_per_user,omitempty"`   // msgs/min (0 = default 20, negative disables)
	RateLimitWindow  int      `json:"rate_limit_window_sec,omitempty"` // window seconds, default 60
	BlockedUsers     []string `json:"blocked_users,omitempty"`         // user IDs never processed
	SensitiveWords   []string `json:"sensitive_words,omitempty"`       // filtered words
}

// PlatformConfig represents platform-specific configuration
type PlatformConfig struct {
	Token   string `json:"token,omitempty"`
	Enabled bool   `json:"enabled"`
	// Channel allowlist/blocklist - only respond to messages from allowed channels
	AllowedChannels []string `json:"allowed_channels,omitempty"` // Whitelist of channel/chat IDs; empty means allow all
	BlockedChannels []string `json:"blocked_channels,omitempty"` // Blacklist of channel/chat IDs; takes precedence over whitelist
	// Access control (DM / group policies). Policies: "open", "allowlist", "disabled".
	DMPolicy        string   `json:"dm_policy,omitempty"`        // default: open
	DMAllowlist     []string `json:"dm_allowlist,omitempty"`     // user IDs allowed to DM when policy=allowlist
	GroupPolicy     string   `json:"group_policy,omitempty"`     // default: open (mention required)
	GroupAllowlist  []string `json:"group_allowlist,omitempty"`  // group IDs allowed when policy=allowlist
	MentionPatterns []string `json:"mention_patterns,omitempty"` // extra regex patterns treated as mentions
	// Legacy WeCom 自建应用 (app) fields —— app 模式已于 2026-09 移除；
	// 保留字段仅为兼容读取旧配置，新配置一律用 BotID/Secret。
	CorpID  string `json:"corp_id,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
	Secret  string `json:"secret,omitempty"`
	// QQ fields（仅官方机器人；个人 QQ OneBot 模式已移除）
	Number   string `json:"number,omitempty"`
	Password string `json:"password,omitempty"`
	// DingTalk fields
	AppKey    string `json:"app_key,omitempty"`
	AppSecret string `json:"app_secret,omitempty"`
	// Feishu/Lark fields
	AppID  string `json:"app_id,omitempty"`
	APIURL string `json:"api_url,omitempty"`
	APIKey string `json:"api_key,omitempty"`
	// Mode 语义（whatsapp 已于 2026-09 移除）：wecom="aibot"（官方智能机器人扫码，bot_id+secret；
	// 2026-09 起唯一接入方式，自建应用 app 已移除）；matrix="password"（密码登录）。
	Mode   string `json:"mode,omitempty"`
	BotID  string `json:"bot_id,omitempty"`  // WeCom AI Bot
	AESKey string `json:"aes_key,omitempty"` // legacy (WeChat ClawBot)
	// WeChat ClawBot fields
	ClientID string `json:"client_id,omitempty"`
	DataDir  string `json:"data_dir,omitempty"`
	// 自定义 WebSocket 端点覆盖（当前仅 WeCom AI Bot 网关使用；留空用官方默认端点）
	WSURL string `json:"ws_url,omitempty"`
	// Microsoft Teams（Bot Framework）复用上方 AppID/AppSecret（app_id/app_secret）
	// Google Chat fields
	WebhookURL  string `json:"webhook_url,omitempty"`  // googlechat space incoming webhook (send + space identity)
	EventsToken string `json:"events_token,omitempty"` // googlechat shared secret on inbound ?token=
	// Email fields（IMAP 收 + SMTP 发；smtp_*/imap_user 留空时默认回落到 email/password）
	Email        string `json:"email,omitempty"` // 机器人自己的邮箱地址
	IMAPHost     string `json:"imap_host,omitempty"`
	IMAPPort     int    `json:"imap_port,omitempty"` // 默认 993（隐式 TLS）；143 = STARTTLS
	IMAPUser     string `json:"imap_user,omitempty"`
	IMAPPass     string `json:"imap_pass,omitempty"`
	SMTPHost     string `json:"smtp_host,omitempty"`
	SMTPPort     int    `json:"smtp_port,omitempty"` // 默认 465（隐式 TLS）；587/25 = STARTTLS
	SMTPUser     string `json:"smtp_user,omitempty"`
	SMTPPass     string `json:"smtp_pass,omitempty"`
	PollInterval int    `json:"poll_interval,omitempty"` // email 收件轮询秒数，默认 30
	// SMS（Twilio）fields
	AccountSID string `json:"account_sid,omitempty"`
	AuthToken  string `json:"auth_token,omitempty"`
	From       string `json:"from,omitempty"` // Twilio 号码（E.164），sms 用
}

// BotModeConfig enables/disables Bot Mode and tunes its behavior.
// Bots themselves are defined as files under <magicHome>/bots/<name>.json.
type BotModeConfig struct {
	Enabled bool `json:"enabled"`
	// InjectBotProtocol adds a short bot-to-bot messaging protocol section
	// (mention tags, message_agent usage) to every bot's system prompt.
	InjectBotProtocol *bool `json:"inject_bot_protocol,omitempty"`
	// HistoryWindow caps how many messages of each bot's canonical chat are
	// kept in context and on disk. 0 = default (200). The window always keeps
	// whole turns: it is trimmed back to (not past) the oldest user message,
	// so a turn's tool calls/results never get separated from its prompt.
	HistoryWindow int `json:"history_window,omitempty"`
	// TurnTimeoutMinutes caps how long a single agent turn (all LLM calls and
	// tool executions for one inbound message) may run. 0 = default (5 min).
	// When the deadline hits, the turn is aborted gracefully: progress made so
	// far is persisted and the user gets a friendly timeout notice instead of
	// a raw "context deadline exceeded" error.
	TurnTimeoutMinutes int `json:"turn_timeout_minutes,omitempty"`
	// RelayToken is an optional shared secret that remote instances ("peers")
	// must present when DMing this instance's bots through /api/relay/v1/dm.
	// Empty = relay accepts anonymous requests (use only on trusted networks).
	RelayToken string `json:"relay_token,omitempty"`
}

// DefaultBotModeConfig returns default Bot Mode settings.
func DefaultBotModeConfig() *BotModeConfig {
	return &BotModeConfig{
		Enabled:           true,
		InjectBotProtocol: nil, // nil = enabled by default at runtime
	}
}

// MCPConfig represents MCP server configuration
type MCPConfig struct {
	Servers map[string]mcp.ServerConfig `json:"servers,omitempty"`
}

// SubAgentConfig represents subagent configuration
type SubAgentConfig struct {
	MaxConcurrent int           `json:"max_concurrent"`
	MaxDepth      int           `json:"max_depth"`
	Timeout       time.Duration `json:"timeout"`
}

// VoiceConfig represents voice configuration (alias for voice.VoiceConfig)
type VoiceConfig = voice.VoiceConfig

// ApprovalConfig represents command approval system configuration
type ApprovalConfig struct {
	Strategy         string `json:"strategy"`           // "smart", "manual", "auto"
	TrustThreshold   int    `json:"trust_threshold"`    // Auto-approve after N trusted uses
	EnableLearning   bool   `json:"enable_learning"`    // Learn from user decisions
	EnableCLIConfirm bool   `json:"enable_cli_confirm"` // Enable CLI confirmation prompt
	ApprovalTimeout  int    `json:"approval_timeout"`   // Approval timeout in seconds
	TimeoutStrategy  string `json:"timeout_strategy"`   // "deny", "allow_low_medium", "allow_all"
}

// DefaultApprovalConfig returns default approval configuration
func DefaultApprovalConfig() *ApprovalConfig {
	return &ApprovalConfig{
		Strategy:         "smart",
		TrustThreshold:   3,
		EnableLearning:   true,
		EnableCLIConfirm: false,
		ApprovalTimeout:  300,
		TimeoutStrategy:  "deny",
	}
}

// DefaultSubAgentConfig returns default subagent configuration
func DefaultSubAgentConfig() *SubAgentConfig {
	return &SubAgentConfig{
		MaxConcurrent: 3,
		MaxDepth:      2,
		Timeout:       120 * time.Second,
	}
}

func Load() (*Config, error) {
	magicHome := GetMagicHome()
	configPath := filepath.Join(magicHome, ConfigFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := defaultConfig()
			return cfg, ErrNoConfig
		}
		cfg := defaultConfig()
		return cfg, nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Default working_dir to "working" subdirectory of current directory if not set
	if cfg.WorkingDir == "" {
		cfg.WorkingDir = getDefaultWorkingDir()
	}

	// 配置里的路径允许写 `~`（config.example.json / docs/USAGE 推荐
	// `~/.magic/browser-profile` 这种写法），在唯一的读取入口展开成真实主目录。
	// 少了这一步，进程会把 `~` 当普通目录名，在**当前工作目录**（打包安装后
	// 就是安装目录）下建出一个名为 `~` 的字面量文件夹。
	cfg.WorkingDir = ExpandHome(cfg.WorkingDir)
	if cfg.BrowserProfileDir != nil {
		// 这里额外做一次"无 HOME 时改落到 magic home"的兜底，理由见
		// ResolveBrowserProfileDir。注意不能只依赖 ExpandHome：面板/守护进程起
		// 服务时常常没有 HOME，`~` 会原样留下来。
		dir := ResolveBrowserProfileDir(*cfg.BrowserProfileDir)
		cfg.BrowserProfileDir = &dir
	}
	if cfg.Memory.DBPath != nil {
		dbPath := ExpandHome(*cfg.Memory.DBPath)
		cfg.Memory.DBPath = &dbPath
	}

	// 兜底 Agent 循环上限默认值：磁盘 JSON 可能未写入 agent.max_turns 等字段
	// （旧配置或手动编辑），此时若为 0 会导致 server 端回退到 agent 硬编码的
	// 内置上限，与 Web 配置界面默认值不一致。这里补齐默认值，确保
	// 实际生效的上限与 UI 展示一致。
	// 取值依据：单个回合受回合时限（TurnTimeoutMinutes，默认 30 分钟）约束，
	// 而每轮迭代是一次 LLM 调用加工具执行（实测 10~30s），因此单回合物理可达
	// 的迭代数是 30min/10s≈180 到 30min/30s≈60 之间。旧默认 300 落在该区间
	// 之外——永远先撞时间墙，回合上限形同虚设。150 落在可达区间内，能真正起到
	// "止住失控循环"的作用；max_iterations 是 max_turns 之外的转向闸门，两者
	// 取先到者，故设为 200 使其同样是个有意义的约束。
	//
	// 注意：这几个默认值只在字段为 0 时补齐，用户显式写下的值一律不动。
	if cfg.Agent.MaxTurns == 0 {
		cfg.Agent.MaxTurns = 150
	}
	if cfg.Agent.MaxIterations == 0 {
		cfg.Agent.MaxIterations = 200
	}
	// 回合时限兜底。注意它**不随 max_turns 联动**：用户把时限调大后，
	// 只要不显式改 max_turns，上限仍是 150——两者是独立的闸门，改一个
	// 不会自动放开另一个（配置页文案已说明这点）。
	if cfg.Agent.TurnTimeoutMinutes == 0 {
		cfg.Agent.TurnTimeoutMinutes = DefaultTurnTimeoutMinutes
	}

	return &cfg, nil
}

// GetBrowserProfileDir 返回最终生效的浏览器持久 profile 目录（绝对路径）。
//   - 未配置（nil，老配置文件没这个键）→ 默认目录（见 defaultBrowserProfileDirAbs）
//   - 显式空字符串 → ""，表示不持久化、每次启动用全新临时 profile
//
// 调用方一律走这里，别直接读字段，否则会漏掉"默认值"这一支。
func (c *Config) GetBrowserProfileDir() string {
	if c == nil || c.BrowserProfileDir == nil {
		return defaultBrowserProfileDirAbs()
	}
	return ResolveBrowserProfileDir(*c.BrowserProfileDir)
}

// defaultBrowserProfileDirAbs 解析"没有显式配置 browser_profile_dir"时的默认目录。
//
// 口径分两支，**顺序不能调换**（调换过一次，CI 上 red 了，见 paths_test.go）：
//
//  1. 拿得到真实主目录 → `$REALHOME/.magic/browser-profile`，也就是展示值
//     DefaultBrowserProfileDir（`~/.magic/browser-profile`）的展开结果。
//     老部署的登录态就在这儿，这一支必须**一个字节都不能变**，否则升级后
//     所有人的 cookie/localStorage 会凭空消失。
//  2. 拿不到主目录（面板 / systemd `Restart=` 起的服务、`sudo` 清过环境、
//     容器里的裸 root）→ 退回**真实的 magic home**（GetMagicHome：
//     GO_MAGIC_HOME → HOME → UserHomeDir → /etc/passwd → /tmp）。
//
// 为什么第 2 支不能直接写成 `filepath.Join(GetMagicHome(), "browser-profile")`
// 一把梭：HOME 可用时 GetMagicHome() 本身就是 `$HOME/.magic`，Join 出来仍是
// `$HOME/.magic/browser-profile`，看着对；但这样得到的是**绝对路径**，
// ExpandHome 于是原样返回、函数提前返回，"重锚到 magic home"的逻辑永远走不到。
// 更要命的是它绕开了展示值的口径——配置里写的、界面上显示的、真正用的三者
// 从此不一致，排障时 `magic config list` 的 Profile Dir 会指到一个没人配过的
// 地方。所以第 1 支显式复用 ExpandHome(DefaultBrowserProfileDir)。
//
// 注意第 2 支里 magic home 自身可能带 `~`（HOME 缺失时 GetMagicHome 会拼出
// `~/.magic`），所以还要再兜一层 ExpandHome，末尾再兜一层"基于 CWD 绝对化"：
// 位置不理想，但至少是绝对路径，Chrome 不会再在站点目录里建出一个字面量 `~`。
func defaultBrowserProfileDirAbs() string {
	// 第 1 支：有真实主目录，保持约定位置不动。
	if dir := ExpandHome(DefaultBrowserProfileDir); !strings.Contains(dir, "~") {
		return dir
	}

	// 第 2 支：主目录不可用，落到 magic home 下。
	dir := ExpandHome(filepath.Join(GetMagicHome(), "browser-profile"))
	if !filepath.IsAbs(dir) {
		if wd, err := os.Getwd(); err == nil {
			return filepath.Join(wd, dir)
		}
	}
	return dir
}

// ResolveBrowserProfileDir 把一处 browser_profile_dir 取值解析成绝对路径：
// 展开 `~`，并把解析后仍带字面量 `~` 的（= HOME 缺失，ExpandHome 拿不到家目录）
// 落到 magic home 下的同名子目录。
//
// 这个兜底是为了"配置里残留 DefaultBrowserProfileDir / 文档示例里的
// `~/.magic/browser-profile`，但服务进程没有 HOME"那类部署：此时若原样交给
// Chrome，`--user-data-dir` 就是相对路径，会在站点目录下建出字面量 `~` 目录。
// 相对路径（用户有意为之）不带 `~`，原样返回，行为不变。
//
// 展示与默认值仍以 DefaultBrowserProfileDir（`~/.magic/browser-profile`）为准，
// 所以新写的配置文件里看到的还是那个值；这里只管"真正落盘用哪个目录"。
func ResolveBrowserProfileDir(dir string) string {
	if dir == "" {
		return ""
	}
	expanded := ExpandHome(dir)
	if !strings.Contains(expanded, "~") {
		return expanded
	}
	if rest := tildeSuffix(expanded); rest != "" {
		return filepath.Join(GetMagicHome(), rest)
	}
	return expanded
}

// tildeSuffix 从一条仍带波浪号的路径里取出"波浪号之后的部分"，取不到返回 ""。
//
// 只认纯 `~` 或以 `~` + 分隔符开头这一种形态（与 ExpandHome 同口径），并且只在
// 波浪号确实处在路径**开头**时才替换——`/tmp/~cache/x` 里那个 `~` 是普通字符，
// 不能碰。
func tildeSuffix(p string) string {
	p = strings.TrimPrefix(p, "./")
	if !strings.HasPrefix(p, "~") {
		return ""
	}
	rest := strings.TrimLeft(p[1:], `/\`)
	return strings.TrimPrefix(rest, ".magic/")
}

// strPtr 返回字符串字面量的指针，供指针语义的配置字段（nil = 用默认值）使用。
func strPtr(s string) *string { return &s }

// boolPtr 返回 bool 字面量的指针，供指针语义的配置字段（nil = 用默认值）使用。
func boolPtr(b bool) *bool { return &b }

// BrowserHeadlessSource 说明 GetBrowserHeadless 的返回值是哪里来的。
// 只为排障/日志用——只报一个 true/false 的话，用户没法知道是自己的配置生效了、
// 还是被环境变量或默认值覆盖了。
type BrowserHeadlessSource string

const (
	// BrowserHeadlessFromEnv 来自 BROWSER_HEADLESS 环境变量（优先级最高）。
	BrowserHeadlessFromEnv BrowserHeadlessSource = "env:BROWSER_HEADLESS"
	// BrowserHeadlessFromConfig 来自配置文件的 browser_headless 字段。
	BrowserHeadlessFromConfig BrowserHeadlessSource = "config:browser_headless"
	// BrowserHeadlessFromDefault 既没配也没设环境变量，取内置默认值。
	BrowserHeadlessFromDefault BrowserHeadlessSource = "default"
)

// GetBrowserHeadless 返回自动化浏览器是否应该无头运行。
//
// 优先级（高 → 低）：
//  1. BROWSER_HEADLESS 环境变量：认 "false"/"0"/"no"/"off" 为关，其余非空值为开。
//     只认 "true" 会让 `BROWSER_HEADLESS=false` 被静默当成没设，反过来也踩坑。
//  2. 配置文件的 browser_headless 字段（显式 true / false）。
//  3. 内置默认值 DefaultBrowserHeadless（true = 无头）。
//
// 调用方一律走这里，别直接读字段，否则会漏掉"环境变量覆盖"和"默认值"两支。
func (c *Config) GetBrowserHeadless() bool {
	v, _ := c.GetBrowserHeadlessWithSource()
	return v
}

// GetBrowserHeadlessWithSource 同 GetBrowserHeadless，另外返回取值来源。
func (c *Config) GetBrowserHeadlessWithSource() (bool, BrowserHeadlessSource) {
	if raw, ok := os.LookupEnv("BROWSER_HEADLESS"); ok {
		// 空字符串等同于没设：`BROWSER_HEADLESS=` 这种写法在 shell 脚本里
		// 很常见（尤其是 `docker run -e BROWSER_HEADLESS` 不带值），
		// 把它当成"显式开启"会让用户莫名其妙地失去有头窗口。
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			return ParseBoolish(trimmed, DefaultBrowserHeadless), BrowserHeadlessFromEnv
		}
	}
	if c != nil && c.BrowserHeadless != nil {
		return *c.BrowserHeadless, BrowserHeadlessFromConfig
	}
	return DefaultBrowserHeadless, BrowserHeadlessFromDefault
}

// ParseBoolish 解析人类可写的布尔值（"1"/"true"/"yes"/"on"、"0"/"false"/"no"/"off"）。
// 认不出来时返回 def（不报错）：环境变量里写错一个词就让浏览器起不来，
// 比"按默认值走"糟糕得多。导出是为了让 internal/tool 在拿不到配置文件时
// （首次运行 / 不经过 server 的 TUI 会话）复用同一套判定。
func ParseBoolish(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on", "y", "t":
		return true
	case "0", "false", "no", "off", "n", "f":
		return false
	}
	return def
}

func defaultConfig() *Config {
	return &Config{
		Profile:    "default",
		MagicHome:  "~/.magic",
		WorkingDir: getDefaultWorkingDir(),
		Provider:   "deepseek",
		Model:      "deepseek-v4-flash",
		Mode:       "chat",
		// 默认就带上持久 profile 目录：浏览器登录态（cookie/localStorage）跨
		// 会话保留。显式写成 "" 才会退回每次全新的临时 profile。
		BrowserProfileDir: strPtr(DefaultBrowserProfileDir),
		// 默认无头：服务器/容器没有显示服务，有头 Chrome 会直接起不来。
		BrowserHeadless: boolPtr(DefaultBrowserHeadless),
		Cortex: CortexConfig{
			Enabled:             true,
			SkillMinPatternFreq: 3,
		},
		Memory: MemoryConfig{
			Enabled: true,
		},
		Providers: make(map[string]ProviderConfig),
		Tools: ToolsConfig{
			Enabled: []string{"all"},
		},
		Skills: SkillsConfig{
			Enabled:    []string{"all"},
			DefaultDir: "skills",
			UserDir:    "skills",
		},
		Plugins: PluginsConfig{
			Enabled: []string{"all"},
		},
		Gateway: GatewayConfig{
			Enabled:   false,
			Platforms: make(map[string]PlatformConfig),
		},
		BotMode: DefaultBotModeConfig(),
		Voice:   voice.DefaultVoiceConfig(),
		// Agent 循环上限默认值，与 Web 配置界面(ConfigView.vue)的默认一致，
		// 避免新建配置时回退到 agent 内置的上限。
		Agent: struct {
			MaxTurns           int   `json:"max_turns,omitempty"`
			MaxIterations      int   `json:"max_iterations,omitempty"`
			MaxTokenBudget     int64 `json:"max_token_budget,omitempty"`
			TurnTimeoutMinutes int   `json:"turn_timeout_minutes,omitempty"`
		}{
			MaxTurns:           150,
			MaxIterations:      200,
			TurnTimeoutMinutes: DefaultTurnTimeoutMinutes,
		},
	}
}

// DefaultConfig returns a default configuration (exported version)
func DefaultConfig() *Config {
	return defaultConfig()
}

// GetConfigDir returns the configuration directory path.
// Uses GO_MAGIC_HOME environment variable if set, otherwise ~/.magic.
func GetConfigDir() string {
	return GetMagicHome()
}

// getDefaultWorkingDir returns the default working directory.
// 当 WorkingDir 配置为空时，使用 magicHome 下的 "workspace" 目录，
// 并确保该目录存在（先创建 workspace 目录，对话子目录再放到里面）。
func getDefaultWorkingDir() string {
	magicHome := GetMagicHome()
	workspaceDir := filepath.Join(magicHome, "workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err == nil {
		return workspaceDir
	}
	// 创建失败时回退到当前工作目录
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// Save saves the configuration to disk.
// It uses a safe write approach to avoid data loss:
// 1. First reads existing config from disk to preserve any fields not in memory
// 2. Merges in-memory changes on top
// 3. Writes result to a temp file, then renames
func (c *Config) Save() error {
	magicHome := GetMagicHome()

	configDir := magicHome
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, ConfigFileName)

	// Step 1: Try to read existing config to preserve values not in memory
	existingData, readErr := os.ReadFile(configPath)
	var existingCfg Config
	hasExisting := readErr == nil && json.Unmarshal(existingData, &existingCfg) == nil

	// Step 2: Merge - preserve existing provider fields that might be empty in current config
	if hasExisting {
		for name, existingProv := range existingCfg.Providers {
			if currentProv, ok := c.Providers[name]; ok {
				// Preserve API key if current is empty but exists on disk
				if currentProv.APIKey == "" && existingProv.APIKey != "" {
					currentProv.APIKey = existingProv.APIKey
				}
				// Preserve BaseURL if current is empty but exists on disk
				if currentProv.BaseURL == "" && existingProv.BaseURL != "" {
					currentProv.BaseURL = existingProv.BaseURL
				}
				// Preserve Model if current is empty but exists on disk
				if currentProv.Model == "" && existingProv.Model != "" {
					currentProv.Model = existingProv.Model
				}
				// Preserve Models if current is empty but exists on disk
				if len(currentProv.Models) == 0 && len(existingProv.Models) > 0 {
					currentProv.Models = existingProv.Models
				}
				c.Providers[name] = currentProv
			}
		}

		// Preserve Voice config API keys if current is empty but exists on disk
		if c.Voice != nil && existingCfg.Voice != nil {
			// Preserve global API key
			if c.Voice.APIKey == "" && existingCfg.Voice.APIKey != "" {
				c.Voice.APIKey = existingCfg.Voice.APIKey
			}
			// Preserve region
			if c.Voice.Region == "" && existingCfg.Voice.Region != "" {
				c.Voice.Region = existingCfg.Voice.Region
			}
			// Preserve provider-specific credentials
			if c.Voice.Providers == nil {
				c.Voice.Providers = make(map[string]voice.ProviderCredentials)
			}
			for provName, existingCreds := range existingCfg.Voice.Providers {
				if currentCreds, ok := c.Voice.Providers[provName]; ok {
					if currentCreds.APIKey == "" && existingCreds.APIKey != "" {
						currentCreds.APIKey = existingCreds.APIKey
					}
					if currentCreds.Region == "" && existingCreds.Region != "" {
						currentCreds.Region = existingCreds.Region
					}
					c.Voice.Providers[provName] = currentCreds
				} else {
					c.Voice.Providers[provName] = existingCreds
				}
			}
		}
	}

	// Step 3: Marshal and write safely (write to temp file first, then rename)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Write to temp file first
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}

	// Rename temp file to actual path (atomic on most OS)
	if err := os.Rename(tmpPath, configPath); err != nil {
		// Fallback: try direct write
		return os.WriteFile(configPath, data, 0600)
	}

	return nil
}
