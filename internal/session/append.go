package session

import (
	"context"
	"database/sql"
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

// InsertSessionMessageBefore 把一条消息插到会话历史中指定 id 的消息**之前**，
// 并轻量更新派生 meta（msg_count/preview/updated_at，语义与 Append 一致）。
//
// 为什么需要它：回合中的 assistant 回复有 3 秒一次的 checkpoint
// （UpsertSessionMessage upsert assistant_<turnId>），它先于注入发生时已
// 占住数组尾位；此后落库的引导（guide）若走 AppendSessionMessages 只能排
// 到回复后面——刷新后对话顺序错乱（2026-11-24：引导在对话里浮在回答上方，
// 刷新后却掉到回答下面）。引导在语义上属于"本回合的输入"，必须排在本回合
// 回复之前。
//
// 实现：SQLite 的 json_insert 在已占用的数组下标上是 no-op（只会在"越界
// 下标"处追加，2026-11-24 实测），没法原地插位；这里用 json_group_array
// 按"前段 + 新消息 + 后段"重建数组。引导注入是用户触发、极低频的事件，
// 这个重建成本可以接受；回合 delta 走的 Append/Upsert 路径不受影响。
//
// 返回 (false, nil) 表示会话行不存在**或 beforeID 未找到**——调用方应回落
// AppendSessionMessages。典型回落场景：本回合还没有 checkpoint 落库（头
// 3 秒），append 之后首次 checkpoint 会排在引导之后，顺序同样正确。
// err 非 nil 时行未被修改。
func (s *Store) InsertSessionMessageBefore(ctx context.Context, id, beforeID string, msg types.Message) (bool, error) {
	if id == "" || beforeID == "" {
		return false, fmt.Errorf("session/before id is empty")
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return false, fmt.Errorf("marshal message: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var idx int
	err = tx.QueryRowContext(ctx,
		`SELECT je.key FROM sessions, json_each(sessions.messages) je
		 WHERE sessions.id = ? AND json_extract(je.value, '$.id') = ? LIMIT 1`,
		id, beforeID).Scan(&idx)
	if err == sql.ErrNoRows {
		// 目标消息不在历史里（本回合还没有 checkpoint / id 打错）。
		return false, nil
	}
	if err != nil {
		// 行不存在时 json_each(NULL) 扫描报错 —— 同样回落 append。
		return false, nil
	}

	// 探针自检：json_group_array 认 JSON subtype（json() 的返回值按 JSON 值
	// 而不是字符串入组）是 SQLite 3.45.0 起的行为。老版本会把每个元素双重
	// 编码成字符串，整段 messages 就写坏了——那种环境宁可回落 append
	// （顺序差一点，数据不能坏）。探针是纯内存表达式，每事务一次成本可忽略。
	var probe string
	err = tx.QueryRowContext(ctx,
		`SELECT json_type(json_group_array(json(value)), '$[0].a')
		 FROM (SELECT json('{"a":1}') AS value)`).Scan(&probe)
	if err != nil || probe != "integer" {
		return false, nil
	}

	// meta 增量与 AppendSessionMessages 保持同一口径。
	previewTail := ""
	if msg.Role == "user" || msg.Role == "assistant" {
		previewTail = msg.Content + " "
	}
	title := ""
	if msg.Role == "user" && strings.TrimSpace(msg.Content) != "" {
		title = metaTitleForRunes(msg.Content, maxMetaTitleLen)
	}

	// 三段重建：key < idx 的原样保留，新消息插在 idx 位，key >= idx 的右移。
	// json_each 对 NULL/非数组根不产出任何行，json_group_array 返回 NULL，
	// COALESCE 兜住保持原值（虽然能走到这里 messages 必是数组，防御性保留）。
	// 聚合时逐元素套 json()：UNION 子查询会丢掉 JSON subtype 标记，不补的话
	// json_group_array 把元素当字符串入组（双重编码），见上面的探针自检。
	update := `UPDATE sessions SET
		messages = COALESCE((
			SELECT json_group_array(json(seg.value)) FROM (
				SELECT je1.value AS value, 0 AS seg, je1.key AS ord
				  FROM json_each((SELECT messages FROM sessions WHERE id = ?)) je1
				  WHERE je1.key < ?
				UNION ALL
				SELECT json(?), 1, ?
				UNION ALL
				SELECT je1.value, 2, je1.key
				  FROM json_each((SELECT messages FROM sessions WHERE id = ?)) je1
				  WHERE je1.key >= ?
			) seg
		), messages),
		msg_count = msg_count + 1,
		preview = substr(COALESCE(preview, '') || ?, 1, ?),
		title = CASE WHEN title IS NULL OR title = '' THEN substr(?, 1, ?) ELSE title END,
		updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`

	args := []interface{}{id, idx, string(b), idx, id, idx, previewTail, maxMetaPreviewLen, title, maxMetaTitleLen, id}
	res, err := tx.ExecContext(ctx, update, args...)
	if err != nil {
		return false, fmt.Errorf("insert session message before: %w", err)
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

// UpsertSessionMessage updates a stable turn checkpoint without adding duplicate
// assistant messages. JSON is patched in SQL so concurrent guide appends survive.
func (s *Store) UpsertSessionMessage(ctx context.Context, id string, msg types.Message) (bool, error) {
	if id == "" || msg.ID == "" {
		return false, fmt.Errorf("session/message id is empty")
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var path string
	err = tx.QueryRowContext(ctx, `SELECT '$[' || key || ']' FROM sessions, json_each(sessions.messages)
		WHERE sessions.id = ? AND json_extract(value, '$.id') = ? LIMIT 1`, id, msg.ID).Scan(&path)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	var res sql.Result
	if err == sql.ErrNoRows {
		res, err = tx.ExecContext(ctx, `UPDATE sessions SET messages = json_insert(
			CASE WHEN json_type(messages) = 'array' THEN messages ELSE '[]' END, '$[#]', json(?)),
			msg_count = msg_count + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, string(b), id)
	} else {
		res, err = tx.ExecContext(ctx, `UPDATE sessions SET messages = json_set(messages, ?, json(?)),
			updated_at = CURRENT_TIMESTAMP WHERE id = ?`, path, string(b), id)
	}
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}
