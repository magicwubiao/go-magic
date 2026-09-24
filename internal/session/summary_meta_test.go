package session

// 会话列表的冗余摘要列（title / preview / msg_count / tool_call_count）是
// "用写入时的一次计算，换列表查询永不读 messages 大字段"的优化。
//
// 它引入的不变量有两条，本文件把它们钉住：
//   1. 列里的值必须恒等于 DeriveSessionMeta 对同一份消息的现算结果 ——
//      两处只要漂移，列表页显示的标题/预览就会和真实会话对不上；
//   2. 老库（没有这些列）打开后必须被回填，且回填是幂等的。

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

func TestDeriveSessionMeta(t *testing.T) {
	t.Run("空会话", func(t *testing.T) {
		meta := DeriveSessionMeta(nil)
		if meta.Title != "" || meta.Preview != "" || meta.MessageCount != 0 || meta.ToolCallCount != 0 {
			t.Fatalf("空会话应产出全零摘要，got %+v", meta)
		}
	})

	t.Run("标题取首条 user 消息并截 50 字", func(t *testing.T) {
		long := strings.Repeat("字", 300)
		meta := DeriveSessionMeta([]types.Message{
			{Role: "system", Content: "system 不参与标题"},
			{Role: "user", Content: long},
		})
		if want := strings.Repeat("字", 50) + "..."; meta.Title != want {
			t.Fatalf("标题截断不符：got %q want %q", meta.Title, want)
		}
		if want := strings.Repeat("字", 200) + "..."; meta.Preview != want {
			t.Fatalf("预览截断不符：got %q want %q", meta.Preview, want)
		}
		if meta.MessageCount != 2 {
			t.Fatalf("消息条数应为 2，got %d", meta.MessageCount)
		}
	})

	t.Run("标题跳过空 user 消息", func(t *testing.T) {
		meta := DeriveSessionMeta([]types.Message{
			{Role: "user", Content: "   "},
			{Role: "user", Content: "真正的第一条"},
		})
		if meta.Title != "真正的第一条" {
			t.Fatalf("应跳过分空白的第一条，got %q", meta.Title)
		}
	})

	t.Run("工具调用累加", func(t *testing.T) {
		meta := DeriveSessionMeta([]types.Message{
			{Role: "assistant", Content: "a", ToolCalls: make([]types.ToolCall, 2)},
			{Role: "assistant", Content: "b", ToolCalls: make([]types.ToolCall, 3)},
		})
		if meta.ToolCallCount != 5 {
			t.Fatalf("工具调用数应为 5，got %d", meta.ToolCallCount)
		}
	})
}

func TestSessionSummaryColumnsStayInSync(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	msgs := []types.Message{
		{Role: "user", Content: "帮我看看首屏为什么慢"},
		{Role: "assistant", Content: "先看列表接口", ToolCalls: make([]types.ToolCall, 1)},
	}
	if err := store.SaveSession(ctx, &Session{
		ID: "sync-1", Profile: "default", Platform: "web", Name: "",
		WorkDir: "D:/tmp/ws", WorkDirUserSet: true, Messages: msgs,
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	summaries, total, err := store.ListSessionSummaries(ctx, 20, 0)
	if err != nil {
		t.Fatalf("ListSessionSummaries: %v", err)
	}
	if total != 1 || len(summaries) != 1 {
		t.Fatalf("期望 1 条摘要，got total=%d len=%d", total, len(summaries))
	}
	want := DeriveSessionMeta(msgs)
	got := summaries[0]
	if got.Title != want.Title || got.Preview != want.Preview ||
		got.MessageCount != want.MessageCount || got.ToolCallCount != want.ToolCallCount {
		t.Fatalf("摘要列与现算值不一致\n got=%+v\nwant=%+v", *got, want)
	}
	// 工作目录相关字段也要从轻量列正确带出（dir-groups 依赖它们）
	if got.WorkDir != "D:/tmp/ws" || !got.WorkDirUserSet {
		t.Fatalf("工作目录字段丢失：%+v", *got)
	}

	// 追加消息后再存，摘要必须跟着变（否则列表页会一直显示旧标题/旧条数）
	msgs = append(msgs, types.Message{Role: "user", Content: "再补一条"})
	if err := store.SaveSession(ctx, &Session{
		ID: "sync-1", Profile: "default", Platform: "web",
		WorkDir: "D:/tmp/ws", WorkDirUserSet: true, Messages: msgs,
	}); err != nil {
		t.Fatalf("SaveSession (update): %v", err)
	}
	summaries, _, err = store.ListSessionSummaries(ctx, 20, 0)
	if err != nil {
		t.Fatalf("ListSessionSummaries (update): %v", err)
	}
	if summaries[0].MessageCount != 3 {
		t.Fatalf("更新后消息条数应为 3，got %d", summaries[0].MessageCount)
	}
}

func TestListSessionSummariesPagination(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	for i := 0; i < 7; i++ {
		if err := store.SaveSession(ctx, &Session{
			ID: fmt.Sprintf("s-%d", i), Profile: "default", Platform: "web",
			Messages: []types.Message{{Role: "user", Content: fmt.Sprintf("第 %d 条", i)}},
		}); err != nil {
			t.Fatalf("SaveSession %d: %v", i, err)
		}
	}

	page1, total, err := store.ListSessionSummaries(ctx, 3, 0)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if total != 7 || len(page1) != 3 {
		t.Fatalf("第 1 页期望 3 条 / total 7，got len=%d total=%d", len(page1), total)
	}

	page3, _, err := store.ListSessionSummaries(ctx, 3, 6)
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if len(page3) != 1 {
		t.Fatalf("第 3 页期望 1 条，got %d", len(page3))
	}

	// 越界页返回空，不报错（前端全量补齐时会多算页）
	empty, _, err := store.ListSessionSummaries(ctx, 3, 999)
	if err != nil {
		t.Fatalf("越界页不应报错: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("越界页应为空，got %d", len(empty))
	}
}

// TestInitSchemaBackfillsLegacyDB 模拟"升级前就存在的老库"：
// 建表时没有摘要列，打开时必须自动补齐列并把存量行回填。
func TestInitSchemaBackfillsLegacyDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	// 老版本的 schema：只有 messages，没有 title/preview/msg_count/...
	legacy, err := sql.Open("sqlite", dbPath+"?mode=rwc")
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := legacy.Exec(`
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			name TEXT DEFAULT '',
			profile TEXT NOT NULL,
			platform TEXT NOT NULL,
			model TEXT DEFAULT '',
			workdir TEXT DEFAULT '',
			workdir_user_set INTEGER DEFAULT 0,
			messages TEXT,
			input_tokens INTEGER DEFAULT 0,
			output_tokens INTEGER DEFAULT 0,
			cache_read_tokens INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	_, err = legacy.Exec(
		`INSERT INTO sessions (id, profile, platform, messages) VALUES (?, ?, ?, ?)`,
		"legacy-1", "default", "web",
		`[{"role":"user","content":"老会话的第一句话"},{"role":"assistant","content":"收到"}]`,
	)
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore on legacy db: %v", err)
	}
	defer store.Close()

	summaries, total, err := store.ListSessionSummaries(context.Background(), 20, 0)
	if err != nil {
		t.Fatalf("ListSessionSummaries: %v", err)
	}
	if total != 1 {
		t.Fatalf("期望 1 条，got %d", total)
	}
	if got := summaries[0]; got.Title != "老会话的第一句话" || got.MessageCount != 2 {
		t.Fatalf("存量行未被回填：title=%q msg_count=%d", got.Title, got.MessageCount)
	}
}
