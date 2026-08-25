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
	MCP          *MCPConfig                `json:"mcp,omitempty"`
	SubAgent     *SubAgentConfig           `json:"subagent,omitempty"`
	Voice        *VoiceConfig              `json:"voice,omitempty"`
	// Bot Mode: named agent profiles with persistent canonical chats
	BotMode *BotModeConfig `json:"bot_mode,omitempty"`
	// Privacy / PII 脱敏配置，统一存储于 config.json（团队约定：一个配置管所有）。
	Privacy *privacy.Config `json:"privacy,omitempty"`
	Display DisplayConfig   `json:"display,omitempty"`
	Server  ServerConfig    `json:"server,omitempty"`
	// Agent settings
	SecretRedaction bool   `json:"secret_redaction,omitempty"`
	Mode            string `json:"mode,omitempty"`      // chat, plan, act
	ChatMode        string `json:"chat_mode,omitempty"` // chat, coding - default mode for magic chat
	Agent           struct {
		GoalMaxTurns int `json:"goal_max_turns"`
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

// ServerConfig represents server-related configuration
type ServerConfig struct {
	UploadURLPrefix string `json:"upload_url_prefix,omitempty"` // Public URL prefix for uploaded files (e.g., "https://your-domain.com/uploads")
	FileStrategy    string `json:"file_strategy,omitempty"`     // "auto" (default), "url", "base64"
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
}

// PlatformConfig represents platform-specific configuration
type PlatformConfig struct {
	Token   string `json:"token,omitempty"`
	Enabled bool   `json:"enabled"`
	// Channel allowlist/blocklist - only respond to messages from allowed channels
	AllowedChannels []string `json:"allowed_channels,omitempty"` // Whitelist of channel/chat IDs; empty means allow all
	BlockedChannels []string `json:"blocked_channels,omitempty"` // Blacklist of channel/chat IDs; takes precedence over whitelist
	// WeCom fields
	CorpID  string `json:"corp_id,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
	Secret  string `json:"secret,omitempty"`
	// QQ fields
	Number   string `json:"number,omitempty"`
	Password string `json:"password,omitempty"`
	// DingTalk fields
	AppKey    string `json:"app_key,omitempty"`
	AppSecret string `json:"app_secret,omitempty"`
	// Feishu/Lark fields
	AppID  string `json:"app_id,omitempty"`
	APIURL string `json:"api_url,omitempty"`
	APIKey string `json:"api_key,omitempty"`
	// WhatsApp fields
	VerifyToken string `json:"verify_token,omitempty"`
	Mode        string `json:"mode,omitempty"` // WhatsApp: "personal" (QR login, default) or "business" (API)
	// WeCom fields: Mode = "qr" (QR code login, default) or "app" (API callback mode)
	// WeChat fields: Mode = "qr" (QR code login, default) or "callback" (webhook mode)
	// WeChat ClawBot fields
	AESKey string `json:"aes_key,omitempty"`
	// WeChat ClawBot fields
	ClientID  string `json:"client_id,omitempty"`
	DataDir   string `json:"data_dir,omitempty"`
	AutoLogin bool   `json:"auto_login,omitempty"`
	// Slack/Line/Matrix fields
}

// BotModeConfig enables/disables Bot Mode and tunes its behavior.
// Bots themselves are defined as files under <magicHome>/bots/<name>.json.
type BotModeConfig struct {
	Enabled bool `json:"enabled"`
	// InjectBotProtocol adds a short bot-to-bot messaging protocol section
	// (mention tags, message_agent usage) to every bot's system prompt.
	InjectBotProtocol *bool `json:"inject_bot_protocol,omitempty"`
}

// DefaultBotModeConfig returns default Bot Mode settings.
func DefaultBotModeConfig() *BotModeConfig {
	return &BotModeConfig{
		Enabled:           false,
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

	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Profile:    "default",
		MagicHome:  "~/.magic",
		WorkingDir: getDefaultWorkingDir(),
		Provider:   "deepseek",
		Model:      "deepseek-chat",
		Mode:       "chat",
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
		Voice: voice.DefaultVoiceConfig(),
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
