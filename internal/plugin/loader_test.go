package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLoader(t *testing.T) {
	registry := NewRegistry()
	config := &LoaderConfig{
		PluginDir: filepath.Join(os.TempDir(), "plugins"),
	}

	loader := NewLoader(registry, config)
	if loader == nil {
		t.Fatal("expected non-nil loader")
	}
	if loader.registry != registry {
		t.Error("expected registry to be set")
	}
}

func TestLoadFromDirectory(t *testing.T) {
	registry := NewRegistry()
	pluginDir := filepath.Join(os.TempDir(), "plugin-load-test")

	// Create test plugin
	os.MkdirAll(pluginDir, 0755)
	defer os.RemoveAll(pluginDir)

	pluginPath := filepath.Join(pluginDir, "test-plugin")
	os.MkdirAll(pluginPath, 0755)

	// Create manifest
	manifest := PluginManifest{
		ID:      "test-plugin",
		Name:    "Test Plugin",
		Version: "1.0.0",
		Type:    TypeScript,
	}
	manifestData := toJSON(manifest)
	os.WriteFile(filepath.Join(pluginPath, "manifest.json"), manifestData, 0644)

	// Create script
	os.WriteFile(filepath.Join(pluginPath, "run.sh"), []byte("#!/bin/bash\necho hello"), 0755)

	config := &LoaderConfig{
		PluginDir: pluginDir,
	}
	loader := NewLoader(registry, config)

	err := loader.LoadFromDirectory(pluginDir)
	if err != nil {
		t.Fatalf("failed to load from directory: %v", err)
	}

	if registry.Count() != 1 {
		t.Errorf("expected 1 plugin, got %d", registry.Count())
	}
}

