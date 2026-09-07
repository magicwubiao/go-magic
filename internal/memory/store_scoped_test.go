package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newDirScopeStore 建一个指向同一 DB 文件但 WorkspaceScope 不同的 Store 实例，
// 模拟同一进程里不同「目录记忆桶」共用一份记忆库（目录级共享记忆）。
func newDirScopeStore(t *testing.T, dbPath, scope string) *Store {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DBPath = dbPath
	cfg.WorkspaceScope = scope
	s, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore(scope=%q) failed: %v", scope, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestDirScopeIsolation 验证目录级共享记忆核心语义：
// 同 scope（目录）的会话共享、不同 scope 互不串扰；user/preference 画像跨目录
// 贯通；显式指定 scope 的记忆落指定桶（不被实例默认 scope 改写）。
func TestDirScopeIsolation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	alpha := newDirScopeStore(t, dbPath, "d:/work/alpha")
	beta := newDirScopeStore(t, dbPath, "d:/work/beta")

	// alpha 目录的会话沉淀一条 project 记忆 → 应自动落 alpha 桶
	if err := alpha.Store(&Memory{Type: TypeProject, Content: "alpha has secret learn config v1", Importance: 0.8}); err != nil {
		t.Fatalf("store alpha mem: %v", err)
	}
	// beta 目录的会话沉淀一条 project 记忆 → 应自动落 beta 桶
	if err := beta.Store(&Memory{Type: TypeProject, Content: "beta has secret learn config v1", Importance: 0.8}); err != nil {
		t.Fatalf("store beta mem: %v", err)
	}
	// user 画像：不带 scope，跨目录可见
	if err := alpha.Store(&Memory{Type: TypeUser, Content: "user likes coffee in the morning", Importance: 0.8}); err != nil {
		t.Fatalf("store user mem: %v", err)
	}

	// alpha 桶召回：命中 alpha 记忆，见不到 beta 的记忆
	gotA, err := alpha.RecallScopedBy("d:/work/alpha", "secret learn config", 10)
	if err != nil {
		t.Fatalf("recall alpha failed: %v", err)
	}
	if !containsContent(gotA, "alpha has secret") {
		t.Errorf("alpha bucket should contain alpha mem, got %d results", len(gotA))
	}
	if containsContent(gotA, "beta has secret") {
		t.Errorf("alpha bucket must NOT contain beta mem")
	}

	// beta 桶召回：只见 beta 记忆
	gotB, err := beta.RecallScopedBy("d:/work/beta", "secret learn config", 10)
	if err != nil {
		t.Fatalf("recall beta failed: %v", err)
	}
	if !containsContent(gotB, "beta has secret") {
		t.Errorf("beta bucket should contain beta mem")
	}
	if containsContent(gotB, "alpha has secret") {
		t.Errorf("beta bucket must NOT contain alpha mem")
	}

	// user 画像跨目录贯通：查询命中 user 内容时，两个目录桶都能召回同一份画像
	for _, scope := range []string{"d:/work/alpha", "d:/work/beta"} {
		gotU, err := alpha.RecallScopedBy(scope, "user likes coffee morning", 10)
		if err != nil {
			t.Fatalf("recall user mem in %s failed: %v", scope, err)
		}
		if !containsContent(gotU, "user likes coffee") {
			t.Errorf("user profile should be visible in bucket %s", scope)
		}
	}
}

// TestDirScopeExplicitStamp 验证显式 scope 的记忆不被实例默认 workspace scope
// 改写（cortex 提取链路 applyMemoryScope 后走的就是这条路径）。
func TestDirScopeExplicitStamp(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	alpha := newDirScopeStore(t, dbPath, "d:/work/alpha")
	beta := newDirScopeStore(t, dbPath, "d:/work/beta")

	// 通过 alpha 实例写入但显式指定 beta scope → 必须落在 beta 桶
	if err := alpha.Store(&Memory{Type: TypeProject, Scope: "d:/work/beta", Content: "manual scope note goes beta dir", Importance: 0.8}); err != nil {
		t.Fatalf("store explicit-scope mem: %v", err)
	}

	gotB, err := beta.RecallScopedBy("d:/work/beta", "manual scope note", 10)
	if err != nil {
		t.Fatalf("recall beta failed: %v", err)
	}
	if !containsContent(gotB, "manual scope note") {
		t.Errorf("explicit beta-scope mem should be recalled in beta bucket")
	}
	gotA, err := alpha.RecallScopedBy("d:/work/alpha", "manual scope note", 10)
	if err != nil {
		t.Fatalf("recall alpha failed: %v", err)
	}
	if containsContent(gotA, "manual scope note") {
		t.Errorf("explicit beta-scope mem must NOT leak into alpha bucket")
	}
}

// TestGetTopMemoriesByScope 验证按目录 scope 的高分召回门面可用且默认 scope
// 退化路径与旧 API 一致。
func TestGetTopMemoriesByScope(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	alpha := newDirScopeStore(t, dbPath, "d:/work/alpha")

	if err := alpha.Store(&Memory{Type: TypeProject, Content: "deploy github actions with review gate enforced", Importance: 0.9}); err != nil {
		t.Fatalf("store: %v", err)
	}
	tops := alpha.GetTopMemoriesByScope("d:/work/alpha", "deploy github actions", 5, time.Now())
	if len(tops) != 1 {
		t.Fatalf("expected 1 top memory, got %d", len(tops))
	}
	if !containsContent(tops, "deploy github actions") {
		t.Errorf("top memory mismatch: %+v", tops)
	}
	// 空 scope 退化为实例默认 workspace scope，与 GetTopMemoriesScoped 一致
	tops2 := alpha.GetTopMemoriesByScope("", "deploy github actions", 5, time.Now())
	if len(tops2) != 1 {
		t.Errorf("empty-scope fallback should hit workspace scope, got %d", len(tops2))
	}
}

func containsContent(mems []*Memory, sub string) bool {
	for _, m := range mems {
		if m != nil && strings.Contains(m.Content, sub) {
			return true
		}
	}
	return false
}
