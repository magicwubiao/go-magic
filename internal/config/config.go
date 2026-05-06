package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config represents the application configuration
type Config struct {
	// Cortex core settings
	Cortex CortexConfig `json:"cortex"`

	// Plugin settings
	Plugin PluginConfig `json:"plugin"`

	// Execution settings
	Execution ExecutionConfig `json:"execution"`

	// Memory settings
	Memory MemoryConfig `json:"memory"`

	// Logging settings
	Log LogConfig `json:"log"`

	// Server settings
	Server ServerConfig `json:"server"`
}

// CortexConfig is the new name for the core cognitive engine configuration.
// This provides an alternative name for the CortexConfig, maintaining
// full backward compatibility while allowing a cleaner naming convention.

// CortexConfig contains Cortex agent configuration
type CortexConfig struct {
	// Memory settings
	MemoryLimit int `json:"memory_limit_mb"` // Memory limit in MB

	// Trigger settings
	NudgeInterval time.Duration `json:"nudge_interval"` // Interval between nudges
	NudgeEnabled bool `json:"nudge_enabled"` // Enable/disable nudges

	// Review settings
	ReviewInterval time.Duration `json:"review_interval"` // Background review interval
	ReviewEnabled  bool `json:"review_enabled"` // Enable/disable reviews

	// FTS settings
	EnableFTS bool `json:"enable_fts"` // Enable full-text search
	FTSCache  bool `json:"fts_cache"` // Enable FTS caching

	// Perception settings
	PerceptionConfidenceThreshold float64 `json:"perception_confidence_threshold"`
	PerceptionMaxHistory         int     `json:"perception_max_history"`

	// Cognition settings
	PlanningMaxSteps int `json:"planning_max_steps"`
	PlanningTimeout  time.Duration `json:"planning_timeout"`

	// Skills settings
	AutoSkillCreation bool `json:"auto_skill_creation"`
	MinPatternFreq    int  `json:"min_pattern_frequency"`
}

// PluginConfig contains plugin system configuration
type PluginConfig struct {
	// Plugin loading
	AutoInstall bool `json:"auto_install"` // Auto-install missing plugins
	AutoUpdate  bool `json:"auto_update"`  // Auto-update plugins
	AllowedDirs []string `json:"allowed_dirs"` // Allowed plugin directories

	// Security
	SandboxEnabled bool `json:"sandbox_enabled"` // Enable plugin sandboxing
	SandboxTimeout time.Duration `json:"sandbox_timeout"` // Sandbox timeout

	// Cache
	CacheEnabled bool `json:"cache_enabled"` // Enable plugin cache
	CacheDir     string `json:"cache_dir"`    // Cache directory
}

// ExecutionConfig contains execution layer configuration
type ExecutionConfig struct {
	// Iteration limits
	MaxIterations int `json:"max_iterations"` // Maximum iterations per task
	MaxDepth      int `json:"max_depth"`      // Maximum recursion depth

	// Checkpoint settings
	CheckpointsEnabled bool `json:"checkpoints_enabled"`
	CheckpointFreq     int  `json:"checkpoint_frequency"` // Create checkpoint every N iterations
	CheckpointDir      string `json:"checkpoint_dir"`
	CheckpointTTL      time.Duration `json:"checkpoint_ttl"` // How long to keep checkpoints

	// Recovery settings
	AutoResume        bool `json:"auto_resume"` // Auto-resume from checkpoint on failure
	MaxRecoveryAttempts int `json:"max_recovery_attempts"`

	// Validation settings
	ValidationLevel string `json:"validation_level"` // "strict", "normal", "relaxed"
	FailOnWarning   bool   `json:"fail_on_warning"`

	// Timeout settings
	DefaultTimeout   time.Duration `json:"default_timeout"`
	LongRunningLimit time.Duration `json:"long_running_limit"`
}

