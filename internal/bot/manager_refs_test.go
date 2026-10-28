package bot

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestRuntimeByRefResolvesMentionTag 是"按 @tag 找不到 bot"的回归：bots 以
// 小写 Name 为 map key，而对外（delegate_task / message_agent / CLI）用
// mention tag 寻址，MentionTag 又会把 "_" 改成 "-"。直接用 tag 查 map 时，
// 名字带下划线的 bot 一律 "bot not found"，尽管工具描述里明明列着这个 tag。
func TestRuntimeByRefResolvesMentionTag(t *testing.T) {
	mgr := newTestManager(t, "research_bot")

	cases := []struct{ ref, wantKey string }{
		{"research_bot", "research_bot"},  // 原名
		{"research-bot", "research_bot"},  // mention tag（下划线已被改写）
		{"@research-bot", "research_bot"}, // 带 @ 的 tag
		{"Research_Bot", "research_bot"},  // 大小写混写
	}
	for _, tc := range cases {
		mgr.mu.Lock()
		rt, key := mgr.runtimeByRefLocked(tc.ref)
		mgr.mu.Unlock()
		if rt == nil || key != tc.wantKey {
			t.Errorf("runtimeByRefLocked(%q) = (%v, %q), want key %q", tc.ref, rt != nil, key, tc.wantKey)
		}
	}

	// 投递面同样要认 tag：这条曾经在 EnqueueMsg 里直接查 map。
	mgr.SetBotActive("research_bot", false) // 暂停，worker 不会真跑 turn
	if err := mgr.EnqueueMsg("research-bot", pendingMessage{Text: "hi", From: "user"}); err != nil {
		t.Fatalf("EnqueueMsg by mention tag: %v", err)
	}

	mgr.mu.Lock()
	_, unknown := mgr.runtimeByRefLocked("@nobody")
	mgr.mu.Unlock()
	if unknown != "" {
		t.Errorf("unknown ref resolved to %q", unknown)
	}
}

// TestDelegateTaskRejectsSelfDelegation：委派给自己会把子回合排进自己的队列，
// 也就是排在"正在等待这个子回合"的那个回合后面 —— 必然超时，白烧整个回合预算。
func TestDelegateTaskRejectsSelfDelegation(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")

	mgr.mu.Lock()
	tool := newDelegateTaskTool(mgr, "alice")
	mgr.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		_, err := tool.Execute(context.Background(), map[string]interface{}{
			"target": "alice",
			"task":   "do something",
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "yourself") {
			t.Fatalf("self-delegation should be refused, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delegate_task blocked on a self-addressed turn instead of refusing it")
	}
}

// TestMessageAgentRejectsSelfMessage：fire-and-forget 的自投会让 bot 回答自己、
// 再回答自己 —— 没有人工介入的无限自我对话。
func TestMessageAgentRejectsSelfMessage(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")

	mgr.mu.Lock()
	tool := newMessageAgentTool(mgr, "alice")
	mgr.mu.Unlock()

	if _, err := tool.Execute(context.Background(), map[string]interface{}{
		"target":  "@alice",
		"message": "note to self",
	}); err == nil || !strings.Contains(err.Error(), "yourself") {
		t.Fatalf("self-addressed message_agent should be refused, got %v", err)
	}

	// 队友仍然可达（投递成功即返回，不等回复）。先把 bob 暂停，否则这条投递
	// 会真的触发一个模型回合，测试就要等网络超时/重试（数百秒）。
	mgr.SetBotActive("bob", false)
	if _, err := tool.Execute(context.Background(), map[string]interface{}{
		"target":  "bob",
		"message": "hello",
	}); err != nil {
		t.Fatalf("message to a teammate failed: %v", err)
	}
}

// TestDeleteBotAnswersQueuedCallers：删除 bot 时排队消息会被丢弃，但每个
// 排队消息可能都有同步调用方阻塞在 replyCh 上（web 聊天、中继 DM、委派、
// 群聊成员回合）。丢弃切片却不回错误会让这些调用方一直等下去 —— 媒体路径
// 曾经没有等待超时，表现为 HTTP 请求挂死。
func TestDeleteBotAnswersQueuedCallers(t *testing.T) {
	mgr := newTestManager(t, "alice")

	replyCh := make(chan turnResult, 1)
	// 直接入队而不 Broadcast：确保消息还在队列里被 DeleteBot 丢弃，而不是
	// 先被 worker 取走。
	mgr.mu.Lock()
	if rt, ok := mgr.bots["alice"]; ok {
		rt.queue = append(rt.queue, pendingMessage{Text: "queued", From: "user", replyCh: replyCh})
	}
	mgr.mu.Unlock()

	if err := mgr.DeleteBot("alice"); err != nil {
		t.Fatalf("DeleteBot: %v", err)
	}

	select {
	case res := <-replyCh:
		if res.Err == nil || !strings.Contains(res.Err.Error(), "deleted") {
			t.Fatalf("queued caller should be answered with a deleted error, got %v", res.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued caller was never answered after DeleteBot (replyCh hung)")
	}
}

// TestRememberMemoryIdempotentAndCapped：同一句话反复记不该无限膨胀 system
// prompt；超过上限要明确拒绝，而不是把 bot 的每轮请求越拖越长。
func TestRememberMemoryIdempotentAndCapped(t *testing.T) {
	mgr := newTestManager(t, "alice")

	const fact = "The user prefers concise replies."
	if err := mgr.RememberMemory("alice", fact); err != nil {
		t.Fatalf("first RememberMemory: %v", err)
	}
	first, err := mgr.GetBot("alice")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(first.Memory, fact) != 1 {
		t.Fatalf("memory should hold the fact once, got %q", first.Memory)
	}

	if err := mgr.RememberMemory("alice", fact); err != nil {
		t.Fatalf("repeated RememberMemory should be a no-op: %v", err)
	}
	second, err := mgr.GetBot("alice")
	if err != nil {
		t.Fatal(err)
	}
	if second.Memory != first.Memory {
		t.Fatalf("duplicate fact changed memory:\n before %q\n after  %q", first.Memory, second.Memory)
	}

	// 撑到上限后必须拒绝。
	big := strings.Repeat("x", maxBotMemoryRunes)
	if err := mgr.RememberMemory("alice", big); err == nil {
		t.Fatal("memory beyond the cap should be refused")
	}
	// 空事实同样拒绝（模型偶尔会传空串）。
	if err := mgr.RememberMemory("alice", "   "); err == nil {
		t.Fatal("blank fact should be refused")
	}
}
