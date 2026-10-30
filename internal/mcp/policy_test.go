package mcp

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// 准入校验必须在真正建连之前生效：否则一个被策略拒绝的 server 仍然会
// 拉起子进程/发出网络请求，"拒绝"就只是事后报错。
func TestSecurityCheckBlocksConnect(t *testing.T) {
	m := NewManager()
	m.SetSecurityCheck(func(name string, cfg ServerConfig) error {
		return fmt.Errorf("host of %q is not in the policy allowlist", name)
	})

	err := m.ConnectStdio("blocked", ServerConfig{Command: "definitely-not-a-real-binary"})
	if err == nil {
		t.Fatal("connect must fail when policy denies")
	}
	if !strings.Contains(err.Error(), "policy allowlist") {
		t.Fatalf("error should come from the policy check, got: %v", err)
	}
	if m.isConnected("blocked") {
		t.Fatal("denied server must not be registered as connected")
	}
	if servers := m.ListServers(); len(servers) != 0 {
		t.Fatalf("denied server leaked into ListServers: %v", servers)
	}
}

// 校验通过时行为保持不变（不能因为加了钩子就把正常连接弄坏）。
func TestSecurityCheckAllowsConnect(t *testing.T) {
	m := NewManager()
	called := false
	m.SetSecurityCheck(func(name string, cfg ServerConfig) error {
		called = true
		if cfg.Command != "definitely-not-a-real-binary-policy" {
			t.Fatalf("check should see the config, got command %q", cfg.Command)
		}
		return nil
	})
	// 命令不存在会在建连阶段失败，但校验确实先跑过了。
	_ = m.ConnectStdio("srv", ServerConfig{Command: "definitely-not-a-real-binary-policy"})
	if !called {
		t.Fatal("security check was not invoked")
	}
}

// 懒连接重试也必须走同一道门：EnsureConnected 不能绕过校验。
func TestSecurityCheckAppliesToLazyEnsure(t *testing.T) {
	// 先种一条缓存：没有缓存的 lazy server 会退化成"启动就连接"（见 shouldDefer），
	// 那样根本走不到 EnsureConnected，测的就不是这条路径了。
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "tool_cache.json")
	if err := writeToolCache(cachePath, toolCacheFile{
		Version: 1,
		Servers: map[string][]Tool{"lazy": {{Name: "some_tool", Description: "shadow"}}},
	}); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	if err := m.SetToolCachePath(cachePath); err != nil {
		t.Fatal(err)
	}
	m.SetSecurityCheck(func(name string, cfg ServerConfig) error { return fmt.Errorf("denied by policy") })
	if err := m.ConnectConfigured(map[string]ServerConfig{
		"lazy": {Command: "definitely-not-a-real-binary", Lazy: true},
	}); err != nil {
		t.Fatalf("deferring must not fail: %v", err)
	}
	if !m.IsDeferred("lazy") {
		t.Fatal("server should be deferred when a cache exists")
	}
	if err := m.EnsureConnected("lazy"); err == nil {
		t.Fatal("lazy reconnect must still be checked")
	}
}
