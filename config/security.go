package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SecurityConfig represents the security configuration
type SecurityConfig struct {
	// PII handling
	PII PIIConfig `yaml:"pii" json:"pii"`

	// Command approval
	Commands CommandSecurityConfig `yaml:"commands" json:"commands"`

	// Hooks
	Hooks HooksConfig `yaml:"hooks" json:"hooks"`

	// Rate limiting
	RateLimit RateLimitConfig `yaml:"rate_limit" json:"rate_limit"`
}

// PIIConfig configures PII handling
type PIIConfig struct {
	Enabled        bool              `yaml:"enabled" json:"enabled"`
	RedactPhone    bool              `yaml:"redact_phone" json:"redact_phone"`
	RedactEmail    bool              `yaml:"redact_email" json:"redact_email"`
	RedactIDCard   bool              `yaml:"redact_id_card" json:"redact_id_card"`
	RedactBankCard bool              `yaml:"redact_bank_card" json:"redact_bank_card"`
	RedactIP       bool              `yaml:"redact_ip" json:"redact_ip"`
	RedactAddress  bool              `yaml:"redact_address" json:"redact_address"`
	CustomPatterns map[string]string `yaml:"custom_patterns" json:"custom_patterns"`
}

// CommandSecurityConfig configures command security
type CommandSecurityConfig struct {
	AutoApproveSafe       bool     `yaml:"auto_approve_safe" json:"auto_approve_safe"`
	AutoRejectDangerous   bool     `yaml:"auto_reject_dangerous" json:"auto_reject_dangerous"`
	RequireApprovalLevels []string `yaml:"require_approval_levels" json:"require_approval_levels"` // "medium", "dangerous
	AllowedCommands       []string `yaml:"allowed_commands" json:"allowed_commands"`               // Whitelist
	BlockedCommands       []string `yaml:"blocked_commands" json:"blocked_commands"`               // Blacklist
}

// HooksConfig configures hooks
type HooksConfig struct {
	Enabled []string            `yaml:"enabled" json:"enabled"` // List of enabled hooks
	Process []ProcessHookConfig `yaml:"process" json:"process"` // External process hooks
}

// ProcessHookConfig configures an external process hook
type ProcessHookConfig struct {
	Name          string   `yaml:"name" json:"name"`
	Command       []string `yaml:"command" json:"command"`
	Dir           string   `yaml:"dir" json:"dir"`
	Env           []string `yaml:"env" json:"env"`
	ObserveKinds  []string `yaml:"observe_kinds" json:"observe_kinds"`
	InterceptLLM  bool     `yaml:"intercept_llm" json:"intercept_llm"`
	InterceptTool bool     `yaml:"intercept_tool" json:"intercept_tool"`
	ApproveTool   bool     `yaml:"approve_tool" json:"approve_tool"`
}

// RateLimitConfig configures rate limiting
type RateLimitConfig struct {
	Enabled              bool  `yaml:"enabled" json:"enabled"`
	MaxRequestsPerMinute int   `yaml:"max_requests_per_minute" json:"max_requests_per_minute"`
	MaxTokenBudget       int64 `yaml:"max_token_budget" json:"max_token_budget"`
}

// DefaultSecurityConfig returns the default security configuration
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		PII: PIIConfig{
			Enabled:        true,
			RedactPhone:    true,
			RedactEmail:    true,
			RedactIDCard:   true,
			RedactBankCard: true,
			RedactIP:       true,
			RedactAddress:  true,
		},
		Commands: CommandSecurityConfig{
			AutoApproveSafe:       true,
			AutoRejectDangerous:   true,
			RequireApprovalLevels: []string{"dangerous"},
		},
		Hooks: HooksConfig{
			Enabled: []string{"privacy"},
		},
		RateLimit: RateLimitConfig{
			Enabled:              true,
			MaxRequestsPerMinute: 60,
			MaxTokenBudget:       100000,
		},
	}
}

