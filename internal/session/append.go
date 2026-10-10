package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// 本文件是会话持久化的**增量**路径：与 store.go 的 SaveSession（全量重写
// messages 大字段）并存，专供回合内高频追加调用。
//
// 背景：几百轮长任务里每回合走 LoadSession + SaveSession 是 O(n²) —— 第
// 500 轮时每回合要把几 MB 的 messages JSON 读回来再整体写回去，还附带一次
// 全量 DeriveSessionMeta。AppendSessionMessages 把"追加一条消息"降到
// SQL 层单行 UPDATE（json_insert），不读回大字段、不全量重算 meta。

// maxMetaPreviewLen 与 DeriveSessionMeta 的预览截断长度保持一致（200 rune）。
const maxMetaPreviewLen = 200

// maxMetaTitleLen 与 DeriveSessionMeta 的标题截断长度保持一致（50 rune）。
const maxMetaTitleLen = 50

// metaTitleForRunes 把标题按 rune 截断并加省略号（与 DeriveSessionMeta 一致）。
func metaTitleForRunes(s string, limit int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return s
}

// AppendSessionMessages 把一条或多条新消息**增量**追加进会话：不读回、不
// 重写 messages 大字段，只在 SQL 层 json_insert 追加，并轻量更新派生 meta
// 列（标题/预览/计数，语义与 SaveSession 全量路径一致）。
//
// 返回 (false, nil) 表示会话行由调用方兜底（如全量 SaveSession 补建）。
// err 非 nil 时行未被修改。
func (s *Store) AppendSessionMessages(ctx context.Context, id string, msgs ...types.Message) (bool, error) {
	if len(msgs) == 0 {
		return s.touchSessionRow(ctx, id)
	}
	if id == "" {
		return false, fmt.Errorf("session id is empty")
	}

	// 先算好本轮要追加的 JSON 片段与 meta 增量，再进事务，尽量缩小写锁窗口。
	fragments := make([]string, 0, len(msgs))
	var (
		addMsgCount      int
		addToolCallCount int
		previewTail      strings.Builder
		titleSource      string // 本轮第一条非空 user 正文，仅当行内 title 为空时启用
	)
	for _, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			return false, fmt.Errorf("marshal message: %w", err)
		}
		fragments = append(fragments, string(b))
		addMsgCount++
		addToolCallCount += len(m.ToolCalls)
		if m.Role == "user" || m.Role == "assistant" {
			// 与 DeriveSessionMeta 一致：预览只累计 user/assistant 正文。
			// 这里只带本轮新增文本，SQL 侧做 substr 截断。
			previewTail.WriteString(m.Content)
			previewTail.WriteString(" ")
		}
		if titleSource == "" && m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			titleSource = m.Content
		}
	}
	title := metaTitleForRunes(titleSource, maxMetaTitleLen)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	// json_insert 的第 3+ 参数是成对的 (path, value)：$[#] 表示"追加到数组
	// 末尾"。多对参数一次 UPDATE 全部追加，失败则整体回滚。
	// 两个关键点：
	//   1. value 必须包 json(?)：json_insert 对 TEXT 参数按**字符串标量**
	//      插入（数组会变成 ["{\"id\":...}"] 双重编码），json() 先解析成
	//      JSON 值再插入，落库形态与 SaveSession 全量路径一致。
	//   2. 根必须归一化成数组：兜底两种脏值——① 兜底 INSERT 留下的
	//      messages=NULL；② SaveSession 对 nil Messages 写入的 JSON 字面量
	//      'null'（json.Marshal(nil slice) = "null"，COALESCE 兜不住它）。
	//      json_type 对两者都返回非 'array'，CASE 统一归一化为 '[]'，
	//      否则 json_insert 对非数组根直接报 SQL 错误。
	update := `UPDATE sessions SET
		messages = json_insert(CASE WHEN json_type(messages) = 'array' THEN messages ELSE '[]' END` + strings.Repeat(`, '$[#]', json(?)`, len(fragments)) + `),
		msg_count = msg_count + ?,
		tool_call_count = tool_call_count + ?,
		preview = substr(COALESCE(preview, '') || ?, 1, ?),
		title = CASE WHEN title IS NULL OR title = '' THEN substr(?, 1, ?) ELSE title END,
		updated_at = CURRENT_TIMESTAMP
	WHERE id = ?`

	args := make([]interface{}, 0, len(fragments)+7)
	for _, f := range fragments {
		args = append(args, f)
	}
	args = append(args, addMsgCount, addToolCallCount, previewTail.String(), maxMetaPreviewLen)
	// 标题取第一条非空 user 消息（与 DeriveSessionMeta 一致，rune 截断 +
	// "..."）。本轮追加里若有 user 消息且行内 title 为空，就用它补标题；
	// 否则传空串，CASE 分支不生效。
	args = append(args, title, maxMetaTitleLen, id)

	res, err := tx.ExecContext(ctx, update, args...)
	if err != nil {
		return false, fmt.Errorf("append session messages: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// SessionMessageIDExists 判断会话历史里是否已存在指定 id 的消息（persistUserMessage
// 的幂等判重用）。json_each 在 SQLite 内部扫描，不把 messages 大字段传回 Go
// 内存——比 LoadSession 全量读回便宜一个数量级。
func (s *Store) SessionMessageIDExists(ctx context.Context, id, msgID string) (bool, error) {
	if id == "" || msgID == "" {
		return false, nil
	}
	const q = `SELECT EXISTS(
		SELECT 1 FROM json_each(
			CASE WHEN json_type((SELECT messages FROM sessions WHERE id = ?)) = 'array'
			     THEN (SELECT messages FROM sessions WHERE id = ?)
			     ELSE '[]' END
		)
		WHERE json_extract(value, '$.id') = ?
	)`
	var exists bool
	err := s.db.QueryRowContext(ctx, q, id, id, msgID).Scan(&exists)
	if err != nil {
		// 行不存在时子查询为 NULL，json_each(NULL) 报错 —— 视为不存在。
		return false, nil
	}
	return exists, nil
}

// touchSessionRow 仅刷新 updated_at（消息为空时的轻量路径）。行不存在返回
// false, nil。
func (s *Store) touchSessionRow(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, fmt.Errorf("session id is empty")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