// MemoryConfig contains memory system configuration
type MemoryConfig struct {
	// Storage settings
	StorageDir string `json:"storage_dir"` // Base storage directory
	MaxSizeMB  int64  `json:"max_size_mb"` // Maximum memory store size

	// FTS settings
	FTSEnabled     bool `json:"fts_enabled"`
	FTSMaxResults  int  `json:"fts_max_results"`
	FTSBoostRecent bool `json:"fts_boost_recent"` // Boost recent results

	// Importance decay
	EnableDecay bool    `json:"enable_decay"`
	DecayRate   float64 `json:"decay_rate"` // Daily decay rate (0-1)

	// Deduplication
	EnableDedup bool `json:"enable_dedup"` // Enable content deduplication

	// Cleanup settings
	CleanupEnabled bool          `json:"cleanup_enabled"`
	CleanupInterval time.Duration `json:"cleanup_interval"`
	CleanupOlderThan time.Duration `json:"cleanup_older_than"`
}

// LogConfig contains logging configuration
type LogConfig struct {
	// Output settings
	Level  string `json:"level"`  // debug, info, warn, error
	Format string `json:"format"` // text, json

	// File settings
	FileEnabled bool   `json:"file_enabled"`
	FilePath    string `json:"file_path"`
	MaxSizeMB   int    `json:"max_size_mb"`
	MaxBackups  int    `json:"max_backups"`
	MaxAgeDays  int    `json:"max_age_days"`

	// Output
	StdoutEnabled bool `json:"stdout_enabled"`
	StderrEnabled bool `json:"stderr_enabled"`
}

// ServerConfig contains HTTP server configuration
type ServerConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`

	// TLS settings
	TLSEnabled bool   `json:"tls_enabled"`
	TLSCert    string `json:"tls_cert"`
	TLSKey     string `json:"tls_key"`

	// Timeouts
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout"`

	// Limits
	MaxRequestSize    int64 `json:"max_request_size"`
	MaxHeaderBytes    int   `json:"max_header_bytes"`
	ReadHeaderTimeout time.Duration `json:"read_header_timeout"`
}

// ConfigManager manages application configuration
type ConfigManager struct {
	config    *Config
	configDir string
	filePath  string

	watchers    []chan *Config
	watcherMu   sync.RWMutex

	mu sync.RWMutex
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	// Create default core config (shared between all agents)
	defaultCore := CortexConfig{
		MemoryLimit:                 1024,
		NudgeInterval:               15 * time.Minute,
		NudgeEnabled:                true,
		ReviewInterval:              30 * time.Minute,
		ReviewEnabled:               true,
		EnableFTS:                   true,
		FTSCache:                   true,
		PerceptionConfidenceThreshold: 0.7,
		PerceptionMaxHistory:        100,
		PlanningMaxSteps:           50,
		PlanningTimeout:             30 * time.Second,
		AutoSkillCreation:           true,
		MinPatternFreq:              2,
	}

	return &Config{
		Cortex: defaultCore,
		Plugin: PluginConfig{
			AutoInstall:   false,
			AutoUpdate:    false,
			AllowedDirs:   []string{"./plugins"},
			SandboxEnabled: true,
			SandboxTimeout: 30 * time.Second,
			CacheEnabled:  true,
			CacheDir:      "./.cache/plugins",
		},
		Execution: ExecutionConfig{
			MaxIterations:       100,
			MaxDepth:            10,
			CheckpointsEnabled:  true,
			CheckpointFreq:      10,
			CheckpointDir:       "./.checkpoints",
			CheckpointTTL:       24 * time.Hour,
			AutoResume:          true,
			MaxRecoveryAttempts: 3,
			ValidationLevel:     "normal",
			FailOnWarning:       false,
			DefaultTimeout:      5 * time.Minute,
			LongRunningLimit:    30 * time.Minute,
		},
		Memory: MemoryConfig{
			StorageDir:       "./memory",
			MaxSizeMB:        1024,
			FTSEnabled:       true,
			FTSMaxResults:    20,
			FTSBoostRecent:   true,
			EnableDecay:      true,
			DecayRate:        0.05,
			EnableDedup:      true,
			CleanupEnabled:   true,
			CleanupInterval:  1 * time.Hour,
			CleanupOlderThan: 90 * 24 * time.Hour,
		},
		Log: LogConfig{
			Level:       "info",
			Format:      "json",
			FileEnabled: true,
			FilePath:    "./logs/app.log",
			MaxSizeMB:   100,
			MaxBackups:  5,
			MaxAgeDays:  30,
			StdoutEnabled: true,
			StderrEnabled: true,
		},
		Server: ServerConfig{
			Host:                "0.0.0.0",
			Port:                8080,
			TLSEnabled:         false,
			ReadTimeout:         30 * time.Second,
			WriteTimeout:        30 * time.Second,
			IdleTimeout:         120 * time.Second,
			MaxRequestSize:      10 * 1024 * 1024,
			MaxHeaderBytes:      4096,
			ReadHeaderTimeout:   5 * time.Second,
		},
	}
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(configDir string) (*ConfigManager, error) {
	cm := &ConfigManager{
		config:    DefaultConfig(),
		configDir: configDir,
		filePath:  filepath.Join(configDir, "config.json"),
		watchers:  make([]chan *Config, 0),
	}

	// Load existing config
	if err := cm.Load(); err != nil {
		// If no config exists, save default
		if os.IsNotExist(err) {
			if err := cm.Save(); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return cm, nil
}

// Load loads configuration from file
func (cm *ConfigManager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		return err
	}

	// Parse config
	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return err
	}

	// Apply environment variable overrides
	cm.applyEnvOverrides(config)

	cm.config = config
	return nil
}

// Save saves configuration to file
func (cm *ConfigManager) Save() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	os.MkdirAll(cm.configDir, 0755)

	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cm.filePath, data, 0644)
}

