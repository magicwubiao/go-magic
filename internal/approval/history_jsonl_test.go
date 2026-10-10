package approval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// approvalHistoryPath 返回当前 GO_MAGIC_HOME 下的审批历史文件路径。
func approvalHistoryPath(t *testing.T, home string) string {
	t.Helper()
	return filepath.Join(home, "approval", "history.json")
}

// appendApprovalRecord 直接往内存历史里补一条并同步落盘（绕开审批流程）。
func appendApprovalRecord(t *testing.T, m *Manager, id string) {
	t.Helper()
	m.mu.Lock()
	m.history = append(m.history, &ApprovalRecord{
		ID:        id,
		Command:   "echo " + id,
		Timestamp: time.Now(),
	})
	m.historyDirty = true
	m.mu.Unlock()
	if err := m.saveHistory(); err != nil {
		t.Fatalf("saveHistory: %v", err)
	}
}

// TestApprovalHistoryJSONLIsAppendOnly 锁死"历史只增时按 JSONL 追加写"。
//
// 改造前每次都 `json.MarshalIndent(全部记录)` + 整包 WriteFile：长任务里审批历史
// 上千条时，每写一条都要重新序列化全部记录。这里用"新旧文件字节前缀相同"来证明
// 第二次保存确实只追加、没有重写。
func TestApprovalHistoryJSONLIsAppendOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)

	m, err := NewManager(nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	appendApprovalRecord(t, m, "r1")
	first, err := os.ReadFile(approvalHistoryPath(t, home))
	if err != nil {
		t.Fatalf("read after first save: %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(string(first)), "[") {
		t.Fatalf("history file must be JSONL, not a JSON array: %s", first)
	}
	if lines := strings.Split(strings.TrimSpace(string(first)), "\n"); len(lines) != 1 {
		t.Fatalf("want exactly 1 JSONL line after first save, got %d", len(lines))
	}

	appendApprovalRecord(t, m, "r2")
	second, err := os.ReadFile(approvalHistoryPath(t, home))
	if err != nil {
		t.Fatalf("read after second save: %v", err)
	}
	// 关键断言：旧内容原封不动地留在文件开头 ⇒ 是追加而不是重写。
	if !strings.HasPrefix(string(second), string(first)) {
		t.Fatalf("second save must APPEND to the file, but the previous bytes changed:\n before: %q\n after:  %q", first, second)
	}
	if lines := strings.Split(strings.TrimSpace(string(second)), "\n"); len(lines) != 2 {
		t.Fatalf("want 2 JSONL lines after second save, got %d", len(lines))
	}

	// 重新加载：两条都要在，顺序按时间升序（最新在末尾）。
	m2, err := NewManager(nil)
	if err != nil {
		t.Fatalf("reload NewManager: %v", err)
	}
	if got := m2.HistoryLen(); got != 2 {
		t.Fatalf("reloaded history len = %d, want 2", got)
	}
	if last := m2.history[len(m2.history)-1]; last.ID != "r2" {
		t.Fatalf("newest record must be at the tail after reload, got %q", last.ID)
	}
}

// TestApprovalHistoryLegacyArrayMigratesToJSONL 覆盖旧格式迁移。
//
// 旧版本把整个历史写成 JSON 数组。追加写不能作用在数组文档上（会产出非法
// JSON），所以加载旧格式后必须在第一次保存时整写迁移为 JSONL。
func TestApprovalHistoryLegacyArrayMigratesToJSONL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)

	dir := filepath.Join(home, "approval")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := approvalHistoryPath(t, home)
	legacy := []*ApprovalRecord{{ID: "legacy-1", Command: "ls", Timestamp: time.Now()}}
	raw, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	m, err := NewManager(nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if got := m.HistoryLen(); got != 1 {
		t.Fatalf("legacy array must load, got %d records", got)
	}

	// 触发一次保存（追加一条新记录）。
	appendApprovalRecord(t, m, "new-1")

	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated: %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(string(migrated)), "[") {
		t.Fatalf("legacy array must be migrated to JSONL in place, got: %s", migrated)
	}
	lines := strings.Split(strings.TrimSpace(string(migrated)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 JSONL lines after migration, got %d: %s", len(lines), migrated)
	}

	// 迁移后必须能正常读回两条。
	m2, err := NewManager(nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := m2.HistoryLen(); got != 2 {
		t.Fatalf("after migration history len = %d, want 2", got)
	}
}

// TestApprovalHistoryTruncationFallsBackToRewrite 覆盖"历史被裁剪/过滤"的路径。
//
// m.history 在超过 10000 条时会被裁剪到最近 5000 条，ClearHistory 也会过滤。
// 这时"已落盘前缀"在内存里对不上，必须整写（compaction）——否则文件里会留着
// 已被裁掉的旧记录，重新加载后条数与内存不一致。
func TestApprovalHistoryTruncationFallsBackToRewrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", home)

	m, err := NewManager(nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	for _, id := range []string{"a", "b", "c"} {
		appendApprovalRecord(t, m, id)
	}

	// 模拟裁剪：丢掉最旧的一条。
	m.mu.Lock()
	m.history = m.history[1:]
	m.historyDirty = true
	m.mu.Unlock()
	if err := m.saveHistory(); err != nil {
		t.Fatalf("saveHistory after truncation: %v", err)
	}

	m2, err := NewManager(nil)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := m2.HistoryLen(); got != 2 {
		t.Fatalf("truncated history must be rewritten to disk; reloaded len = %d, want 2", got)
	}
	if first := m2.history[0]; first.ID != "b" {
		t.Fatalf("oldest surviving record must be %q, got %q", "b", first.ID)
	}
}
