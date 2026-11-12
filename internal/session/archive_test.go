package session

// 归档是"软隐藏"：不删除任何数据、不动工作目录，只在列表查询里按
// archived 列过滤。本文件钉住三条不变量：
//   1. 默认列表（archived=0）不含已归档会话，已归档列表（archived=1）恰好相反；
//   2. total 与过滤条件同源 —— 前端按它判断 hasMore，两者错位就会多拉/少拉；
//   3. SaveSession 整行覆写（每回合落库都会走）不得把 archived 冲回 0，
//      否则归档的会话在下一轮对话后偷偷回到侧栏。

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

func TestSessionArchive(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	mkSession := func(id string) *Session {
		return &Session{
			ID:       id,
			Platform: "web",
			Messages: []types.Message{{Role: "user", Content: "hello " + id}},
		}
	}
	for _, id := range []string{"s1", "s2", "s3"} {
		if err := store.SaveSession(ctx, mkSession(id)); err != nil {
			t.Fatalf("SaveSession(%s): %v", id, err)
		}
	}

	// 批量归档 s1、s2
	affected, err := store.SetSessionsArchived(ctx, []string{"s1", "s2"}, true)
	if err != nil {
		t.Fatalf("SetSessionsArchived: %v", err)
	}
	if affected != 2 {
		t.Fatalf("应更新 2 行，got %d", affected)
	}

	// 默认列表（archived=0）：只剩 s3，total 同源
	list, total, err := store.ListSessionSummaries(ctx, 20, 0, 0)
	if err != nil {
		t.Fatalf("ListSessionSummaries(0): %v", err)
	}
	if len(list) != 1 || list[0].ID != "s3" {
		t.Fatalf("未归档列表应只剩 s3，got %+v", list)
	}
	if total != 1 {
		t.Fatalf("未归档 total 应为 1，got %d", total)
	}

	// 已归档列表（archived=1）：s1、s2
	archived, total, err := store.ListSessionSummaries(ctx, 20, 0, 1)
	if err != nil {
		t.Fatalf("ListSessionSummaries(1): %v", err)
	}
	if len(archived) != 2 || total != 2 {
		t.Fatalf("已归档列表应为 2 条（total=2），got %d 条 total=%d", len(archived), total)
	}

	// 全量（archived=-1）：3 条
	_, total, err = store.ListSessionSummaries(ctx, 20, 0, -1)
	if err != nil {
		t.Fatalf("ListSessionSummaries(-1): %v", err)
	}
	if total != 3 {
		t.Fatalf("全量 total 应为 3，got %d", total)
	}

	// 关键回归：回合落库走 SaveSession 整行覆写。LoadSession 读出的 Archived
	// 必须随行带回，覆写后归档标记不能丢。
	sess, err := store.LoadSession(ctx, "s1")
	if err != nil {
		t.Fatalf("LoadSession(s1): %v", err)
	}
	if !sess.Archived {
		t.Fatal("LoadSession 应带回 archived=true")
	}
	sess.Messages = append(sess.Messages, types.Message{Role: "assistant", Content: "reply"})
	if err := store.SaveSession(ctx, sess); err != nil {
		t.Fatalf("SaveSession(s1 round-trip): %v", err)
	}
	still, total, err := store.ListSessionSummaries(ctx, 20, 0, 1)
	if err != nil {
		t.Fatalf("ListSessionSummaries(1) after save: %v", err)
	}
	found := false
	for _, sm := range still {
		if sm.ID == "s1" {
			found = true
		}
	}
	if !found || total != 2 {
		t.Fatalf("SaveSession 覆写后 s1 仍应留在已归档列表（total=2），got total=%d", total)
	}

	// 单条取消归档
	if err := store.SetSessionArchived(ctx, "s1", false); err != nil {
		t.Fatalf("SetSessionArchived(s1,false): %v", err)
	}
	list, _, err = store.ListSessionSummaries(ctx, 20, 0, 0)
	if err != nil {
		t.Fatalf("ListSessionSummaries(0): %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("取消归档后未归档列表应为 2 条，got %d", len(list))
	}

	// 不存在的会话要报错，不能静默成功
	if err := store.SetSessionArchived(ctx, "missing", true); err == nil {
		t.Fatal("对不存在的会话归档应返回错误")
	}

	// 空批量是合法 no-op
	affected, err = store.SetSessionsArchived(ctx, nil, true)
	if err != nil || affected != 0 {
		t.Fatalf("空批量应为 no-op，got affected=%d err=%v", affected, err)
	}
}
