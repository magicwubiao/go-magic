package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/magicwubiao/go-magic/pkg/configtypes"
)

// Loader provides unified configuration loading
// This consolidates all config loading logic into a single entry point
type Loader struct {
	configDir  string
	configFile string
}

// NewLoader creates a new configuration loader
func NewLoader() (*Loader, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(home, ".magic")
	return &Loader{
		configDir:  configDir,
		configFile: filepath.Join(configDir, "config.json"),
	}, nil
}

// NewLoaderWithPath creates a loader with custom config path
func NewLoaderWithPath(configPath string) *Loader {
	return &Loader{
		configDir:  filepath.Dir(configPath),
		configFile: configPath,
	}
}

// Load loads the configuration from the default location
// This is the unified entry point for all configuration loading
func (l *Loader) Load() (*configtypes.Config, error) {
	// Ensure config directory exists
	if err := os.MkdirAll(l.configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Check if config file exists
	if _, err := os.Stat(l.configFile); os.IsNotExist(err) {
		// Return default config if file doesn't exist
		return l.defaultConfig(), nil
	}

	// Read config file
	data, err := os.ReadFile(l.configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	// Parse config
	var cfg configtypes.Config
	if err := json.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply defaults for missing values
	l.applyDefaults(&cfg)

	// Apply environment variable overrides
	l.applyEnvOverrides(&cfg)

	return &cfg, nil
}

// LoadFromFile loads configuration from a specific file path
func (l *Loader) LoadFromFile(path string) (*configtypes.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg configtypes.Config
	if err := json.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	l.applyDefaults(&cfg)
	return &cfg, nil
}

// Save saves the configuration to the default location
func (l *Loader) Save(cfg *configtypes.Config) error {
	if err := os.MkdirAll(l.configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(l.configFile, data, 0644)
}

// defaultConfig returns the default configuration
func (l *Loader) defaultConfig() *configtypes.Config {
	return configtypes.DefaultConfig()
}

// applyDefaults applies default values for missing configuration
func (l *Loader) applyDefaults(cfg *configtypes.Config) {
	if cfg.Profile == "" {
		cfg.Profile = "default"
	}
	if cfg.MagicHome == "" {
		cfg.MagicHome = "~/.magic"
	}
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4"
	}
	// Auto-set Model from first element of Models array if Model is still empty
	if cfg.Model == "" && cfg.Providers != nil {
		if prov, ok := cfg.Providers[cfg.Provider]; ok && len(prov.Models) > 0 {
			cfg.Model = prov.Models[0].Name
		}
	}
	if cfg.Agent.MaxIterations == 0 {
		cfg.Agent.MaxIterations = 50
	}
	if cfg.Agent.MaxTurns == 0 {
		cfg.Agent.MaxTurns = 80
	}
	if cfg.Agent.CompressionRatio == 0 {
		cfg.Agent.CompressionRatio = 0.3
	}
	if cfg.Memory.RecallLimit == 0 {
		cfg.Memory.RecallLimit = 5
	}
}

// applyEnvOverrides applies environment variable overrides
func (l *Loader) applyEnvOverrides(cfg *configtypes.Config) {
	// Provider settings
	if apiKey := os.Getenv("MAGIC_API_KEY"); apiKey != "" {
		if cfg.Providers == nil {
			cfg.Providers = make(map[string]configtypes.ProviderCfg)
		}
		if prov, ok := cfg.Providers[cfg.Provider]; ok {
			prov.APIKey = apiKey
			cfg.Providers[cfg.Provider] = prov
		}
	}

	if model := os.Getenv("MAGIC_MODEL"); model != "" {
		cfg.Model = model
	}

	if provider := os.Getenv("MAGIC_PROVIDER"); provider != "" {
		cfg.Provider = provider
	}

	// Gateway settings
	if gatewayURL := os.Getenv("MAGIC_GATEWAY_URL"); gatewayURL != "" {
		cfg.Gateway.Enabled = true
	}
}

// ConfigFilePath returns the path to the config file
func (l *Loader) ConfigFilePath() string {
	return l.configFile
}

// ConfigDir returns the configuration directory
func (l *Loader) ConfigDir() string {
	return l.configDir
}

// LegacyLoad provides backward compatibility for existing code
// Deprecated: Use Loader.Load() instead
func LegacyLoad() (*Config, error) {
	loader, err := NewLoader()
	if err != nil {
		return nil, err
	}

	// Load using new loader
	newCfg, err := loader.Load()
	if err != nil {
		return nil, err
	}

	// Convert to legacy Config format
	return convertToLegacyConfig(newCfg), nil
}

// convertToLegacyConfig converts new configtypes.Config to legacy Config
// This is a temporary bridge function for backward compatibility
func convertToLegacyConfig(cfg *configtypes.Config) *Config {
	// This is a simplified conversion - full implementation would map all fields
	return &Config{
		Profile:   cfg.Profile,
		MagicHome: cfg.MagicHome,
		Provider:  cfg.Provider,
		Model:     cfg.Model,
		// Map other fields as needed
	}
}
