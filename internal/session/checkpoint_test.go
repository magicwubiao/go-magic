package session

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/magicwubiao/go-magic/pkg/types"
)

func newTestCheckpointManager(t *testing.T) *CheckpointManager {
	t.Helper()
	t.Setenv("GO_MAGIC_HOME", t.TempDir())
	cm, err := NewCheckpointManager()
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}
	return cm
}

func msg(role, content string) types.Message {
	return types.Message{Role: role, Content: content}
}

func mustLoad(t *testing.T, cm *CheckpointManager, id string) *Checkpoint {
	t.Helper()
	cp, err := cm.Load(id)
	if err != nil {
		t.Fatalf("Load(%s): %v", id, err)
	}
	return cp
}

// TestCheckpointDeltaAppendAndReplay 是本文件的核心回归：常规保存只追加新增消息，
// 但重建出来的历史必须与最后写入的完整历史逐条一致。
func TestCheckpointDeltaAppendAndReplay(t *testing.T) {
	cm := newTestCheckpointManager(t)
	const id = "user-1"

	// 第 1 次保存：完整历史（4 条）——应写 Full 行。
	if err := cm.Save(&Checkpoint{SessionID: id, Platform: "wecom", UserID: id, Messages: []types.Message{
		msg("system", "sys"), msg("user", "u1"), msg("assistant", "a1"), msg("user", "u2"),
	}}); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	path := cm.checkpointPath(id)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after save 1: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(string(first)), "\n"); len(lines) != 1 {
		t.Fatalf("first save must produce exactly 1 line, got %d", len(lines))
	}
	var l0 checkpointFileLine
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(first))), &l0); err != nil {
		t.Fatalf("parse first line: %v", err)
	}
	if !l0.Full || len(l0.Messages) != 4 {
		t.Fatalf("first line must be a Full snapshot with 4 messages, got full=%v n=%d", l0.Full, len(l0.Messages))
	}

	// 第 2 次保存：历史前缀未变，只多了 2 条。
	if err := cm.Save(&Checkpoint{SessionID: id, Platform: "wecom", UserID: id, Messages: []types.Message{
		msg("system", "sys"), msg("user", "u1"), msg("assistant", "a1"), msg("user", "u2"),
		msg("assistant", "a2"), msg("user", "u3"),
	}}); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after save 2: %v", err)
	}
	// 关键：旧字节前缀原封不动 ⇒ 是追加而不是重写。
	if !strings.HasPrefix(string(second), string(first)) {
		t.Fatalf("second save must APPEND, but previous bytes changed:\n before=%q\n after =%q", first, second)
	}
	lines := strings.Split(strings.TrimSpace(string(second)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines after second save, got %d", len(lines))
	}
	var l1 checkpointFileLine
	if err := json.Unmarshal([]byte(lines[1]), &l1); err != nil {
		t.Fatalf("parse second line: %v", err)
	}
	if l1.Full {
		t.Fatalf("second line must be a delta (full=false)")
	}
	// 增量只含新增的两条 —— 这是"每次落盘代价与历史长度解耦"的证据。
	if len(l1.Messages) != 2 || l1.Messages[0].Content != "a2" || l1.Messages[1].Content != "u3" {
		t.Fatalf("delta line must carry only the 2 new messages, got %#v", l1.Messages)
	}

	// 重新加载（新进程视角）：必须重建出完整的 6 条。
	cm2, err := NewCheckpointManager()
	if err != nil {
		t.Fatalf("reopen manager: %v", err)
	}
	cp := mustLoad(t, cm2, id)
	if cp == nil {
		t.Fatalf("checkpoint must exist after reload")
	}
	if len(cp.Messages) != 6 {
		t.Fatalf("replayed history len = %d, want 6", len(cp.Messages))
	}
	if cp.Messages[5].Content != "u3" || cp.Messages[4].Content != "a2" {
		t.Fatalf("replayed tail is wrong: %#v", cp.Messages[4:])
	}
	if cp.Platform != "wecom" || cp.UserID != id {
		t.Fatalf("metadata must survive replay, got platform=%q user=%q", cp.Platform, cp.UserID)
	}
}

