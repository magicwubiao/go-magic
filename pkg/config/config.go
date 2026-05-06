package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/magicwubiao/go-magic/internal/mcp"
	"github.com/magicwubiao/go-magic/internal/privacy"
	"github.com/magicwubiao/go-magic/internal/subagent"
	"github.com/magicwubiao/go-magic/internal/voice"
)

const (
	DefaultMagicHome = "~/.magic"
	ConfigFileName   = "config.json"
)

// Config represents the application configuration
type Config struct {
	Profile   string                    `json:"profile"`
	MagicHome string                    `json:"magic_home"`
	Provider  string                    `json:"provider"`
	Model     string                    `json:"model"`
	Providers map[string]ProviderConfig `json:"providers"`
	Tools     ToolsConfig               `json:"tools"`
	Gateway   GatewayConfig             `json:"gateway"`
	MCP       *MCPConfig                `json:"mcp,omitempty"`
	SubAgent  *SubAgentConfig           `json:"subagent,omitempty"`
	Voice     *VoiceConfig              `json:"voice,omitempty"`
	Privacy   *privacy.Config           `json:"privacy,omitempty"`
}

// ProviderConfig represents provider configuration
type ProviderConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
}

// ToolsConfig represents tools configuration
type ToolsConfig struct {
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
	Token   string `json:"token"`
	Enabled bool   `json:"enabled"`
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

// VoiceConfig represents voice configuration (alias for voice.Config)
type VoiceConfig = voice.Config

func DefaultSubAgentConfig() *subagent.Config {
	return subagent.DefaultConfig()
}

func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(home, ".magic", ConfigFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return defaultConfig(), nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Profile:   "default",
		MagicHome: "~/.magic",
		Provider:  "openai",
		Model:     "gpt-4",
		Providers: make(map[string]ProviderConfig),
		Tools: ToolsConfig{
			Enabled: []string{"all"},
		},
		Gateway: GatewayConfig{
			Enabled:   false,
			Platforms: make(map[string]PlatformConfig),
		},
	}
}

// DefaultConfig returns a default configuration (exported version)
func DefaultConfig() *Config {
	return defaultConfig()
}

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
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}
