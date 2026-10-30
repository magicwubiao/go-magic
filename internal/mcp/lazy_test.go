package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// 懒连接的三条不变量：
//  1. 启动阶段不建连，但 server 仍出现在 ListServers 里（注册表要遍历它）；
//  2. 有缓存时 ListToolsByServer 返回影子工具；
//  3. 首次调用才真正建连，失败后保留登记以便重试。
func TestLazyServerDefersConnection(t *testing.T) {
	m := NewManager()
	// 懒连接的前提是"有缓存"：没有工具清单就无从注册影子工具。
	m.rememberTools("slow", []Tool{{Name: "op"}})
	servers := map[string]ServerConfig{
		"slow": {
			Command: "definitely-not-a-real-binary-for-lazy-test",
			Lazy:    true,
		},
	}

	// 有缓存的 lazy server 即使命令不存在也不会在启动阶段报错——它根本没去执行。
	if err := m.ConnectConfigured(servers); err != nil {
		t.Fatalf("ConnectConfigured with lazy server must not fail: %v", err)
	}

	if !m.IsDeferred("slow") {
		t.Fatal("server should be registered as deferred")
	}

	found := false
	for _, name := range m.ListServers() {
		if name == "slow" {
			found = true
		}
	}
	if !found {
		t.Fatal("deferred server must appear in ListServers")
	}

	tools, err := m.ListToolsByServer("slow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("want cached shadow tool, got %d", len(tools))
	}
}

// 没有缓存的 lazy server 必须退化成"先连一次"：否则工具面为空，
// 模型看不到它，也就永远不会触发首次调用——装了等于没装。
func TestLazyServerWithoutCacheConnectsEagerly(t *testing.T) {
	m := NewManager()
	err := m.ConnectConfigured(map[string]ServerConfig{
		"nocache": {Command: "definitely-not-a-real-binary-for-lazy-test", Lazy: true},
	})
	if err == nil {
		t.Fatal("expected a connect failure for a missing binary")
	}
	if m.IsDeferred("nocache") {
		t.Fatal("server without cache must not be deferred")
	}
}

func TestLazyServerServesCachedTools(t *testing.T) {
	m := NewManager()
	cached := []Tool{{
		Name:        "get_weather",
		Description: "Get the weather",
		InputSchema: map[string]interface{}{"type": "object"},
	}}
	m.rememberTools("weather", cached)

	if err := m.ConnectConfigured(map[string]ServerConfig{
		"weather": {Command: "definitely-not-a-real-binary", Lazy: true},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tools, err := m.ListToolsByServer("weather")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "get_weather" {
		t.Fatalf("want cached shadow tool, got %+v", tools)
	}
}

func TestLazyConnectFailureKeepsRegistration(t *testing.T) {
	m := NewManager()
	m.rememberTools("broken", []Tool{{Name: "op"}})
	if err := m.ConnectConfigured(map[string]ServerConfig{
		"broken": {Command: "definitely-not-a-real-binary-for-lazy-test", Lazy: true},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err := m.EnsureConnected("broken")
	if err == nil {
		t.Fatal("EnsureConnected should fail for a missing binary")
	}
	// 失败后必须还能重试：否则一次失败就让该 server 在本进程里永久失效。
	if !m.IsDeferred("broken") {
		t.Fatal("deferred registration must survive a failed lazy connect")
	}
}

func TestLazyServerKeepsRequireApproval(t *testing.T) {
	m := NewManager()
	m.rememberTools("danger", []Tool{{Name: "op"}})
	if err := m.ConnectConfigured(map[string]ServerConfig{
		"danger": {Command: "x", Lazy: true, RequireApproval: true},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 没连上也不能"顺手免审批"。
	if !m.ServerRequiresApproval("danger") {
		t.Fatal("require_approval must apply to deferred servers")
	}
}

func TestToolCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp", "tool_cache.json")

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	want := toolCacheFile{
		Version: toolCacheVersion,
		Servers: map[string][]Tool{
			"fs": {{Name: "read_file", Description: "read"}},
		},
	}
	if err := writeToolCache(path, want); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := loadToolCache(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got["fs"]) != 1 || got["fs"][0].Name != "read_file" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	// 损坏的缓存不能让启动失败，只应被当作"没有缓存"。
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadToolCache(path); err == nil {
		t.Fatal("expected error for corrupt cache")
	}
}

func TestPruneCacheDropsRemovedServers(t *testing.T) {
	m := NewManager()
	m.rememberTools("gone", []Tool{{Name: "a"}})
	m.rememberTools("kept", []Tool{{Name: "b"}})

	m.pruneCache(map[string]struct{}{"kept": {}})

	if got := m.CachedServers(); len(got) != 1 || got[0] != "kept" {
		t.Fatalf("want only kept, got %v", got)
	}
}
