package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewConfigManager(t *testing.T) {
	configDir := filepath.Join(os.TempDir(), "config-test")
	os.MkdirAll(configDir, 0755)
	defer os.RemoveAll(configDir)

	cm, err := NewConfigManager(configDir)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}
	if cm == nil {
		t.Fatal("expected non-nil config manager")
	}
}

func TestRegisterSchema(t *testing.T) {
	cm, _ := NewConfigManager("")

	schema := []ConfigField{
		{
			Key:         "debug",
			Type:        "boolean",
			Default:     false,
			Description: "Enable debug mode",
			Required:    true,
		},
		{
			Key:         "port",
			Type:        "integer",
			Default:     8080,
			Description: "Server port",
		},
	}

	cm.RegisterSchema("test-plugin", schema)

	// Check that defaults are set
	config := cm.GetConfig("test-plugin")
	if config["debug"] != false {
		t.Errorf("expected debug=false, got %v", config["debug"])
	}
	if config["port"] != 8080 {
		t.Errorf("expected port=8080, got %v", config["port"])
	}
}

func TestSetGetConfig(t *testing.T) {
	cm, _ := NewConfigManager("")

	// Set config
	config := map[string]interface{}{
		"debug": true,
		"port":  9000,
	}

	err := cm.SetConfig("test-plugin", config)
	if err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	// Get config
	retrieved := cm.GetConfig("test-plugin")
	if retrieved["debug"] != true {
		t.Errorf("expected debug=true, got %v", retrieved["debug"])
	}
	if retrieved["port"] != 9000 {
		t.Errorf("expected port=9000, got %v", retrieved["port"])
	}
}

func TestSetDefault(t *testing.T) {
	cm, _ := NewConfigManager("")

	cm.SetDefault("test-plugin", "timeout", 30)
	cm.SetDefault("test-plugin", "retries", 3)

	timeout, ok := cm.GetDefault("test-plugin", "timeout")
	if !ok {
		t.Error("expected timeout default to exist")
	}
	if timeout != 30 {
		t.Errorf("expected timeout=30, got %v", timeout)
	}

	retries, ok := cm.GetDefault("test-plugin", "retries")
	if !ok {
		t.Error("expected retries default to exist")
	}
	if retries != 3 {
		t.Errorf("expected retries=3, got %v", retries)
	}
}

func TestDeleteConfig(t *testing.T) {
	cm, _ := NewConfigManager("")

	config := map[string]interface{}{
		"key1": "value1",
		"key2": "value2",
	}

	cm.SetConfig("test-plugin", config)

	err := cm.DeleteConfig("test-plugin", "key1")
	if err != nil {
		t.Fatalf("failed to delete config: %v", err)
	}

	retrieved := cm.GetConfig("test-plugin")
	if _, exists := retrieved["key1"]; exists {
		t.Error("key1 should be deleted")
	}
	if _, exists := retrieved["key2"]; !exists {
		t.Error("key2 should still exist")
	}
}

func TestResetConfig(t *testing.T) {
	cm, _ := NewConfigManager("")

	// Register schema with defaults
	schema := []ConfigField{
		{
			Key:     "debug",
			Type:    "boolean",
			Default: false,
		},
	}
	cm.RegisterSchema("test-plugin", schema)

	// Set custom config
	config := map[string]interface{}{
		"debug": true,
	}
	cm.SetConfig("test-plugin", config)

	// Reset
	err := cm.ResetConfig("test-plugin")
	if err != nil {
		t.Fatalf("failed to reset config: %v", err)
	}

	// Check that config is back to default
	retrieved := cm.GetConfig("test-plugin")
	if retrieved["debug"] != false {
		t.Errorf("expected debug=false after reset, got %v", retrieved["debug"])
	}
}