// TestCheckpointPrefixChangeFallsBackToFull 覆盖历史被压缩/裁剪的场景：
// 前缀对不上时必须写 Full 行重设基线，否则重放会拼出一段错乱的历史。
func TestCheckpointPrefixChangeFallsBackToFull(t *testing.T) {
	cm := newTestCheckpointManager(t)
	const id = "user-2"

	if err := cm.Save(&Checkpoint{SessionID: id, Messages: []types.Message{
		msg("user", "u1"), msg("assistant", "a1"), msg("user", "u2"),
	}}); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	// 模拟压缩：中段被换成一条摘要 ⇒ 前缀不一致。
	if err := cm.Save(&Checkpoint{SessionID: id, Messages: []types.Message{
		msg("user", "u1"), msg("system", "[CONTEXT COMPACTION] summary"), msg("user", "u2"),
	}}); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	raw, err := os.ReadFile(cm.checkpointPath(id))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	var l1 checkpointFileLine
	if err := json.Unmarshal([]byte(lines[1]), &l1); err != nil {
		t.Fatalf("parse second line: %v", err)
	}
	if !l1.Full {
		t.Fatalf("a changed prefix must be written as a Full line")
	}

	cm2, _ := NewCheckpointManager()
	cp := mustLoad(t, cm2, id)
	if len(cp.Messages) != 3 {
		t.Fatalf("replayed len = %d, want 3", len(cp.Messages))
	}
	if cp.Messages[1].Content != "[CONTEXT COMPACTION] summary" {
		t.Fatalf("replay must reflect the compacted history, got %#v", cp.Messages)
	}
}

