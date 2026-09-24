package bot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// newTestManager starts a Manager over a throwaway magic home with the given
// bots registered.
//
// These tests exercise bot-mode bookkeeping — rooms, routines, runtime status,
// config hot-reload — which never talks to a model, so no mock LLM endpoint is
// needed (see setupEnv in e2e_test.go for the turn-running harness).
//
// The manager is stopped automatically. t.TempDir() cleanup is registered
// first, so Stop() (which closes bots.db) runs before the directory is removed;
// on Windows an open SQLite handle would otherwise break the cleanup.
func newTestManager(t *testing.T, botNames ...string) *Manager {
	t.Helper()

	home := t.TempDir()
	cfgData := map[string]interface{}{
		"provider": "custom",
		"providers": map[string]interface{}{
			"custom": map[string]interface{}{
				"api_key": "test-key",
				"models":  []string{"mock-model"},
			},
		},
		"bot_mode": map[string]interface{}{"enabled": true},
	}
	raw, _ := json.Marshal(cfgData)
	if err := os.WriteFile(filepath.Join(home, "config.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_MAGIC_HOME", home)

	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range botNames {
		if err := store.Save(&Config{Name: n, Title: n}); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	mgr, err := NewManager(cfg)
	if err != nil || mgr == nil {
		t.Fatalf("NewManager: %v (nil manager = bot mode off?)", err)
	}
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("manager start: %v", err)
	}
	t.Cleanup(mgr.Stop)
	return mgr
}
