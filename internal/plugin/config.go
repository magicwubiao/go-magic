package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// ConfigManager manages plugin configurations
type ConfigManager struct {
	mu        sync.RWMutex
	configs   map[string]map[string]interface{} // pluginID -> config
	defaults  map[string]map[string]interface{} // pluginID -> defaults
	schema    map[string][]ConfigField         // pluginID -> schema
	configDir string
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(configDir string) (*ConfigManager, error) {
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".magic", "plugins", "config")
	}

	os.MkdirAll(configDir, 0755)

	cm := &ConfigManager{
		configs:  make(map[string]map[string]interface{}),
		defaults: make(map[string]map[string]interface{}),
		schema:  make(map[string][]ConfigField),
		configDir: configDir,
	}

	// Load existing configs
	if err := cm.loadAll(); err != nil {
		fmt.Printf("Warning: failed to load configs: %v\n", err)
	}

	return cm, nil
}

// RegisterSchema registers a configuration schema for a plugin
func (cm *ConfigManager) RegisterSchema(pluginID string, fields []ConfigField) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.schema[pluginID] = fields

	// Initialize defaults
	if cm.defaults[pluginID] == nil {
		cm.defaults[pluginID] = make(map[string]interface{})
	}

	for _, field := range fields {
		if _, exists := cm.defaults[pluginID][field.Key]; !exists {
			cm.defaults[pluginID][field.Key] = field.Default
		}
	}

	// Set config from defaults if not exists
	if cm.configs[pluginID] == nil {
		cm.configs[pluginID] = make(map[string]interface{})
		for k, v := range cm.defaults[pluginID] {
			if _, exists := cm.configs[pluginID][k]; !exists {
				cm.configs[pluginID][k] = v
			}
		}
	}
}

// SetConfig sets configuration for a plugin
func (cm *ConfigManager) SetConfig(pluginID string, config map[string]interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Validate if schema exists
	if fields, exists := cm.schema[pluginID]; exists {
		if err := cm.validateConfig(pluginID, config, fields); err != nil {
			return err
		}
	}

	// Merge with defaults
	if cm.defaults[pluginID] != nil {
		merged := make(map[string]interface{})
		for k, v := range cm.defaults[pluginID] {
			merged[k] = v
		}
		for k, v := range config {
			merged[k] = v
		}
		cm.configs[pluginID] = merged
	} else {
		cm.configs[pluginID] = config
	}

	// Save to disk
	return cm.saveConfig(pluginID)
}

// GetConfig returns configuration for a plugin
func (cm *ConfigManager) GetConfig(pluginID string) map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	config := make(map[string]interface{})

	// Start with defaults
	if defaults, exists := cm.defaults[pluginID]; exists {
		for k, v := range defaults {
			config[k] = v
		}
	}

	// Override with actual config
	if userConfig, exists := cm.configs[pluginID]; exists {
		for k, v := range userConfig {
			config[k] = v
		}
	}

	return config
}

// GetRawConfig returns the raw (user-set) configuration without defaults
func (cm *ConfigManager) GetRawConfig(pluginID string) map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if config, exists := cm.configs[pluginID]; exists {
		result := make(map[string]interface{})
		for k, v := range config {
			result[k] = v
		}
		return result
	}
	return nil
}

// SetDefault sets a default value for a configuration key
func (cm *ConfigManager) SetDefault(pluginID, key string, value interface{}) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.defaults[pluginID] == nil {
		cm.defaults[pluginID] = make(map[string]interface{})
	}
	cm.defaults[pluginID][key] = value

	// Apply to config if not set
	if cm.configs[pluginID] == nil {
		cm.configs[pluginID] = make(map[string]interface{})
	}
	if _, exists := cm.configs[pluginID][key]; !exists {
		cm.configs[pluginID][key] = value
	}
}

// GetDefault returns the default value for a key
func (cm *ConfigManager) GetDefault(pluginID, key string) (interface{}, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if defaults, exists := cm.defaults[pluginID]; exists {
		if v, ok := defaults[key]; ok {
			return v, true
		}
	}
	return nil, false
}

// DeleteConfig removes a configuration key
func (cm *ConfigManager) DeleteConfig(pluginID, key string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if config, exists := cm.configs[pluginID]; exists {
		delete(config, key)
		return cm.saveConfig(pluginID)
	}

	return nil
}

// ResetConfig resets plugin config to defaults
func (cm *ConfigManager) ResetConfig(pluginID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.configs, pluginID)
	return cm.saveConfig(pluginID)
}

// ListConfigKeys lists all configuration keys for a plugin
func (cm *ConfigManager) ListConfigKeys(pluginID string) []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	config := cm.GetConfig(pluginID)
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	return keys
}

// GetSchema returns the configuration schema for a plugin
func (cm *ConfigManager) GetSchema(pluginID string) []ConfigField {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.schema[pluginID]
}

