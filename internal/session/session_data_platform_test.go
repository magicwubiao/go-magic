package session

// platform 列的归属：它是会话的**客户端身份**（web / wecom / qq / tui …），
// 不是"用量的来源"。前端侧栏、搜索、"按目录查看会话"面板全靠它区分
// web 会话与各平台 bot 会话：
//
//	chat.ts::loadSessions      → result.sessions.filter(s => !s.source || s.source === 'web')
//	ChatView.vue::isWebSession → !s.source || s.source === 'web'
//	store.go::ListSessionSummariesByUserWorkDir → 只收 web 与空 platform
//
// 而 token 记账的调用方传进来的 platform 是**另一套语义**的值：server 的队列
// 记账路径曾经传"当前 LLM 供应商名"（mimo / huoshan / …）。旧实现把它写进
// saveSessionDataInternal 的 UPDATE，于是每跑完一个回合 web 会话就被改名成
// 供应商名，随即从前端侧栏与目录分组里消失，侧栏里残留的那条条目标题也不再
// 刷新（用户报"chat 页面对话标题名称不更新了"）。
//
// 本文件钉住两条：记账**不得**改已有行的 platform；行不存在时兜底 INSERT 仍要
// 落 platform（网关会在会话行缺失时补建）。

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/pkg/types"
)

func newPlatformTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, context.Background()
}

func TestSaveSessionDataDoesNotClobberPlatform(t *testing.T) {
	store, ctx := newPlatformTestStore(t)
	now := time.Now()

	if err := store.SaveSession(ctx, &Session{
		ID:        "sess-web",
		Profile:   "magic",
		Platform:  "web",
		Messages:  []types.Message{{ID: "m1", Role: "user", Content: "你好"}},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// 模拟 accountTurnUsage 的实参：供应商名（曾经就是它把 platform 覆盖掉的）。
	if err := store.SaveSessionDataFromMap(ctx, "sess-web", "mimo", 100, 50, 7); err != nil {
		t.Fatalf("SaveSessionDataFromMap: %v", err)
	}

	got, err := store.LoadSession(ctx, "sess-web")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got.Platform != "web" {
		t.Fatalf("记账把 platform 改成了 %q —— web 会话会从前端侧栏/目录分组里消失，"+
			"标题也不再刷新；期望仍是 \"web\"", got.Platform)
	}
	// token 必须照常累加（别为了不改 platform 把记账也一起丢了）。
	if got.InputTokens != 100 || got.OutputTokens != 50 || got.CacheReadTokens != 7 {
		t.Fatalf("token 记账结果 = (in=%d, out=%d, cache=%d)，期望 (100, 50, 7)",
			got.InputTokens, got.OutputTokens, got.CacheReadTokens)
	}

	// 第二次记账：继续累加，platform 依旧不动。
	if err := store.SaveSessionDataFromMap(ctx, "sess-web", "huoshan", 10, 5, 0); err != nil {
		t.Fatalf("第二次 SaveSessionDataFromMap: %v", err)
	}
	got2, err := store.LoadSession(ctx, "sess-web")
	if err != nil {
		t.Fatalf("LoadSession(2): %v", err)
	}
	if got2.Platform != "web" {
		t.Fatalf("第二次记账后 platform = %q，期望 \"web\"", got2.Platform)
	}
	if got2.InputTokens != 110 || got2.OutputTokens != 55 || got2.CacheReadTokens != 7 {
		t.Fatalf("第二次记账后 token = (in=%d, out=%d, cache=%d)，期望 (110, 55, 7)",
			got2.InputTokens, got2.OutputTokens, got2.CacheReadTokens)
	}
}

// 行不存在时的兜底 INSERT 仍要落 platform：网关的会话可能先有 token 再建行。
func TestSaveSessionDataInsertFallbackKeepsPlatform(t *testing.T) {
	store, ctx := newPlatformTestStore(t)

	if err := store.SaveSessionDataFromMap(ctx, "sess-wecom", "wecom", 20, 9, 0); err != nil {
		t.Fatalf("SaveSessionDataFromMap(INSERT): %v", err)
	}

	// 直接读列，不走 LoadSession：兜底 INSERT 不写 messages，落库是 NULL，
	// 而 LoadSession 把 messages 扫进 string（既有行为，与本用例无关）。
	var platform string
	var in, out int
	if err := store.db.QueryRowContext(ctx,
		`SELECT platform, input_tokens, output_tokens FROM sessions WHERE id = ?`,
		"sess-wecom").Scan(&platform, &in, &out); err != nil {
		t.Fatalf("查询兜底 INSERT 的行: %v", err)
	}
	if platform != "wecom" {
		t.Fatalf("兜底 INSERT 落库的 platform = %q，期望 \"wecom\"", platform)
	}
	if in != 20 || out != 9 {
		t.Fatalf("兜底 INSERT 的 token = (%d, %d)，期望 (20, 9)", in, out)
	}
}
