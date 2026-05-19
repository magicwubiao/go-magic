package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config represents the main application configuration
type Config struct {
	Profile         string                 `json:"profile"`
	MagicHome       string                 `json:"magic_home"`
	Provider        string                 `json:"provider"`
	Model           string                 `json:"model"`
	Providers       map[string]ProviderCfg `json:"providers"`
	Tools           ToolsConfig            `json:"tools"`
	OptionalTools   []string               `json:"optional_tools"` // Optional tools to enable (image_gen, tts, video_analyze)
	Gateway         GatewayConfig          `json:"gateway"`
	Agent           AgentConfig            `json:"agent"`
	Memory          MemoryConfig           `json:"memory"`
	Kanban          KanbanConfig           `json:"kanban"`
	Notification    NotificationConfig     `json:"notification"`     // Email and SMS notification configuration
	ImageGen        ImageGenConfig         `json:"image_gen"`        // Image generation configuration
	SecretRedaction bool                   `json:"secret_redaction"` // Secret redaction (API keys, tokens, etc.)
	CortexEnabled   bool                   `json:"cortex_enabled"`   // Enable Cortex Agent six-system integration
	WorkingDir      string                 `json:"working_dir"`      // Working directory for file operations
	Security        *SecurityConfig        `json:"-"`
	SecurityPath    string                 `json:"-"`
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
	GoalMaxTurns       int     `json:"goal_max_turns"` // Max turns for /goal command, default 20
	SameToolLimit      int     `json:"same_tool_limit"`      // Same tool call limit (default: 3)
	ConsecutiveLimit   int     `json:"consecutive_limit"`    // Consecutive tool call limit (default: 10)
}

// MemoryConfig represents memory configuration
type MemoryConfig struct {
	Enabled     bool   `json:"enabled"`
	DBPath      string `json:"db_path"`
	AutoRecall  bool   `json:"auto_recall"`
	RecallLimit int    `json:"recall_limit"`
}

// KanbanConfig represents kanban board configuration
type KanbanConfig struct {
	Enabled                bool          `json:"enabled"`
	DBPath                 string        `json:"db_path"`                  // Default: ~/.magic/kanban.db
	DispatcherTick         time.Duration `json:"dispatcher_tick"`          // Default: 60s
	MaxRetries             int           `json:"max_retries"`              // Default: 3
	MaxConsecutiveFailures int           `json:"max_consecutive_failures"` // Default: 5
	DefaultMaxRuntime      int           `json:"default_max_runtime"`      // Default: 3600s (1 hour)
}

// EmailConfig represents email (SMTP) configuration
type EmailConfig struct {
	SMTPHost     string `json:"smtp_host"`     // SMTP server host
	SMTPPort     int    `json:"smtp_port"`     // SMTP server port
	Username     string `json:"username"`      // SMTP username
	Password     string `json:"password"`      // SMTP password or app-specific password
	From         string `json:"from"`          // From email address
	FromName     string `json:"from_name"`     // From name
	UseTLS       bool   `json:"use_tls"`       // Use TLS
	UseStartTLS  bool   `json:"use_starttls"`  // Use STARTTLS
	InsecureSkip bool   `json:"insecure_skip"` // Skip TLS certificate verification
}

// TwilioConfig represents Twilio SMS configuration
type TwilioConfig struct {
	AccountSID string `json:"account_sid"` // Twilio Account SID
	AuthToken  string `json:"auth_token"`  // Twilio Auth Token
	FromNumber string `json:"from_number"` // Twilio phone number
}

// AliyunSMSConfig represents Aliyun SMS configuration
type AliyunSMSConfig struct {
	AccessKeyID     string `json:"access_key_id"`     // Aliyun AccessKey ID
	AccessKeySecret string `json:"access_key_secret"` // Aliyun AccessKey Secret
	SignName        string `json:"sign_name"`         // SMS signature name
	Endpoint        string `json:"endpoint"`          // API endpoint (optional)
}

// TencentSMSConfig represents Tencent Cloud SMS configuration
type TencentSMSConfig struct {
	SecretID  string `json:"secret_id"`  // Tencent Secret ID
	SecretKey string `json:"secret_key"` // Tencent Secret Key
	SDKAppID  string `json:"sdk_app_id"` // SMS SDK App ID
	SignName  string `json:"sign_name"`  // SMS signature name
	Endpoint  string `json:"endpoint"`   // API endpoint (optional)
}

// SMSConfig represents SMS service configuration
type SMSConfig struct {
	Provider string          `json:"provider"` // Provider: twilio, aliyun, tencent
	Twilio   TwilioConfig    `json:"twilio"`
	Aliyun   AliyunSMSConfig `json:"aliyun"`
	Tencent  TencentSMSConfig `json:"tencent"`
}

// NotificationConfig represents notification service configuration
type NotificationConfig struct {
	Email EmailConfig `json:"email"`
	SMS   SMSConfig   `json:"sms"`
}

// ImageGenConfig represents image generation configuration
type ImageGenConfig struct {
	Provider        string `json:"provider"`          // Provider: dall-e, stable-diffusion, midjourney, together
	APIKey          string `json:"api_key"`           // API key for the provider
	BaseURL         string `json:"base_url"`          // Custom base URL (optional)
	DefaultSize     string `json:"default_size"`      // Default image size
	DefaultStyle    string `json:"default_style"`     // Default art style
	OutputDirectory string `json:"output_directory"`  // Output directory for generated images
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
		Profile:         "default",
		Provider:        "openai",
		Model:           "gpt-4",
		SecretRedaction: true, // Default ON for security
		Tools: ToolsConfig{
			Enabled:  []string{"all"},
			Disabled: []string{},
		},
		Agent: AgentConfig{
			MaxTurns:           60,
			MaxIterations:      80,
			CompressionEnabled: true,
			CompressionRatio:   0.7,
			ContextWindow:      200000,
			GoalMaxTurns:       20,
		},
		Memory: MemoryConfig{
			Enabled:     true,
			AutoRecall:  true,
			RecallLimit: 5,
		},
		Kanban: KanbanConfig{
			Enabled:               true,
			DBPath:                "",
			DispatcherTick:        60 * time.Second,
			MaxRetries:            3,
			MaxConsecutiveFailures: 5,
			DefaultMaxRuntime:     3600,
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

	// Secret redaction defaults to true (Hermes v0.13.0 behavior)
	// Note: bool default in Go is false, so we only set true if not explicitly configured
	// For JSON configs, if the field is omitted, we want it true

	if c.Agent.MaxTurns == 0 {
		c.Agent.MaxTurns = 60
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

	// Kanban defaults
	if c.Kanban.DispatcherTick == 0 {
		c.Kanban.DispatcherTick = 60 * time.Second
	}
	if c.Kanban.MaxRetries == 0 {
		c.Kanban.MaxRetries = 3
	}
	if c.Kanban.MaxConsecutiveFailures == 0 {
		c.Kanban.MaxConsecutiveFailures = 5
	}
	if c.Kanban.DefaultMaxRuntime == 0 {
		c.Kanban.DefaultMaxRuntime = 3600
	}

	// Cortex disabled by default
	// Note: bool default in Go is false, which is the correct default

	// WorkingDir defaults to current directory if not set
	if c.WorkingDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			c.WorkingDir = cwd
		}
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
