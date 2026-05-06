package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config represents the main application configuration
type Config struct {
	Profile      string                 `json:"profile"`
	MagicHome    string                 `json:"magic_home"`
	Provider     string                 `json:"provider"`
	Model        string                 `json:"model"`
	Providers    map[string]ProviderCfg `json:"providers"`
	Tools        ToolsConfig            `json:"tools"`
	Gateway      GatewayConfig          `json:"gateway"`
	Agent        AgentConfig            `json:"agent"`
	Memory       MemoryConfig           `json:"memory"`
	Security     *SecurityConfig        `json:"-"`
	SecurityPath string                 `json:"-"`
}

// ProviderCfg represents a provider configuration
type ProviderCfg struct {
	Provider  string                 `json:"provider,omitempty"`
	APIKey    string                 `json:"api_key,omitempty"`
	BaseURL   string                 `json:"base_url,omitempty"`
	Model     string                 `json:"model,omitempty"`
	Proxy     string                 `json:"proxy,omitempty"`
	Fallback  []string               `json:"fallback,omitempty"` // Fallback models
	ExtraBody map[string]interface{} `json:"extra_body,omitempty"`
}

// ToolsConfig represents tools configuration
type ToolsConfig struct {
	Enabled  []string `json:"enabled"`
	Disabled []string `json:"disabled"`
}

// GatewayConfig represents gateway configuration
type GatewayConfig struct {
	Enabled   bool                   `json:"enabled"`
	Platforms map[string]PlatformCfg `json:"platforms"`
}

// PlatformCfg represents a platform configuration
type PlatformCfg struct {
	Token     string `json:"token,omitempty"`
	Secret    string `json:"secret,omitempty"`
	AppKey    string `json:"app_key,omitempty"`
	AppSecret string `json:"app_secret,omitempty"`
	Enabled   bool   `json:"enabled"`
	ProxyURL  string `json:"proxy_url,omitempty"`
}

// AgentConfig represents agent configuration
type AgentConfig struct {
	MaxTurns           int     `json:"max_turns"`
	MaxIterations      int     `json:"max_iterations"`
	MaxTokenBudget     int64   `json:"max_token_budget"`
	CompressionEnabled bool    `json:"compression_enabled"`
	CompressionRatio   float64 `json:"compression_ratio"`
	ContextWindow      int     `json:"context_window"`
}

// MemoryConfig represents memory configuration
type MemoryConfig struct {
	Enabled     bool   `json:"enabled"`
	DBPath      string `json:"db_path"`
	AutoRecall  bool   `json:"auto_recall"`
	RecallLimit int    `json:"recall_limit"`
}

// Load loads configuration from a file with environment variable overrides
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := json.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply defaults
	cfg.applyDefaults()

	// Load security config if exists
	dir := filepath.Dir(path)
	securityPath := filepath.Join(dir, ".security.yml")
	if secCfg, err := LoadSecurityConfig(securityPath); err == nil {
		cfg.Security = secCfg
		cfg.SecurityPath = securityPath
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// LoadFromDefault loads configuration from default locations
func LoadFromDefault() (*Config, error) {
	// Try common locations
	locations := []string{
		"config.json",
		".config.json",
		"~/.magic/config.json",
		"/etc/magic/config.json",
	}

	for _, loc := range locations {
		expanded := os.ExpandEnv(loc)
		if data, err := os.ReadFile(expanded); err == nil {
			var cfg Config
			if err := json.Unmarshal(data, &cfg); err == nil {
				cfg.applyDefaults()
				return &cfg, nil
			}
		}
	}

	return DefaultConfig(), nil
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Profile:  "default",
		Provider: "openai",
		Model:    "gpt-4",
		Tools: ToolsConfig{
			Enabled:  []string{"all"},
			Disabled: []string{},
		},
		Agent: AgentConfig{
			MaxTurns:           10,
			MaxIterations:      50,
			CompressionEnabled: true,
			CompressionRatio:   0.7,
			ContextWindow:      200000,
		},
		Memory: MemoryConfig{
			Enabled:     true,
			AutoRecall:  true,
			RecallLimit: 5,
		},
		Security: DefaultSecurityConfig(),
	}
}

// Save saves configuration to a file
func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Provider == "" {
		return fmt.Errorf("provider is required")
	}

	// Validate provider exists in providers map
	if _, ok := c.Providers[c.Provider]; !ok {
		// Check if it's a known provider type
		if !isKnownProvider(c.Provider) {
			return fmt.Errorf("unknown provider: %s", c.Provider)
		}
	}

	// Validate agent settings
	if c.Agent.MaxTurns <= 0 {
		return fmt.Errorf("max_turns must be positive")
	}

	if c.Agent.CompressionRatio <= 0 || c.Agent.CompressionRatio > 1 {
		return fmt.Errorf("compression_ratio must be between 0 and 1")
	}

	// Validate memory settings
	if c.Memory.RecallLimit <= 0 {
		return fmt.Errorf("recall_limit must be positive")
	}

	return nil
}

func (c *Config) applyDefaults() {
	if c.Profile == "" {
		c.Profile = "default"
	}

	if c.Agent.MaxTurns == 0 {
		c.Agent.MaxTurns = 10
	}

	if c.Agent.MaxIterations == 0 {
		c.Agent.MaxIterations = 50
	}

	if c.Agent.CompressionRatio == 0 {
		c.Agent.CompressionRatio = 0.7
	}

	if c.Memory.RecallLimit == 0 {
		c.Memory.RecallLimit = 5
	}

	// Apply default provider settings if not specified
	for name, prov := range c.Providers {
		if prov.Provider == "" {
			prov.Provider = name
		}
		c.Providers[name] = prov
	}
}

func isKnownProvider(name string) bool {
	knownProviders := map[string]bool{
		"openai":     true,
		"anthropic":  true,
		"deepseek":   true,
		"zhipu":      true,
		"qwen":       true,
		"kimi":       true,
		"minimax":    true,
		"dashscope":  true,
		"openrouter": true,
		"ollama":     true,
		"vllm":       true,
	}
	return knownProviders[name]
}

// GetProviderConfig returns the configuration for a specific provider
func (c *Config) GetProviderConfig(name string) (*ProviderCfg, error) {
	// Try direct match first
	if prov, ok := c.Providers[name]; ok {
		return &prov, nil
	}

	// Try to extract provider from model name (provider/model format)
	if strings.Contains(name, "/") {
		parts := strings.SplitN(name, "/", 2)
		if prov, ok := c.Providers[parts[0]]; ok {
			return &prov, nil
		}
	}

	return nil, fmt.Errorf("provider config not found: %s", name)
}

// GetFallbackChain returns the fallback chain for a provider
func (c *Config) GetFallbackChain(name string) []string {
	prov, err := c.GetProviderConfig(name)
	if err != nil {
		return nil
	}
	return prov.Fallback
}