func TestValidateConfig(t *testing.T) {
	cm, _ := NewConfigManager("")

	schema := []ConfigField{
		{
			Key:      "port",
			Type:     "integer",
			Required: true,
		},
		{
			Key:     "host",
			Type:    "string",
			Pattern: "^localhost$|^[0-9.]+$",
		},
		{
			Key:     "mode",
			Type:    "string",
			Options: []string{"dev", "prod", "test"},
		},
	}

	cm.RegisterSchema("test-plugin", schema)

	tests := []struct {
		name      string
		config    map[string]interface{}
		wantError bool
	}{
		{
			name: "valid config",
			config: map[string]interface{}{
				"port": 8080,
				"host": "localhost",
				"mode": "dev",
			},
			wantError: false,
		},
		{
			name: "missing required field",
			config: map[string]interface{}{
				"host": "localhost",
				"mode": "dev",
			},
			wantError: true,
		},
		{
			name: "invalid pattern",
			config: map[string]interface{}{
				"port": 8080,
				"host": "invalid-host",
				"mode": "dev",
			},
			wantError: true,
		},
		{
			name: "invalid option",
			config: map[string]interface{}{
				"port": 8080,
				"host": "localhost",
				"mode": "invalid",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cm.SetConfig("test-plugin", tt.config)
			if (err != nil) != tt.wantError {
				t.Errorf("SetConfig() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateType(t *testing.T) {
	tests := []struct {
		typeName string
		value    interface{}
		wantErr  bool
	}{
		{"string", "hello", false},
		{"string", 123, true},
		{"number", 123.45, false},
		{"number", "not a number", true},
		{"integer", 123, false},
		{"integer", 123.45, true},
		{"boolean", true, false},
		{"boolean", "true", true},
		{"array", []interface{}{1, 2, 3}, false},
		{"array", "not an array", true},
		{"object", map[string]interface{}{}, false},
		{"object", "not an object", true},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			err := validateType("test_field", tt.value, tt.typeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeConfig(t *testing.T) {
	base := map[string]interface{}{
		"key1": "base1",
		"key2": "base2",
		"key3": "base3",
	}

	override := map[string]interface{}{
		"key1": "override1",
		"key4": "override4",
	}

	merged := MergeConfig(base, override)

	if merged["key1"] != "override1" {
		t.Errorf("expected key1=override1, got %v", merged["key1"])
	}
	if merged["key2"] != "base2" {
		t.Errorf("expected key2=base2, got %v", merged["key2"])
	}
	if merged["key4"] != "override4" {
		t.Errorf("expected key4=override4, got %v", merged["key4"])
	}
}

func TestGetSchema(t *testing.T) {
	cm, _ := NewConfigManager("")

	schema := []ConfigField{
		{Key: "field1", Type: "string"},
		{Key: "field2", Type: "integer"},
	}

	cm.RegisterSchema("test-plugin", schema)

	retrieved := cm.GetSchema("test-plugin")
	if len(retrieved) != 2 {
		t.Errorf("expected 2 fields, got %d", len(retrieved))
	}

	// Non-existent schema
	retrieved = cm.GetSchema("nonexistent")
	if len(retrieved) != 0 {
		t.Errorf("expected 0 fields for nonexistent, got %d", len(retrieved))
	}
}

func TestHasSchema(t *testing.T) {
	cm, _ := NewConfigManager("")

	schema := []ConfigField{
		{Key: "field1", Type: "string"},
	}

	cm.RegisterSchema("test-plugin", schema)

	if !cm.HasSchema("test-plugin") {
		t.Error("expected schema to exist")
	}

	if cm.HasSchema("nonexistent") {
		t.Error("expected no schema for nonexistent plugin")
	}
}

func TestExportImportConfig(t *testing.T) {
	cm, _ := NewConfigManager("")

	config := map[string]interface{}{
		"key1": "value1",
		"key2": 123,
	}

	cm.SetConfig("plugin1", config)
	cm.SetConfig("plugin2", map[string]interface{}{"key": "value"})

	// Export
	data, err := cm.ExportConfig()
	if err != nil {
		t.Fatalf("failed to export: %v", err)
	}

	// Create new manager
	cm2, _ := NewConfigManager("")

	// Import
	err = cm2.ImportConfig(data)
	if err != nil {
		t.Fatalf("failed to import: %v", err)
	}

	// Verify
	retrieved := cm2.GetConfig("plugin1")
	if retrieved["key1"] != "value1" {
		t.Errorf("expected key1=value1, got %v", retrieved["key1"])
	}
}