// Get returns the current configuration
func (cm *ConfigManager) Get() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return a copy to prevent external modification
	config := *cm.config
	return &config
}

// Update updates configuration with a partial config
func (cm *ConfigManager) Update(updates map[string]interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Convert updates to JSON and merge
	data, err := json.Marshal(updates)
	if err != nil {
		return err
	}

	// Create a temporary config to unmarshal updates
	var partial Config
	if err := json.Unmarshal(data, &partial); err != nil {
		return err
	}

	// Merge with existing config
	cm.mergeConfig(&partial)

	// Apply environment variable overrides
	cm.applyEnvOverrides(cm.config)

	// Save to file
	if err := cm.saveLocked(); err != nil {
		return err
	}

	// Notify watchers
	go cm.notifyWatchers()

	return nil
}

// mergeConfig merges partial config into the current config
func (cm *ConfigManager) mergeConfig(partial *Config) {
	// Use reflection to merge
	cm.mergeStruct(reflect.ValueOf(cm.config), reflect.ValueOf(partial))
}

// mergeStruct recursively merges struct values
func (cm *ConfigManager) mergeStruct(target, source reflect.Value) {
	for i := 0; i < target.Elem().NumField(); i++ {
		targetField := target.Elem().Field(i)
		sourceField := source.Elem().Field(i)

		// Only process if source is not zero value
		if sourceField.IsZero() {
			continue
		}

		switch targetField.Kind() {
		case reflect.Struct:
			cm.mergeStruct(targetField.Addr(), sourceField.Addr())
		case reflect.Slice:
			if sourceField.Len() > 0 {
				targetField.Set(sourceField)
			}
		case reflect.Map:
			if sourceField.Len() > 0 {
				targetField.Set(sourceField)
			}
		default:
			targetField.Set(sourceField)
		}
	}
}

// applyEnvOverrides applies environment variable overrides
func (cm *ConfigManager) applyEnvOverrides(config *Config) {
	// Cortex overrides
	if v := os.Getenv("CORTEX_MEMORY_LIMIT"); v != "" {
		if val, err := strconv.Atoi(v); err == nil {
			config.Cortex.MemoryLimit = val
		}
	}
	if v := os.Getenv("CORTEX_NUDGE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Cortex.NudgeInterval = d
		}
	}

	// Execution overrides
	if v := os.Getenv("EXECUTION_MAX_ITERATIONS"); v != "" {
		if val, err := strconv.Atoi(v); err == nil {
			config.Execution.MaxIterations = val
		}
	}
	if v := os.Getenv("EXECUTION_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.Execution.DefaultTimeout = d
		}
	}

	// Log overrides
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		config.Log.Level = v
	}

	// Server overrides
	if v := os.Getenv("SERVER_PORT"); v != "" {
		if val, err := strconv.Atoi(v); err == nil {
			config.Server.Port = val
		}
	}
}

// saveLocked saves configuration (must be called with lock held)
func (cm *ConfigManager) saveLocked() error {
	os.MkdirAll(cm.configDir, 0755)

	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cm.filePath, data, 0644)
}

