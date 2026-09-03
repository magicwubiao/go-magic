package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSnapshotManagerLoad 测试从磁盘加载 memory
func TestSnapshotManagerLoad(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("# Memory\nKey fact\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "USER.md"), []byte("# User\nPrefers Go\n"), 0644)

	sm := NewSnapshotManager(tmpDir)
	if err := sm.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if sm.GetMemoryForPrompt() != "# Memory\nKey fact\n" {
		t.Errorf("Memory load wrong: %q", sm.GetMemoryForPrompt())
	}
	if sm.GetUserForPrompt() != "# User\nPrefers Go\n" {
		t.Errorf("User load wrong: %q", sm.GetUserForPrompt())
	}
}

// TestSnapshotFreeze 测试冻结快照机制
func TestSnapshotFreeze(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSnapshotManager(tmpDir)
	sm.Load()

	sm.UpdateMemory("v1")
	sm.RefreshSnapshot()

	// 冻结后修改
	sm.OnTurnStart()
	sm.UpdateMemory("v2")

	// frozen 应该是 v1
	if sm.GetMemoryForPrompt() != "v1" {
		t.Errorf("Frozen memory should be v1, got %q", sm.GetMemoryForPrompt())
	}
	// latest 应该是 v2
	if sm.GetLatestMemory() != "v2" {
		t.Errorf("Latest memory should be v2, got %q", sm.GetLatestMemory())
	}
}

// TestSnapshotRefresh 测试 RefreshSnapshot 更新冻结版本
func TestSnapshotRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSnapshotManager(tmpDir)
	sm.Load()

	sm.UpdateMemory("old")
	sm.RefreshSnapshot()
	v1 := sm.GetVersion()

	sm.UpdateMemory("new")
	sm.RefreshSnapshot()
	v2 := sm.GetVersion()

	if v2 != v1+1 {
		t.Errorf("Version not incremented: %d -> %d", v1, v2)
	}
	if sm.GetMemoryForPrompt() != "new" {
		t.Errorf("Refresh failed, got %q", sm.GetMemoryForPrompt())
	}
}

// TestMemoryAppend 测试 AppendToMemory
func TestMemoryAppend(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSnapshotManager(tmpDir)
	sm.Load()

	sm.AppendToMemory("Line 1")
	sm.AppendToMemory("Line 2")

	mem := sm.GetLatestMemory()
	if !strings.Contains(mem, "Line 1") || !strings.Contains(mem, "Line 2") {
		t.Errorf("Append failed: %q", mem)
	}

	// 验证写入磁盘
	data, _ := os.ReadFile(filepath.Join(tmpDir, "MEMORY.md"))
	if !strings.Contains(string(data), "Line 2") {
		t.Error("Memory not persisted to disk")
	}
}

// TestMemoryLimitEnforcement 测试 memory 超限自动压缩
func TestMemoryLimitEnforcement(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSnapshotManager(tmpDir)
	sm.Load()

	// 构造超长内容
	longContent := strings.Repeat("This is a very long line that will exceed the memory limit eventually. ", 100)
	sm.UpdateMemory(longContent)

	mem := sm.GetLatestMemory()
	if len(mem) > MemoryLimitChars {
		t.Errorf("Memory exceeds limit: %d > %d", len(mem), MemoryLimitChars)
	}
}

// TestUserLimitEnforcement 测试 user profile 超限压缩
func TestUserLimitEnforcement(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSnapshotManager(tmpDir)
	sm.Load()

	longContent := strings.Repeat("User preference detail. ", 200)
	sm.UpdateUser(longContent)

	user := sm.GetLatestUser()
	if len(user) > UserLimitChars {
		t.Errorf("User exceeds limit: %d > %d", len(user), UserLimitChars)
	}
}

// TestConcurrentAccess 测试并发读写安全
func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSnapshotManager(tmpDir)
	sm.Load()

	done := make(chan bool, 3)

	go func() {
		for i := 0; i < 100; i++ {
			sm.AppendToMemory("concurrent write")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = sm.GetMemoryForPrompt()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			sm.RefreshSnapshot()
		}
		done <- true
	}()

	for i := 0; i < 3; i++ {
		<-done
	}
	// 无 panic 即通过
}

// TestMemoryCompressorDeduplication 测试去重功能
func TestMemoryCompressorDeduplication(t *testing.T) {
	mc := &MemoryCompressor{}
	input := "Line A\nLine A\nLine A\nLine B\nLine B\nLine C"
	result := mc.CompressMemory(input, MemoryLimitChars)

	// 去重后应该只有 3 个非空行
	lines := strings.Split(result, "\n")
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" && !strings.Contains(l, "[...") {
			nonEmpty++
		}
	}
	if nonEmpty > 5 {
		t.Errorf("Deduplication failed, too many lines: %d", nonEmpty)
	}
}

// TestMemoryCompressorSectionPreservation 测试 section 标题保留
func TestMemoryCompressorSectionPreservation(t *testing.T) {
	mc := &MemoryCompressor{}
	input := "## Section A\nDetail 1\nDetail 2\nDetail 3\n## Section B\nDetail 4\nDetail 5\n## Section C\nDetail 6"
	result := mc.CompressMemory(input, 200)

	if !strings.Contains(result, "## Section A") {
		t.Error("Section A header lost")
	}
	if !strings.Contains(result, "## Section C") {
		t.Error("Section C header lost")
	}
}

// TestMemoryCompressorTruncation 测试超限截断
func TestMemoryCompressorTruncation(t *testing.T) {
	mc := &MemoryCompressor{}
	input := strings.Repeat("x", 5000)
	result := mc.CompressMemory(input, 100)

	if len(result) > 100 {
		t.Errorf("Truncation failed: %d > 100", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Error("Truncation should add ellipsis")
	}
}

// TestEmptyMemory 测试空 memory 处理
func TestEmptyMemory(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSnapshotManager(tmpDir)
	sm.Load()

	if sm.GetMemoryForPrompt() != "" {
		t.Error("Empty memory should return empty string")
	}
	if sm.GetUserForPrompt() != "" {
		t.Error("Empty user should return empty string")
	}
}

// TestVersionIncrement 测试版本号递增
func TestVersionIncrement(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSnapshotManager(tmpDir)
	sm.Load()

	v0 := sm.GetVersion()
	sm.RefreshSnapshot()
	v1 := sm.GetVersion()
	sm.RefreshSnapshot()
	v2 := sm.GetVersion()

	if v1 != v0+1 || v2 != v1+1 {
		t.Errorf("Version not sequential: %d, %d, %d", v0, v1, v2)
	}
}
