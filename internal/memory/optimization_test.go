package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestStore 构造一个落在临时目录的 Store，避免污染真实 home。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("GO_MAGIC_HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.DBPath = filepath.Join(t.TempDir(), "memory.db")
	s, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestSanitizeFTSQuery_WordOR 验证自然语言查询被转成逐词 OR 匹配，
// 不再是整句短语（P0-3 修复的核心行为）。
func TestSanitizeFTSQuery_WordOR(t *testing.T) {
	got := sanitizeFTSQuery("how to deploy kubernetes")
	for _, want := range []string{`"how"*`, `"deploy"*`, `"kubernetes"*`, " OR "} {
		if !strings.Contains(got, want) {
			t.Errorf("sanitizeFTSQuery result %q should contain %q", got, want)
		}
	}
	// 整句短语匹配是旧实现的 bug 形态，不应出现
	if strings.Contains(got, `"how to deploy`) {
		t.Errorf("sanitizeFTSQuery should not produce a whole-sentence phrase: %q", got)
	}

	// 双引号转义
	if got := sanitizeFTSQuery(`say "hi"`); strings.Contains(got, `""`) == false {
		t.Errorf("double quotes should be escaped, got %q", got)
	}
	// 空查询
	if got := sanitizeFTSQuery("   \t\n"); got != "" {
		t.Errorf("blank query should sanitize to empty, got %q", got)
	}
	// 控制字符清理
	if got := sanitizeFTSQuery("hello\x00world"); !strings.Contains(got, `"hello"`) {
		t.Errorf("control chars should be stripped, got %q", got)
	}
}

// TestStore_WorkspaceScope 验证 scope 自动填充与 RecallScoped 过滤（P2-1）。
func TestStore_WorkspaceScope(t *testing.T) {
	s := newTestStore(t)
	if s.WorkspaceScope() == "" {
		t.Fatal("workspace scope should be auto-filled from cwd")
	}

	// m1: scope 自动填充为当前工作区；m2: 显式其他工作区；m3: user 类型跨工作区
	s.Store(&Memory{Type: TypeProject, Content: "kubernetes deployment uses helm charts", Importance: 0.8})
	s.Store(&Memory{Type: TypeProject, Content: "kubernetes rolling update policy", Scope: "/some/other/workspace", Importance: 0.8})
	s.Store(&Memory{Type: TypeUser, Content: "user prefers kubernetes over docker swarm", Scope: "/yet/another/ws", Importance: 0.9})

	// RecallScoped：应命中本工作区 m1 与跨工作区 user m3，不命中其他工作区的 m2
	results := s.GetTopMemoriesScoped("kubernetes", 10, time.Now())
	foundSelf, foundOther, foundUser := false, false, false
	for _, m := range results {
		switch {
		case strings.Contains(m.Content, "helm charts"):
			foundSelf = true
		case strings.Contains(m.Content, "rolling update"):
			foundOther = true
		case strings.Contains(m.Content, "docker swarm"):
			foundUser = true
		}
	}
	if !foundSelf {
		t.Error("RecallScoped should include memories scoped to the current workspace")
	}
	if foundOther {
		t.Error("RecallScoped should exclude memories scoped to other workspaces")
	}
	if !foundUser {
		t.Error("RecallScoped should include user-type memories regardless of scope")
	}
}

// TestStore_ContentDeduplication 验证相同内容的新增被合并为原地更新（P1-4）。
func TestStore_ContentDeduplication(t *testing.T) {
	s := newTestStore(t)

	if err := s.Store(&Memory{Type: TypeKnowledge, Content: "unique dedup marker content", Importance: 0.5}); err != nil {
		t.Fatalf("first store failed: %v", err)
	}
	if err := s.Store(&Memory{Type: TypeKnowledge, Content: "unique dedup marker content", Importance: 0.9}); err != nil {
		t.Fatalf("second store failed: %v", err)
	}

	all, err := s.List(TypeKnowledge, 100, 0)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	count := 0
	imp := 0.0
	for _, m := range all {
		if m.Content == "unique dedup marker content" {
			count++
			imp = m.Importance
		}
	}
	if count != 1 {
		t.Errorf("duplicate content should be merged into one row, got %d", count)
	}
	if imp < 0.9 {
		t.Errorf("importance should be raised to max(0.5, 0.9), got %v", imp)
	}
}

// TestMergeIntoMarkdown_SectionMerge 验证分节合并语义（P1-1）。
func TestMergeIntoMarkdown_SectionMerge(t *testing.T) {
	existing := "# Notes\n\n## A\nline1\nline2\n\n## B\nold\n"

	// 同名分节合并 + 新分节追加
	merged := mergeIntoMarkdown(existing, "## A\nline3\n\n## C\nfresh\n", 100000, truncateString)
	if !strings.Contains(merged, "line3") {
		t.Error("mergeIntoMarkdown should merge body lines into existing section A")
	}
	if strings.Count(merged, "## A") != 1 {
		t.Errorf("section A header should appear exactly once, got:\n%s", merged)
	}
	if !strings.Contains(merged, "## C") || !strings.Contains(merged, "fresh") {
		t.Error("mergeIntoMarkdown should append new section C")
	}
	if !strings.Contains(merged, "old") {
		t.Error("existing section B content should be preserved")
	}

	// 无分节头的 addition 走追加路径
	appended := mergeIntoMarkdown(existing, "plain trailing line", 100000, truncateString)
	if !strings.Contains(appended, "plain trailing line") {
		t.Error("header-less addition should be appended")
	}

	// 空-existing 路径
	if got := mergeIntoMarkdown("", "## X\nval\n", 100000, truncateString); !strings.Contains(got, "val") {
		t.Errorf("empty existing should return addition, got %q", got)
	}

	// 超限触发压缩回调
	compressCalled := false
	_ = mergeIntoMarkdown(existing, "## A\nextra line to force over limit\n", 40, func(c string, limit int) string {
		compressCalled = true
		return truncateString(c, limit)
	})
	if !compressCalled {
		t.Error("compress callback should be invoked when merged content exceeds limit")
	}
}

// TestAppendToMemory_SectionAware 验证 SnapshotManager.AppendToMemory 走分节合并。
func TestAppendToMemory_SectionAware(t *testing.T) {
	t.Setenv("GO_MAGIC_HOME", t.TempDir())
	sm := NewSnapshotManager(t.TempDir())

	if err := sm.AppendToMemory("## Deployment\nuses blue-green strategy"); err != nil {
		t.Fatalf("append 1 failed: %v", err)
	}
	if err := sm.AppendToMemory("## Deployment\nrollback is automatic"); err != nil {
		t.Fatalf("append 2 failed: %v", err)
	}

	content := sm.GetLatestMemory()
	if strings.Count(content, "## Deployment") != 1 {
		t.Errorf("same-topic section should be merged, got:\n%s", content)
	}
	if !strings.Contains(content, "blue-green") || !strings.Contains(content, "rollback") {
		t.Errorf("both lines should be present, got:\n%s", content)
	}
}

// TestDistiller_BasicFallback 验证蒸馏器无 LLM 时的基础摘要路径（P1-3）：
// 旧日志 → 合并进 MEMORY.md → 删除旧日志 → 状态文件幂等。
func TestDistiller_BasicFallback(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "daily")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldLog := filepath.Join(logDir, "2025-07-15.md")
	content := strings.Repeat("- 10:00:00 [user] discussed deployment strategy for the api gateway\n", 10)
	if err := os.WriteFile(oldLog, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDistiller(DistillerConfig{
		LogDir:       logDir,
		MemoryMDPath: filepath.Join(dir, "MEMORY.md"),
		Retention:    30 * 24 * time.Hour,
		MinChars:     10,
		// StateFile 留空 → 默认 logDir/.last_distill
	}, nil, nil, nil)

	if err := d.RunIfNeeded(); err != nil {
		t.Fatalf("RunIfNeeded failed: %v", err)
	}

	merged, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("MEMORY.md should exist after distill: %v", err)
	}
	if !strings.Contains(string(merged), "Conversation Digest") || !strings.Contains(string(merged), "api gateway") {
		t.Errorf("MEMORY.md should contain the digest, got:\n%s", string(merged))
	}
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Error("old log file should be deleted after distillation")
	}
	statePath := filepath.Join(logDir, ".last_distill")
	if _, err := os.Stat(statePath); err != nil {
		t.Error("state file should exist after distillation")
	}

	// 幂等：当日再跑不应报错也不应重复蒸馏
	if err := d.RunIfNeeded(); err != nil {
		t.Fatalf("second RunIfNeeded failed: %v", err)
	}
}

