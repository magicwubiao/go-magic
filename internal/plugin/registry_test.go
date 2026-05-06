package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.plugins == nil {
		t.Error("expected plugins map to be initialized")
	}
	if r.index == nil {
		t.Error("expected index to be initialized")
	}
}

func TestRegisterPlugin(t *testing.T) {
	r := NewRegistry()

	plugin := &MockPlugin{
		manifest: &PluginManifest{
			ID:   "test-plugin",
			Name: "Test Plugin",
			Version: "1.0.0",
			Type: TypeScript,
		},
	}

	err := r.Register(plugin)
	if err != nil {
		t.Fatalf("failed to register plugin: %v", err)
	}

	// Check that plugin is registered
	p, exists := r.Get("test-plugin")
	if !exists {
		t.Error("plugin not found after registration")
	}
	if p == nil {
		t.Error("plugin is nil after registration")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	r := NewRegistry()

	plugin := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "test-plugin",
			Name:    "Test Plugin",
			Version: "1.0.0",
			Type:    TypeScript,
		},
	}

	err := r.Register(plugin)
	if err != nil {
		t.Fatalf("failed to register plugin: %v", err)
	}

	// Try to register again
	err = r.Register(plugin)
	if err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegisterDuplicateName(t *testing.T) {
	r := NewRegistry()

	plugin1 := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "test-plugin-1",
			Name:    "Test Plugin",
			Version: "1.0.0",
			Type:    TypeScript,
		},
	}

	plugin2 := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "test-plugin-2",
			Name:    "Test Plugin", // Same name
			Version: "1.0.0",
			Type:    TypeScript,
		},
	}

	err := r.Register(plugin1)
	if err != nil {
		t.Fatalf("failed to register first plugin: %v", err)
	}

	err = r.Register(plugin2)
	if err == nil {
		t.Error("expected error for duplicate name")
	}
}

func TestUnregisterPlugin(t *testing.T) {
	r := NewRegistry()

	plugin := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "test-plugin",
			Name:    "Test Plugin",
			Version: "1.0.0",
			Type:    TypeScript,
		},
	}

	r.Register(plugin)

	err := r.Unregister("test-plugin")
	if err != nil {
		t.Fatalf("failed to unregister plugin: %v", err)
	}

	// Check that plugin is gone
	_, exists := r.Get("test-plugin")
	if exists {
		t.Error("plugin still exists after unregister")
	}
}

func TestUnregisterNonexistent(t *testing.T) {
	r := NewRegistry()

	err := r.Unregister("nonexistent")
	if err == nil {
		t.Error("expected error for unregistering nonexistent plugin")
	}
}

func TestEnableDisable(t *testing.T) {
	r := NewRegistry()

	plugin := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "test-plugin",
			Name:    "Test Plugin",
			Version: "1.0.0",
			Type:    TypeScript,
		},
	}

	r.Register(plugin)

	// Enable
	err := r.Enable("test-plugin")
	if err != nil {
		t.Fatalf("failed to enable plugin: %v", err)
	}

	info, _ := r.GetInfo("test-plugin")
	if info.State != StateEnabled {
		t.Errorf("expected state %s, got %s", StateEnabled, info.State)
	}

	// Disable
	err = r.Disable("test-plugin")
	if err != nil {
		t.Fatalf("failed to disable plugin: %v", err)
	}

	info, _ = r.GetInfo("test-plugin")
	if info.State != StateDisabled {
		t.Errorf("expected state %s, got %s", StateDisabled, info.State)
	}
}

func TestList(t *testing.T) {
	r := NewRegistry()

	// Register multiple plugins
	for i := 0; i < 3; i++ {
		plugin := &MockPlugin{
			manifest: &PluginManifest{
				ID:      "test-plugin-" + string(rune('a'+i)),
				Name:    "Test Plugin " + string(rune('A'+i)),
				Version: "1.0.0",
				Type:    TypeScript,
			},
		}
		r.Register(plugin)
	}

	manifests := r.List()
	if len(manifests) != 3 {
		t.Errorf("expected 3 plugins, got %d", len(manifests))
	}
}

func TestListByCategory(t *testing.T) {
	r := NewRegistry()

	plugin1 := &MockPlugin{
		manifest: &PluginManifest{
			ID:       "test-plugin-1",
			Name:     "Test Plugin 1",
			Version:  "1.0.0",
			Type:     TypeScript,
			Category: "utilities",
		},
	}

	plugin2 := &MockPlugin{
		manifest: &PluginManifest{
			ID:       "test-plugin-2",
			Name:     "Test Plugin 2",
			Version:  "1.0.0",
			Type:     TypeScript,
			Category: "productivity",
		},
	}

	r.Register(plugin1)
	r.Register(plugin2)

	// List utilities
	utils := r.ListByCategory("utilities")
	if len(utils) != 1 {
		t.Errorf("expected 1 utility plugin, got %d", len(utils))
	}

	// List productivity
	prod := r.ListByCategory("productivity")
	if len(prod) != 1 {
		t.Errorf("expected 1 productivity plugin, got %d", len(prod))
	}
}