// LoadSecurityConfig loads security configuration from file
func LoadSecurityConfig(path string) (*SecurityConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultSecurityConfig(), nil
		}
		return nil, fmt.Errorf("failed to read security config: %w", err)
	}

	// Try YAML parsing
	config, err := parseSecurityConfigYAML(string(data))
	if err != nil {
		// Fallback to JSON parsing
		config, err = parseSecurityConfigJSON(string(data))
		if err != nil {
			return nil, fmt.Errorf("failed to parse security config: %w", err)
		}
	}

	return config, nil
}

// Simple YAML parser for basic security config
func parseSecurityConfigYAML(data string) (*SecurityConfig, error) {
	cfg := &SecurityConfig{
		PII: PIIConfig{
			Enabled:        true,
			RedactPhone:    true,
			RedactEmail:    true,
			RedactIDCard:   true,
			RedactBankCard: true,
			RedactIP:       true,
			RedactAddress:  true,
			CustomPatterns: make(map[string]string),
		},
		Commands: CommandSecurityConfig{
			AutoApproveSafe:       true,
			AutoRejectDangerous:   true,
			RequireApprovalLevels: []string{"dangerous"},
		},
		Hooks: HooksConfig{
			Enabled: []string{"privacy"},
		},
		RateLimit: RateLimitConfig{
			Enabled:              true,
			MaxRequestsPerMinute: 60,
			MaxTokenBudget:       100000,
		},
	}

	// Simple line-by-line parsing for key values
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse simple key: value pairs
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			value = strings.Trim(value, "\"")

			switch key {
			case "enabled":
				if value == "false" || value == "no" {
					cfg.PII.Enabled = false
				}
			case "redact_phone":
				if value == "false" || value == "no" {
					cfg.PII.RedactPhone = false
				}
			case "redact_email":
				if value == "false" || value == "no" {
					cfg.PII.RedactEmail = false
				}
			case "auto_approve_safe":
				if value == "false" || value == "no" {
					cfg.Commands.AutoApproveSafe = false
				}
			case "auto_reject_dangerous":
				if value == "false" || value == "no" {
					cfg.Commands.AutoRejectDangerous = false
				}
			case "max_requests_per_minute":
				fmt.Sscanf(value, "%d", &cfg.RateLimit.MaxRequestsPerMinute)
			case "max_token_budget":
				fmt.Sscanf(value, "%d", &cfg.RateLimit.MaxTokenBudget)
			}
		}
	}

	return cfg, nil
}