// TestCheckpointMarkInterruptedRoundTrip 保证 interrupted 标记在 JSONL 下仍然有效，
// 且标记操作不会丢历史（它写的是"无消息的 delta 行"）。
func TestCheckpointMarkInterruptedRoundTrip(t *testing.T) {
	cm := newTestCheckpointManager(t)
	const id = "user-3"

	if err := cm.Save(&Checkpoint{SessionID: id, Messages: []types.Message{msg("user", "hi")}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := cm.MarkInterrupted(id); err != nil {
		t.Fatalf("MarkInterrupted: %v", err)
	}

	cm2, _ := NewCheckpointManager()
	cp := mustLoad(t, cm2, id)
	if cp == nil || !cp.Interrupted {
		t.Fatalf("interrupted flag must survive JSONL round trip, got %#v", cp)
	}
	if len(cp.Messages) != 1 || cp.Messages[0].Content != "hi" {
		t.Fatalf("marking must not drop history, got %#v", cp.Messages)
	}

	interrupted, err := cm2.ListInterrupted()
	if err != nil {
		t.Fatalf("ListInterrupted: %v", err)
	}
	if len(interrupted) != 1 || interrupted[0].SessionID != id {
		t.Fatalf("ListInterrupted must find the session, got %#v", interrupted)
	}

	if err := cm2.ClearInterrupted(id); err != nil {
		t.Fatalf("ClearInterrupted: %v", err)
	}
	cp2 := mustLoad(t, cm2, id)
	if cp2.Interrupted {
		t.Fatalf("ClearInterrupted must clear the flag")
	}
	if len(cp2.Messages) != 1 {
		t.Fatalf("ClearInterrupted must not drop history, got %#v", cp2.Messages)
	}
}

// TestCheckpointLegacyJSONMigration 覆盖旧格式（整包 JSON 对象）的读取与迁移。
func TestCheckpointLegacyJSONMigration(t *testing.T) {
	cm := newTestCheckpointManager(t)
	const id = "user-4"

	legacy := &Checkpoint{
		SessionID: id,
		Platform:  "wecom",
		Messages:  []types.Message{msg("user", "old"), msg("assistant", "reply")},
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if err := os.WriteFile(cm.legacyCheckpointPath(id), data, 0644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	// 读取：回退到旧格式。
	cp := mustLoad(t, cm, id)
	if cp == nil || len(cp.Messages) != 2 {
		t.Fatalf("legacy checkpoint must load, got %#v", cp)
	}

	// 写入：迁移为 JSONL 并删除旧文件。
	if err := cm.Save(&Checkpoint{SessionID: id, Platform: "wecom", Messages: []types.Message{
		msg("user", "old"), msg("assistant", "reply"), msg("user", "new"),
	}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(cm.legacyCheckpointPath(id)); !os.IsNotExist(err) {
		t.Fatalf("legacy .json must be removed after migration, stat err=%v", err)
	}
	if _, err := os.Stat(cm.checkpointPath(id)); err != nil {
		t.Fatalf(".jsonl must exist after migration: %v", err)
	}

	cm2, _ := NewCheckpointManager()
	cp2 := mustLoad(t, cm2, id)
	if cp2 == nil || len(cp2.Messages) != 3 || cp2.Messages[2].Content != "new" {
		t.Fatalf("migrated checkpoint must replay completely, got %#v", cp2)
	}
}

// TestCheckpointCompactionBounded 保证 delta 行数超限时会被整理成单行 Full，
// 文件不会随保存次数无限增长。
func TestCheckpointCompactionBounded(t *testing.T) {
	cm := newTestCheckpointManager(t)
	const id = "user-5"

	// 把体量门槛调小，让这条路径可被小样本覆盖。
	origBytes := checkpointCompactMinBytes
	checkpointCompactMinBytes = 512
	defer func() { checkpointCompactMinBytes = origBytes }()

	var msgs []types.Message
	for i := 0; i < checkpointCompactMinLines+10; i++ {
		msgs = append(msgs, msg("user", "m"))
		// 每次多追加一条并保存 ⇒ 产生一条 delta 行。
		if err := cm.Save(&Checkpoint{SessionID: id, Messages: append([]types.Message(nil), msgs...)}); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	raw, err := os.ReadFile(cm.checkpointPath(id))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) >= checkpointCompactMinLines {
		t.Fatalf("compaction must bound the line count, got %d lines", len(lines))
	}
	var head checkpointFileLine
	if err := json.Unmarshal([]byte(lines[0]), &head); err != nil {
		t.Fatalf("parse head line: %v", err)
	}
	if !head.Full {
		t.Fatalf("after compaction the head line must be a Full baseline")
	}

	cm2, _ := NewCheckpointManager()
	cp := mustLoad(t, cm2, id)
	if cp == nil || len(cp.Messages) != checkpointCompactMinLines+10 {
		t.Fatalf("compacted checkpoint must still replay every message, got %d", len(cp.Messages))
	}
}

// TestCheckpointToleratesTornLine 追加写被杀进程打断时会留下半行，
// 重放必须跳过它而不是整体失败。
func TestCheckpointToleratesTornLine(t *testing.T) {
	cm := newTestCheckpointManager(t)
	const id = "user-6"

	if err := cm.Save(&Checkpoint{SessionID: id, Messages: []types.Message{msg("user", "ok")}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := cm.checkpointPath(id)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := f.WriteString(`{"full":false,"messages":[{"role":"us`); err != nil {
		t.Fatalf("write torn line: %v", err)
	}
	f.Close()

	cm2, _ := NewCheckpointManager()
	cp := mustLoad(t, cm2, id)
	if cp == nil || len(cp.Messages) != 1 || cp.Messages[0].Content != "ok" {
		t.Fatalf("torn line must be skipped, got %#v", cp)
	}
}

// TestCheckpointPruneRemovesJSONL 保证 Prune 认得新扩展名。
func TestCheckpointPruneRemovesJSONL(t *testing.T) {
	cm := newTestCheckpointManager(t)
	const id = "user-7"

	if err := cm.Save(&Checkpoint{SessionID: id, Messages: []types.Message{msg("user", "x")}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := cm.checkpointPath(id)
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := cm.Prune(7 * 24 * time.Hour); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("prune must remove the .jsonl checkpoint, stat err=%v", err)
	}
	// 缓存也要一并清掉，避免随后的保存把基线当成"已落盘"。
	if _, ok := cm.persisted[id]; ok {
		t.Fatalf("prune must drop the cached baseline for %s", id)
	}
}
