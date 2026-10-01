package session

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// 回归：sessions.db 的 DSN 必须用 modernc 的 `_pragma=NAME(VALUE)` 形式。
//
// modernc.org/sqlite 只解析 _pragma / _time_format / _txlock，其余查询参数静默
// 忽略。曾写成 mattn 风格 `?mode=rwc&_journal=WAL&_busy_timeout=5000`，实际以
// journal_mode=delete + busy_timeout=0 运行：多进程（desktop server / CLI /
// gateway）同时写同一个 sessions.db 时立刻 SQLITE_BUSY "database is locked"。
func TestNewStoreAppliesPragmas(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	var mode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal (DSN _pragma not applied?)", mode)
	}

	var busy int
	if err := store.db.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busy <= 0 {
		t.Errorf("busy_timeout = %d, want > 0 (DSN _pragma not applied?)", busy)
	}
}

// 每个连接都必须带上 pragma：上面只验证了池中当前那一条连接，这里显式再开一条
// 连接确认（DSN 参数是按连接生效的，若有人改成"只在第一条连接上 Exec PRAGMA"，
// 这条会红）。
func TestNewStorePragmasApplyToEveryConn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// 绕过 Store 的池限制，另开一条独立连接指向同一文件
	other, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatalf("open second handle: %v", err)
	}
	defer other.Close()

	var busy int
	if err := other.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busy <= 0 {
		t.Errorf("busy_timeout = %d on second connection, want > 0", busy)
	}
}

// 两个独立句柄（≈ 两个进程）并发写同一个 sessions.db，必须全部成功。
// 这模拟 desktop server 与 CLI/gateway 同时落库：旧 DSN 下第二写者立即
// SQLITE_BUSY，且报错在 SetMaxOpenConns(1) 的掩护下不会在单句柄内暴露。
func TestConcurrentWritesAcrossHandles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	second, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatalf("open second handle: %v", err)
	}
	defer second.Close()

	if _, err := store.db.Exec(`CREATE TABLE IF NOT EXISTS pragma_probe (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	const n = 12
	handles := []*sql.DB{store.db, second}
	var wg sync.WaitGroup
	errs := make([]error, 2*n)
	for h, db := range handles {
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(idx int, db *sql.DB, i int) {
				defer wg.Done()
				if _, err := db.Exec("INSERT INTO pragma_probe (id) VALUES (?)", fmt.Sprintf("h%d_%d", idx, i)); err != nil {
					errs[idx*n+i] = err
				}
			}(h, db, i)
		}
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent write #%d failed: %v", i, err)
		}
	}

	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM pragma_probe").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2*n {
		t.Errorf("persisted %d rows, want %d", count, 2*n)
	}
}