// JSON parser for security config
func parseSecurityConfigJSON(data string) (*SecurityConfig, error) {
	// Simple JSON parsing using basic string manipulation
	// For a full implementation, use encoding/json with proper struct tags
	cfg := &SecurityConfig{
		PII: PIIConfig{
			Enabled:        true,
			RedactPhone:    true,
			RedactEmail:    true,
			RedactIDCard:   true,
			RedactBankCard: true,
			RedactIP:       true,
			RedactAddress:  true,
			CustomPatterns: make(map[string]string),
		},
		Commands: CommandSecurityConfig{
			AutoApproveSafe:       true,
			AutoRejectDangerous:   true,
			RequireApprovalLevels: []string{"dangerous"},
		},
		Hooks: HooksConfig{
			Enabled: []string{"privacy"},
		},
		RateLimit: RateLimitConfig{
			Enabled:              true,
			MaxRequestsPerMinute: 60,
			MaxTokenBudget:       100000,
		},
	}

	// Use encoding/json for proper parsing
	var jsonCfg struct {
		PII struct {
			Enabled        bool              `json:"enabled"`
			RedactPhone    bool              `json:"redact_phone"`
			RedactEmail    bool              `json:"redact_email"`
			RedactIDCard   bool              `json:"redact_id_card"`
			RedactBankCard bool              `json:"redact_bank_card"`
			RedactIP       bool              `json:"redact_ip"`
			RedactAddress  bool              `json:"redact_address"`
			CustomPatterns map[string]string `json:"custom_patterns"`
		} `json:"pii"`
		Commands struct {
			AutoApproveSafe       bool     `json:"auto_approve_safe"`
			AutoRejectDangerous   bool     `json:"auto_reject_dangerous"`
			RequireApprovalLevels []string `json:"require_approval_levels"`
			AllowedCommands       []string `json:"allowed_commands"`
			BlockedCommands       []string `json:"blocked_commands"`
		} `json:"commands"`
		Hooks struct {
			Enabled []string `json:"enabled"`
		} `json:"hooks"`
		RateLimit struct {
			Enabled              bool  `json:"enabled"`
			MaxRequestsPerMinute int   `json:"max_requests_per_minute"`
			MaxTokenBudget       int64 `json:"max_token_budget"`
		} `json:"rate_limit"`
	}

	if err := json.Unmarshal([]byte(data), &jsonCfg); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Apply parsed values
	if jsonCfg.PII.Enabled {
		cfg.PII.Enabled = jsonCfg.PII.Enabled
	}
	cfg.PII.RedactPhone = jsonCfg.PII.RedactPhone
	cfg.PII.RedactEmail = jsonCfg.PII.RedactEmail
	cfg.PII.RedactIDCard = jsonCfg.PII.RedactIDCard
	cfg.PII.RedactBankCard = jsonCfg.PII.RedactBankCard
	cfg.PII.RedactIP = jsonCfg.PII.RedactIP
	cfg.PII.RedactAddress = jsonCfg.PII.RedactAddress
	if jsonCfg.PII.CustomPatterns != nil {
		cfg.PII.CustomPatterns = jsonCfg.PII.CustomPatterns
	}

	cfg.Commands.AutoApproveSafe = jsonCfg.Commands.AutoApproveSafe
	cfg.Commands.AutoRejectDangerous = jsonCfg.Commands.AutoRejectDangerous
	if len(jsonCfg.Commands.RequireApprovalLevels) > 0 {
		cfg.Commands.RequireApprovalLevels = jsonCfg.Commands.RequireApprovalLevels
	}
	if len(jsonCfg.Commands.AllowedCommands) > 0 {
		cfg.Commands.AllowedCommands = jsonCfg.Commands.AllowedCommands
	}
	if len(jsonCfg.Commands.BlockedCommands) > 0 {
		cfg.Commands.BlockedCommands = jsonCfg.Commands.BlockedCommands
	}

	if len(jsonCfg.Hooks.Enabled) > 0 {
		cfg.Hooks.Enabled = jsonCfg.Hooks.Enabled
	}

	cfg.RateLimit.Enabled = jsonCfg.RateLimit.Enabled
	if jsonCfg.RateLimit.MaxRequestsPerMinute > 0 {
		cfg.RateLimit.MaxRequestsPerMinute = jsonCfg.RateLimit.MaxRequestsPerMinute
	}
	if jsonCfg.RateLimit.MaxTokenBudget > 0 {
		cfg.RateLimit.MaxTokenBudget = jsonCfg.RateLimit.MaxTokenBudget
	}

	return cfg, nil
}

// LoadSecurityConfigFromDir loads security config from a directory
func LoadSecurityConfigFromDir(dir string) (*SecurityConfig, error) {
	// Look for .security.yml in the given directory
	path := filepath.Join(dir, ".security.yml")
	return LoadSecurityConfig(path)
}