func TestLoadNonExistent(t *testing.T) {
	registry := NewRegistry()
	loader := NewLoader(registry, nil)

	err := loader.Load("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
}

func TestValidateManifest(t *testing.T) {
	tests := []struct {
		name      string
		manifest  PluginManifest
		wantError bool
	}{
		{
			name: "valid manifest",
			manifest: PluginManifest{
				ID:      "valid-plugin",
				Name:    "Valid Plugin",
				Version: "1.0.0",
				Type:    TypeScript,
			},
			wantError: false,
		},
		{
			name: "missing ID",
			manifest: PluginManifest{
				Name:    "Test Plugin",
				Version: "1.0.0",
				Type:    TypeScript,
			},
			wantError: true,
		},
		{
			name: "missing name",
			manifest: PluginManifest{
				ID:      "test-plugin",
				Version: "1.0.0",
				Type:    TypeScript,
			},
			wantError: true,
		},
		{
			name: "missing version",
			manifest: PluginManifest{
				ID:   "test-plugin",
				Name: "Test Plugin",
				Type: TypeScript,
			},
			wantError: true,
		},
		{
			name: "missing type",
			manifest: PluginManifest{
				ID:      "test-plugin",
				Name:    "Test Plugin",
				Version: "1.0.0",
			},
			wantError: true,
		},
		{
			name: "invalid type",
			manifest: PluginManifest{
				ID:      "test-plugin",
				Name:    "Test Plugin",
				Version: "1.0.0",
				Type:    "invalid",
			},
			wantError: true,
		},
		{
			name: "invalid ID",
			manifest: PluginManifest{
				ID:      "Invalid ID!",
				Name:    "Test Plugin",
				Version: "1.0.0",
				Type:    TypeScript,
			},
			wantError: true,
		},
		{
			name: "invalid version",
			manifest: PluginManifest{
				ID:      "test-plugin",
				Name:    "Test Plugin",
				Version: "not-a-version",
				Type:    TypeScript,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateManifest(&tt.manifest)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateManifest() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestFindPlugins(t *testing.T) {
	registry := NewRegistry()
	pluginDir := filepath.Join(os.TempDir(), "plugin-find-test")

	// Create test plugins
	os.MkdirAll(pluginDir, 0755)
	defer os.RemoveAll(pluginDir)

	// Plugin 1
	p1 := filepath.Join(pluginDir, "plugin-1")
	os.MkdirAll(p1, 0755)
	os.WriteFile(filepath.Join(p1, "manifest.json"), []byte(`{"id":"plugin-1","name":"Plugin 1"}`), 0644)

	// Plugin 2
	p2 := filepath.Join(pluginDir, "plugin-2")
	os.MkdirAll(p2, 0755)
	os.WriteFile(filepath.Join(p2, "manifest.json"), []byte(`{"id":"plugin-2","name":"Plugin 2"}`), 0644)

	// Not a plugin (no manifest)
	os.MkdirAll(filepath.Join(pluginDir, "not-a-plugin"), 0755)

	config := &LoaderConfig{
		PluginDir: pluginDir,
	}
	loader := NewLoader(registry, config)

	plugins, err := loader.FindPlugins(pluginDir)
	if err != nil {
		t.Fatalf("failed to find plugins: %v", err)
	}

	if len(plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(plugins))
	}
}

func TestValidatePlugin(t *testing.T) {
	registry := NewRegistry()
	pluginDir := filepath.Join(os.TempDir(), "plugin-validate-test")

	// Create valid plugin
	os.MkdirAll(pluginDir, 0755)
	defer os.RemoveAll(pluginDir)

	pluginPath := filepath.Join(pluginDir, "test-plugin")
	os.MkdirAll(pluginPath, 0755)

	manifest := PluginManifest{
		ID:          "test-plugin",
		Name:        "Test Plugin",
		Version:     "1.0.0",
		Type:        TypeScript,
		Entrypoint:  "run.sh",
	}
	manifestData := toJSON(manifest)
	os.WriteFile(filepath.Join(pluginPath, "manifest.json"), manifestData, 0644)
	os.WriteFile(filepath.Join(pluginPath, "run.sh"), []byte("#!/bin/bash"), 0755)

	config := &LoaderConfig{
		PluginDir: pluginDir,
	}
	loader := NewLoader(registry, config)

	err := loader.Validate(pluginPath)
	if err != nil {
		t.Fatalf("failed to validate plugin: %v", err)
	}
}

func TestDetectPluginType(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected PluginType
	}{
		{
			name:     "go plugin",
			files:    []string{"plugin.so"},
			expected: TypeGo,
		},
		{
			name:     "shell script",
			files:    []string{"run.sh"},
			expected: TypeScript,
		},
		{
			name:     "python script",
			files:    []string{"run.py"},
			expected: TypeScript,
		},
		{
			name:     "javascript",
			files:    []string{"run.js"},
			expected: TypeScript,
		},
		{
			name:     "binary",
			files:    []string{"binary"},
			expected: TypeBinary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(os.TempDir(), "plugin-type-test-"+tt.name)
			os.MkdirAll(dir, 0755)
			defer os.RemoveAll(dir)

			for _, file := range tt.files {
				path := filepath.Join(dir, file)
				if tt.expected == TypeBinary {
					os.WriteFile(path, []byte("#!/bin/bash"), 0755)
					os.Chmod(path, 0755)
				} else {
					os.WriteFile(path, []byte("content"), 0644)
				}
			}

			detected := detectPluginType(dir)
			if detected != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, detected)
			}
		})
	}
}

func TestCreateSamplePlugin(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "sample-plugin-test")
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)

	err := CreateSamplePlugin(dir)
	if err != nil {
		t.Fatalf("failed to create sample plugin: %v", err)
	}

	// Check manifest exists
	manifestPath := filepath.Join(dir, "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("manifest.json not created")
	}

	// Check script exists
	scriptPath := filepath.Join(dir, "run.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Error("run.sh not created")
	}
}

// Helper function
func toJSON(v interface{}) []byte {
	// Simple JSON marshal for testing
	return []byte(`{"id":"test","name":"Test","version":"1.0.0","type":"script"}`)
}
