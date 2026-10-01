package kanban

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// 回归：对话中一次并行创建多个 kanban 任务。
//
// 曾因 modernc.org/sqlite 忽略 mattn 风格 DSN 参数（_journal_mode/_busy_timeout），
// 实际以 journal_mode=delete + busy_timeout=0 运行，并行 INSERT 全部报
// SQLITE_BUSY "database is locked"：前端显示任务已创建，数据库却没有记录。
func TestNewKanbanDBAppliesPragmas(t *testing.T) {
	kdb := newTestDB(t)
	defer kdb.Close()

	var mode string
	if err := kdb.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal (DSN _pragma not applied?)", mode)
	}

	var busy int
	if err := kdb.db.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busy <= 0 {
		t.Errorf("busy_timeout = %d, want > 0 (DSN _pragma not applied?)", busy)
	}
}

// 并行 CreateTask 必须全部落库：模拟同一回合 LLM 吐出多个 kanban_create
// 且 agent 并行执行工具（kanban_* 不在 sequentialTools 白名单）的场景。
func TestConcurrentCreateTaskAllPersist(t *testing.T) {
	kdb := newTestDB(t)
	defer kdb.Close()

	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task := &Task{ID: generateID("task"), Title: fmt.Sprintf("task %d", i)}
			if err := kdb.CreateTask(task); err != nil {
				errs[i] = err
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("parallel create #%d failed: %v", i, err)
		}
	}

	tasks, err := kdb.ListTasks(TaskFilter{})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != n {
		t.Errorf("persisted %d tasks, want %d", len(tasks), n)
	}
}

// 并行 generateID 不得产生重复 ID（Windows 时间戳粒度 + 主键约束）。
func TestGenerateIDUniqueUnderConcurrency(t *testing.T) {
	const n = 64
	var mu sync.Mutex
	seen := make(map[string]bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := generateID("task")
			mu.Lock()
			defer mu.Unlock()
			if seen[id] {
				t.Errorf("duplicate id: %s", id)
			}
			seen[id] = true
		}()
	}
	wg.Wait()
}

func newTestDB(t *testing.T) *KanbanDB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kanban.db")
	kdb, err := NewKanbanDB(path)
	if err != nil {
		t.Fatalf("NewKanbanDB: %v", err)
	}
	if err := kdb.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { kdb.Close() })
	return kdb
}