// notifyWatchers notifies all config watchers
func (cm *ConfigManager) notifyWatchers() {
	cm.watcherMu.RLock()
	watchers := make([]chan *Config, len(cm.watchers))
	copy(watchers, cm.watchers)
	cm.watcherMu.RUnlock()

	config := cm.Get()
	for _, ch := range watchers {
		select {
		case ch <- config:
		default:
			// Channel full, skip
		}
	}
}

// Watch returns a channel that receives config updates
func (cm *ConfigManager) Watch() chan *Config {
	cm.watcherMu.Lock()
	defer cm.watcherMu.Unlock()

	ch := make(chan *Config, 1)
	cm.watchers = append(cm.watchers, ch)
	return ch
}

// Unwatch stops watching for config updates
func (cm *ConfigManager) Unwatch(ch chan *Config) {
	cm.watcherMu.Lock()
	defer cm.watcherMu.Unlock()

	for i, watcher := range cm.watchers {
		if watcher == ch {
			cm.watchers = append(cm.watchers[:i], cm.watchers[i+1:]...)
			close(ch)
			break
		}
	}
}

// GetCortex returns Cortex (cognitive engine) configuration
func (cm *ConfigManager) GetCortex() CortexConfig {
	return cm.Get().Cortex
}

// GetPlugin returns plugin configuration
func (cm *ConfigManager) GetPlugin() PluginConfig {
	return cm.Get().Plugin
}

// GetExecution returns execution configuration
func (cm *ConfigManager) GetExecution() ExecutionConfig {
	return cm.Get().Execution
}

// GetMemory returns memory configuration
func (cm *ConfigManager) GetMemory() MemoryConfig {
	return cm.Get().Memory
}

// GetLog returns log configuration
func (cm *ConfigManager) GetLog() LogConfig {
	return cm.Get().Log
}

// GetServer returns server configuration
func (cm *ConfigManager) GetServer() ServerConfig {
	return cm.Get().Server
}

// Validate validates the configuration
func (cm *ConfigManager) Validate() error {
	config := cm.Get()

	var errors []string

	// Validate Cortex config
	if config.Cortex.MemoryLimit <= 0 {
		errors = append(errors, "cortex.memory_limit must be positive")
	}
	if config.Cortex.NudgeInterval <= 0 {
		errors = append(errors, "cortex.nudge_interval must be positive")
	}
	if config.Cortex.PerceptionConfidenceThreshold < 0 || config.Cortex.PerceptionConfidenceThreshold > 1 {
		errors = append(errors, "cortex.perception_confidence_threshold must be between 0 and 1")
	}

	// Validate Execution config
	if config.Execution.MaxIterations <= 0 {
		errors = append(errors, "execution.max_iterations must be positive")
	}
	if config.Execution.MaxDepth <= 0 {
		errors = append(errors, "execution.max_depth must be positive")
	}
	if config.Execution.CheckpointFreq <= 0 {
		errors = append(errors, "execution.checkpoint_frequency must be positive")
	}

	// Validate Memory config
	if config.Memory.MaxSizeMB <= 0 {
		errors = append(errors, "memory.max_size_mb must be positive")
	}

	// Validate Log config
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[config.Log.Level] {
		errors = append(errors, fmt.Sprintf("log.level must be one of: %s", strings.Join(allowedKeys(validLevels), ", ")))
	}

	validFormats := map[string]bool{"text": true, "json": true}
	if !validFormats[config.Log.Format] {
		errors = append(errors, fmt.Sprintf("log.format must be one of: %s", strings.Join(allowedKeys(validFormats), ", ")))
	}

	// Validate Server config
	if config.Server.Port <= 0 || config.Server.Port > 65535 {
		errors = append(errors, "server.port must be between 1 and 65535")
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed:\n%s", strings.Join(errors, "\n"))
	}

	return nil
}

// allowedKeys returns the keys of a map as a slice
func allowedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ExportJSON exports the configuration as JSON
func (cm *ConfigManager) ExportJSON() ([]byte, error) {
	return json.MarshalIndent(cm.Get(), "", "  ")
}

// Reload reloads configuration from file
func (cm *ConfigManager) Reload() error {
	return cm.Load()
}
