package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// newInsertTestStore 建一个独立临时库的 Store。
func newInsertTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// mustStoredMessages 读回会话历史并断言每条消息都是 JSON **对象**——防双重编码
// 回归（json_group_array 不认 JSON subtype 的老版本会把元素写成字符串）。
func mustStoredMessages(t *testing.T, store *Store, id string) []types.Message {
	t.Helper()
	sess, err := store.LoadSession(context.Background(), id)
	if err != nil || sess == nil {
		t.Fatalf("LoadSession(%s): %v (sess=%v)", id, err, sess)
	}
	raw, err := json.Marshal(sess.Messages)
	if err != nil {
		t.Fatalf("marshal stored messages: %v", err)
	}
	var probe []json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("stored messages not an array: %v", err)
	}
	for i, p := range probe {
		var obj map[string]interface{}
		if err := json.Unmarshal(p, &obj); err != nil {
			t.Fatalf("message[%d] is not a JSON object (double-encoded?): %s", i, string(p))
		}
	}
	return sess.Messages
}

// TestInsertSessionMessageBefore 验证核心语义：新消息插到指定 id 的消息之前，
// 其余消息相对顺序不变，且元素仍是 JSON 对象（subtype 探针兜底）。
func TestInsertSessionMessageBefore(t *testing.T) {
	store := newInsertTestStore(t)
	ctx := context.Background()
	const sid = "sess-insert-before"

	// 模拟真实场景：userA 已落库，回合中 checkpoint 已把 assistant 占住尾位，
	// 此时引导（guide）注入，必须排在 assistant 之前而不是 append 到末尾。
	seed := &Session{
		ID: sid,
		Messages: []types.Message{
			{ID: "user-a", Role: "user", Content: "提交代码到仓库"},
			{ID: "assistant-turn1", Role: "assistant", Content: "回答A"},
		},
	}
	if err := store.SaveSession(ctx, seed); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	guide := types.Message{ID: "guide-1", Role: "user", Content: "中文的提交改成英文"}
	ok, err := store.InsertSessionMessageBefore(ctx, sid, "assistant-turn1", guide)
	if err != nil || !ok {
		t.Fatalf("InsertSessionMessageBefore: ok=%v err=%v", ok, err)
	}

	msgs := mustStoredMessages(t, store, sid)
	wantOrder := []string{"user-a", "guide-1", "assistant-turn1"}
	if len(msgs) != len(wantOrder) {
		t.Fatalf("msg count = %d, want %d", len(msgs), len(wantOrder))
	}
	for i, w := range wantOrder {
		if msgs[i].ID != w {
			t.Fatalf("msg[%d].id = %q, want %q (full: %v)", i, msgs[i].ID, w, wantOrder)
		}
	}

	// 幂等性不是本函数的职责（幂等在调用方 persistUserMessage / id 判重），
	// 但重复插入同样不应损坏结构——再插一条到最前面，顺序仍可预期。
	guide2 := types.Message{ID: "guide-2", Role: "user", Content: "第二条"}
	if ok, err := store.InsertSessionMessageBefore(ctx, sid, "user-a", guide2); err != nil || !ok {
		t.Fatalf("insert before head: ok=%v err=%v", ok, err)
	}
	msgs = mustStoredMessages(t, store, sid)
	if len(msgs) != 4 || msgs[0].ID != "guide-2" || msgs[2].ID != "guide-1" {
		t.Fatalf("unexpected order after head insert: %v",
			[]string{msgs[0].ID, msgs[1].ID, msgs[2].ID, msgs[3].ID})
	}
}

// TestInsertSessionMessageBeforeMissingTarget 目标消息不存在时必须返回
// (false, nil)，调用方据此回落 AppendSessionMessages，数据不能丢。
func TestInsertSessionMessageBeforeMissingTarget(t *testing.T) {
	store := newInsertTestStore(t)
	ctx := context.Background()
	const sid = "sess-insert-missing"

	seed := &Session{
		ID:       sid,
		Messages: []types.Message{{ID: "user-a", Role: "user", Content: "hi"}},
	}
	if err := store.SaveSession(ctx, seed); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	ok, err := store.InsertSessionMessageBefore(ctx, sid, "assistant-nope", types.Message{ID: "g", Role: "user", Content: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for missing beforeID")
	}

	// 会话行不存在时同样 (false, nil)。
	ok, err = store.InsertSessionMessageBefore(ctx, "sess-nope", "assistant-nope", types.Message{ID: "g", Role: "user", Content: "x"})
	if err != nil {
		t.Fatalf("unexpected error on missing session: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for missing session row")
	}

	// 原会话未被破坏。
	msgs := mustStoredMessages(t, store, sid)
	if len(msgs) != 1 || msgs[0].ID != "user-a" {
		t.Fatalf("original session corrupted: %d msgs", len(msgs))
	}
}