// SaveSecurityConfig saves security configuration to file
func SaveSecurityConfig(path string, config *SecurityConfig) error {
	// Generate simple YAML output
	var sb strings.Builder
	sb.WriteString("# Security Configuration for go-magic\n\n")

	sb.WriteString("# PII handling\npii:\n")
	sb.WriteString(fmt.Sprintf("  enabled: %v\n", config.PII.Enabled))
	sb.WriteString(fmt.Sprintf("  redact_phone: %v\n", config.PII.RedactPhone))
	sb.WriteString(fmt.Sprintf("  redact_email: %v\n", config.PII.RedactEmail))
	sb.WriteString(fmt.Sprintf("  redact_id_card: %v\n", config.PII.RedactIDCard))
	sb.WriteString(fmt.Sprintf("  redact_bank_card: %v\n", config.PII.RedactBankCard))
	sb.WriteString(fmt.Sprintf("  redact_ip: %v\n", config.PII.RedactIP))
	sb.WriteString(fmt.Sprintf("  redact_address: %v\n\n", config.PII.RedactAddress))

	sb.WriteString("# Commands\ncommands:\n")
	sb.WriteString(fmt.Sprintf("  auto_approve_safe: %v\n", config.Commands.AutoApproveSafe))
	sb.WriteString(fmt.Sprintf("  auto_reject_dangerous: %v\n\n", config.Commands.AutoRejectDangerous))

	sb.WriteString("# Rate limit\nrate_limit:\n")
	sb.WriteString(fmt.Sprintf("  enabled: %v\n", config.RateLimit.Enabled))
	sb.WriteString(fmt.Sprintf("  max_requests_per_minute: %d\n", config.RateLimit.MaxRequestsPerMinute))
	sb.WriteString(fmt.Sprintf("  max_token_budget: %d\n", config.RateLimit.MaxTokenBudget))

	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write security config: %w", err)
	}

	return nil
}

// CreateDefaultSecurityConfig creates a default security config file
func CreateDefaultSecurityConfig(dir string) error {
	path := filepath.Join(dir, ".security.yml")
	config := DefaultSecurityConfig()
	return SaveSecurityConfig(path, config)
}

// Validate validates the security configuration
func (c *SecurityConfig) Validate() error {
	// Validate rate limit
	if c.RateLimit.MaxRequestsPerMinute < 0 {
		return fmt.Errorf("max_requests_per_minute must be non-negative")
	}

	// Validate hook names
	for _, name := range c.Hooks.Enabled {
		if !isValidHookName(name) {
			return fmt.Errorf("invalid hook name: %s", name)
		}
	}

	// Validate process hooks
	for _, hook := range c.Hooks.Process {
		if hook.Name == "" {
			return fmt.Errorf("process hook name is required")
		}
		if len(hook.Command) == 0 {
			return fmt.Errorf("process hook command is required for hook %s", hook.Name)
		}
	}

	return nil
}

func isValidHookName(name string) bool {
	validHooks := map[string]bool{
		"privacy":        true,
		"message_filter": true,
		"rate_limit":     true,
		"approval":       true,
	}
	return validHooks[name]
}

// GetEnabledHooks returns the list of enabled built-in hooks
func (c *SecurityConfig) GetEnabledHooks() []string {
	hooks := make([]string, 0)
	for _, name := range c.Hooks.Enabled {
		if isValidHookName(name) {
			hooks = append(hooks, name)
		}
	}
	return hooks
}

// ShouldAutoApprove checks if a command should be auto-approved
func (c *SecurityConfig) ShouldAutoApprove(command string) bool {
	// Check blocked commands first
	for _, pattern := range c.Commands.BlockedCommands {
		if matchesPattern(command, pattern) {
			return false
		}
	}

	// Check allowed commands
	for _, pattern := range c.Commands.AllowedCommands {
		if matchesPattern(command, pattern) {
			return true
		}
	}

	return false
}

// ShouldRequireApproval checks if a command requires approval
func (c *SecurityConfig) ShouldRequireApproval(riskLevel string) bool {
	for _, level := range c.Commands.RequireApprovalLevels {
		if strings.EqualFold(level, riskLevel) {
			return true
		}
	}
	return false
}

func matchesPattern(command, pattern string) bool {
	// Simple wildcard matching
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		for _, part := range parts {
			if part != "" && !strings.Contains(command, part) {
				return false
			}
		}
		return true
	}
	return strings.Contains(command, pattern)
}
