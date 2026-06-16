package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/privacy"
	"github.com/magicwubiao/go-magic/internal/voice"
)

const (
	DefaultMagicHome = "~/.magic"
	ConfigFileName   = "config.json"
)

// ErrNoConfig indicates that no config file exists (first run).
var ErrNoConfig = fmt.Errorf("config file not found")

// Config represents the application configuration
type Config struct {
	Profile    string                    `json:"profile"`
	MagicHome  string                    `json:"magic_home"`
	WorkingDir string                    `json:"working_dir,omitempty"`
	Provider   string                    `json:"provider"`
	Model      string                    `json:"model"` // Deprecated: Use Providers[].Models[0] instead
	Providers  map[string]ProviderConfig `json:"providers"`
	Tools      ToolsConfig               `json:"tools"`
	Skills     SkillsConfig              `json:"skills"`
	Plugins    PluginsConfig             `json:"plugins"`
	Memory     MemoryConfig              `json:"memory"`
	Gateway    GatewayConfig             `json:"gateway"`
	Cortex     CortexConfig              `json:"cortex"`
	MCP        *MCPConfig                `json:"mcp,omitempty"`
	SubAgent   *SubAgentConfig           `json:"subagent,omitempty"`
	Voice      *VoiceConfig              `json:"voice,omitempty"`
	Privacy    *privacy.Config           `json:"privacy,omitempty"`
	Display    DisplayConfig             `json:"display,omitempty"`
	// Agent settings
	SecretRedaction bool   `json:"secret_redaction,omitempty"`
	Mode            string `json:"mode,omitempty"`            // chat, plan, act
	ChatMode        string `json:"chat_mode,omitempty"`       // chat, coding - default mode for magic chat
	AutoLinkGoals   bool   `json:"auto_link_goals,omitempty"` // Auto-link new sessions to active goals
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
}

// CortexConfig represents Cortex AI configuration
type CortexConfig struct {
	Enabled             bool `json:"enabled"`                // Enable/disable Cortex system
	SkillMinPatternFreq int  `json:"skill_min_pattern_freq"` // Min frequency for skill pattern detection
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
}

// DefaultApprovalConfig returns default approval configuration
func DefaultApprovalConfig() *ApprovalConfig {
	return &ApprovalConfig{
		Strategy:         "smart",
		TrustThreshold:   1,
		EnableLearning:   true,
		EnableCLIConfirm: false,
		ApprovalTimeout:  300,
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
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(home, ".magic", ConfigFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), ErrNoConfig
		}
		return defaultConfig(), nil
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
func GetConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".magic")
}

// getDefaultWorkingDir returns the default working directory.
// It uses the "working" subdirectory of the current working directory.
// If the "working" directory does not exist, falls back to the current directory.
func getDefaultWorkingDir() string {
	if wd, err := os.Getwd(); err == nil {
		workingDir := filepath.Join(wd, "working")
		if info, err := os.Stat(workingDir); err == nil && info.IsDir() {
			return workingDir
		}
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
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configDir := filepath.Join(home, ".magic")
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
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	// Rename temp file to actual path (atomic on most OS)
	if err := os.Rename(tmpPath, configPath); err != nil {
		// Fallback: try direct write
		return os.WriteFile(configPath, data, 0644)
	}

	return nil
}