// TestCleanupExpired 验证低重要度过期记录被清理、高重要度保留（P2-3）。
func TestCleanupExpired(t *testing.T) {
	s := newTestStore(t)

	s.Store(&Memory{Type: TypeKnowledge, Content: "low value expired note", Importance: 0.1})
	s.Store(&Memory{Type: TypeKnowledge, Content: "high value keep note", Importance: 0.95})

	deleted, err := s.CleanupExpired(time.Now().Add(time.Second), 0.3)
	if err != nil {
		t.Fatalf("CleanupExpired failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deletion, got %d", deleted)
	}

	all, _ := s.List(TypeKnowledge, 100, 0)
	for _, m := range all {
		if m.Content == "low value expired note" {
			t.Error("low-importance expired memory should have been deleted")
		}
	}
}

// TestFTSStore_SearchSanitizeAndFallback 验证 FTSStore.Search 的
// sanitize 接入与 CJK LIKE 兜底 + 相关性排序（P0-3/P2-4）。
func TestFTSStore_SearchSanitizeAndFallback(t *testing.T) {
	dir := t.TempDir()
	fts, err := NewFTSStore(dir)
	if err != nil {
		t.Fatalf("NewFTSStore failed: %v", err)
	}
	t.Cleanup(func() { fts.Close() })

	fts.Add(&MemoryRecord{SessionID: "s1", Role: "user", Content: "we deployed kubernetes with helm yesterday", Importance: 5})
	fts.Add(&MemoryRecord{SessionID: "s2", Role: "user", Content: "讨论了记忆系统的优化方案", Importance: 5})

	// 英文：经 sanitize 的逐词 OR 应能命中（旧实现整句短语必空）
	res, err := fts.Search("tell me about kubernetes deployment", 5)
	if err != nil {
		t.Fatalf("FTS search failed: %v", err)
	}
	if len(res) == 0 || !strings.Contains(res[0].Content, "kubernetes") {
		t.Errorf("sanitized FTS query should hit the kubernetes record, got %d results", len(res))
	}

	// CJK：走 LIKE 兜底
	res, err = fts.Search("记忆系统优化", 5)
	if err != nil {
		t.Fatalf("CJK search failed: %v", err)
	}
	if len(res) == 0 || !strings.Contains(res[0].Content, "记忆系统") {
		t.Errorf("CJK fallback should hit the Chinese record, got %d results", len(res))
	}
}