func TestListByTag(t *testing.T) {
	r := NewRegistry()

	plugin := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "test-plugin",
			Name:    "Test Plugin",
			Version: "1.0.0",
			Type:    TypeScript,
			Tags:    []string{"search", "research"},
		},
	}

	r.Register(plugin)

	search := r.ListByTag("search")
	if len(search) != 1 {
		t.Errorf("expected 1 plugin with tag 'search', got %d", len(search))
	}
}

func TestSearch(t *testing.T) {
	r := NewRegistry()

	plugin := &MockPlugin{
		manifest: &PluginManifest{
			ID:          "code-review",
			Name:        "Code Review Plugin",
			Description: "Reviews code for bugs and style issues",
			Version:     "1.0.0",
			Type:        TypeScript,
		},
	}

	r.Register(plugin)

	// Search by name
	results := r.Search("code")
	if len(results) != 1 {
		t.Errorf("expected 1 search result, got %d", len(results))
	}

	// Search by description
	results = r.Search("bugs")
	if len(results) != 1 {
		t.Errorf("expected 1 search result, got %d", len(results))
	}

	// Search by partial match
	results = r.Search("plugin")
	if len(results) != 1 {
		t.Errorf("expected 1 search result, got %d", len(results))
	}

	// Search with no results
	results = r.Search("nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 search results, got %d", len(results))
	}
}

func TestCategories(t *testing.T) {
	r := NewRegistry()

	plugin1 := &MockPlugin{
		manifest: &PluginManifest{
			ID:       "test-plugin-1",
			Name:     "Test Plugin 1",
			Version:  "1.0.0",
			Type:     TypeScript,
			Category: "utilities",
		},
	}

	plugin2 := &MockPlugin{
		manifest: &PluginManifest{
			ID:       "test-plugin-2",
			Name:     "Test Plugin 2",
			Version:  "1.0.0",
			Type:     TypeScript,
			Category: "utilities",
		},
	}

	r.Register(plugin1)
	r.Register(plugin2)

	cats := r.Categories()
	if len(cats) != 1 {
		t.Errorf("expected 1 category, got %d", len(cats))
	}
}

func TestCount(t *testing.T) {
	r := NewRegistry()

	if r.Count() != 0 {
		t.Errorf("expected 0 plugins, got %d", r.Count())
	}

	for i := 0; i < 5; i++ {
		plugin := &MockPlugin{
			manifest: &PluginManifest{
				ID:      "test-plugin-" + string(rune('0'+i)),
				Name:    "Test Plugin " + string(rune('0'+i)),
				Version: "1.0.0",
				Type:    TypeScript,
			},
		}
		r.Register(plugin)
	}

	if r.Count() != 5 {
		t.Errorf("expected 5 plugins, got %d", r.Count())
	}
}

func TestCountByState(t *testing.T) {
	r := NewRegistry()

	plugin1 := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "test-plugin-1",
			Name:    "Test Plugin 1",
			Version: "1.0.0",
			Type:    TypeScript,
		},
	}

	plugin2 := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "test-plugin-2",
			Name:    "Test Plugin 2",
			Version: "1.0.0",
			Type:    TypeScript,
		},
	}

	r.Register(plugin1)
	r.Register(plugin2)

	r.Enable("test-plugin-1")

	counts := r.CountByState()
	if counts[StateLoaded] != 1 {
		t.Errorf("expected 1 loaded plugin, got %d", counts[StateLoaded])
	}
	if counts[StateEnabled] != 1 {
		t.Errorf("expected 1 enabled plugin, got %d", counts[StateEnabled])
	}
}

func TestResolveDependencies(t *testing.T) {
	r := NewRegistry()

	dep := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "dependency",
			Name:    "Dependency Plugin",
			Version: "1.0.0",
			Type:    TypeScript,
		},
	}

	plugin := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "test-plugin",
			Name:    "Test Plugin",
			Version: "1.0.0",
			Type:    TypeScript,
			Dependencies: []Dependency{
				{ID: "dependency", Version: "^1.0.0"},
			},
		},
	}

	r.Register(dep)

	err := r.ResolveDependencies("test-plugin")
	if err != nil {
		t.Fatalf("failed to resolve dependencies: %v", err)
	}

	// Now try to resolve for a plugin with missing dependency
	plugin2 := &MockPlugin{
		manifest: &PluginManifest{
			ID:      "test-plugin-2",
			Name:    "Test Plugin 2",
			Version: "1.0.0",
			Type:    TypeScript,
			Dependencies: []Dependency{
				{ID: "missing", Version: "^1.0.0"},
			},
		},
	}

	r.Register(plugin2)

	err = r.ResolveDependencies("test-plugin-2")
	if err == nil {
		t.Error("expected error for missing dependency")
	}
}

// MockPlugin is a test implementation of Plugin
type MockPlugin struct {
	manifest *PluginManifest
}

func (m *MockPlugin) Manifest() *PluginManifest {
	return m.manifest
}

func (m *MockPlugin) Initialize(ctx *Context) error {
	return nil
}

func (m *MockPlugin) Execute(cmd string, args []string) (interface{}, error) {
	return nil, nil
}

func (m *MockPlugin) Shutdown() error {
	return nil
}

func TestMain(m *testing.M) {
	os.MkdirAll(filepath.Join(os.TempDir(), "plugin-test"), 0755)
	os.Exit(m.Run())
}