// HasSchema returns true if plugin has a registered schema
func (cm *ConfigManager) HasSchema(pluginID string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	_, exists := cm.schema[pluginID]
	return exists
}

// ExportConfig exports configuration for all plugins
func (cm *ConfigManager) ExportConfig() ([]byte, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	export := make(map[string]map[string]interface{})
	for pluginID, config := range cm.configs {
		export[pluginID] = config
	}

	return json.MarshalIndent(export, "", "  ")
}

// ImportConfig imports configuration from JSON
func (cm *ConfigManager) ImportConfig(data []byte) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var configs map[string]map[string]interface{}
	if err := json.Unmarshal(data, &configs); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	for pluginID, config := range configs {
		// Validate if schema exists
		if fields, exists := cm.schema[pluginID]; exists {
			if err := cm.validateConfig(pluginID, config, fields); err != nil {
				return fmt.Errorf("invalid config for %s: %w", pluginID, err)
			}
		}

		cm.configs[pluginID] = config
		if err := cm.saveConfig(pluginID); err != nil {
			fmt.Printf("Warning: failed to save config for %s: %v\n", pluginID, err)
		}
	}

	return nil
}

// loadAll loads all configuration files
func (cm *ConfigManager) loadAll() error {
	entries, err := os.ReadDir(cm.configDir)
	if err != nil {
		return fmt.Errorf("failed to read config directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		pluginID := strings.TrimSuffix(entry.Name(), ".json")
		configPath := filepath.Join(cm.configDir, entry.Name())

		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		var config map[string]interface{}
		if err := json.Unmarshal(data, &config); err != nil {
			continue
		}

		cm.configs[pluginID] = config
	}

	return nil
}

// saveConfig saves configuration to disk
func (cm *ConfigManager) saveConfig(pluginID string) error {
	configPath := filepath.Join(cm.configDir, pluginID+".json")

	config := cm.configs[pluginID]
	if config == nil {
		// Remove config file if config is empty
		os.Remove(configPath)
		return nil
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(configPath, data, 0644)
}

// validateConfig validates configuration against schema
func (cm *ConfigManager) validateConfig(pluginID string, config map[string]interface{}, fields []ConfigField) error {
	for _, field := range fields {
		value, exists := config[field.Key]

		// Check required
		if field.Required && !exists {
			return fmt.Errorf("required field missing: %s", field.Key)
		}

		if !exists {
			continue
		}

		// Check type
		if err := validateType(field.Key, value, field.Type); err != nil {
			return err
		}

		// Check options (enum)
		if len(field.Options) > 0 {
			if strVal, ok := value.(string); ok {
				valid := false
				for _, opt := range field.Options {
					if strVal == opt {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("invalid value for %s: must be one of %v", field.Key, field.Options)
				}
			}
		}

		// Check pattern
		if field.Pattern != "" && field.Type == "string" {
			if strVal, ok := value.(string); ok {
				re, err := regexp.Compile(field.Pattern)
				if err != nil {
					return fmt.Errorf("invalid pattern for %s: %w", field.Key, err)
				}
				if !re.MatchString(strVal) {
					return fmt.Errorf("value for %s does not match pattern: %s", field.Key, field.Pattern)
				}
			}
		}

		// Check min/max for numbers
		if field.Type == "number" || field.Type == "integer" {
			var num float64
			switch v := value.(type) {
			case float64:
				num = v
			case int:
				num = float64(v)
			case int64:
				num = float64(v)
			default:
				return fmt.Errorf("invalid number type for %s", field.Key)
			}

			if field.Min != nil && num < *field.Min {
				return fmt.Errorf("value for %s is below minimum: %.2f", field.Key, *field.Min)
			}
			if field.Max != nil && num > *field.Max {
				return fmt.Errorf("value for %s is above maximum: %.2f", field.Key, *field.Max)
			}
		}
	}

	return nil
}

// validateType validates a value against an expected type
func validateType(key string, value interface{}, expectedType string) error {
	if value == nil {
		return nil
	}

	switch expectedType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", key)
		}
	case "number":
		switch value.(type) {
		case float64, int, int64:
			// OK
		default:
			return fmt.Errorf("%s must be a number", key)
		}
	case "integer":
		switch value.(type) {
		case int, int64:
			// OK
		case float64:
			// Allow float that is whole number
			if v, ok := value.(float64); ok && v != float64(int64(v)) {
				return fmt.Errorf("%s must be an integer", key)
			}
		default:
			return fmt.Errorf("%s must be an integer", key)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", key)
		}
	case "array":
		if _, ok := value.([]interface{}); !ok {
			return fmt.Errorf("%s must be an array", key)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("%s must be an object", key)
		}
	}

	return nil
}

// MergeConfig merges two configurations, with override taking precedence
func MergeConfig(base, override map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// Start with base
	for k, v := range base {
		result[k] = v
	}

	// Override with override
	for k, v := range override {
		result[k] = v
	}

	return result
}
