package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// SessionData represents minimal session data for Gateway analytics
type SessionData struct {
	ID              string    `json:"id"`
	Platform        string    `json:"platform"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	CacheReadTokens int       `json:"cache_read_tokens"`
	CreatedAt       time.Time `json:"created_at"`
	LastActive      time.Time `json:"last_active"`
}

type Store struct {
	db *sql.DB
}

type Session struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Profile         string          `json:"profile"`
	Platform        string          `json:"platform"`
	Model           string          `json:"model"`
	WorkDir         string          `json:"work_dir"`
	WorkDirUserSet  bool            `json:"work_dir_user_set"`
	WorkDirChosen   bool            `json:"work_dir_chosen"`
	PlanMode        bool            `json:"plan_mode"`
	// Archived 为 true 表示会话被用户归档：不出现在默认会话列表（GET /sessions）
	// 里，只在"已归档"视图（?archived=1）展示。归档不删除数据，随时可取消。
	Archived        bool            `json:"archived"`
	Messages        []types.Message `json:"messages"`
	InputTokens     int             `json:"input_tokens"`
	OutputTokens    int             `json:"output_tokens"`
	CacheReadTokens int             `json:"cache_read_tokens"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// SessionMeta 是从消息正文派生的会话摘要字段，列表页渲染只需要这几个。
type SessionMeta struct {
	Title         string
	Preview       string
	MessageCount  int
	ToolCallCount int
}

// metaVersion 标记冗余摘要列的生成规则版本。派生规则变化时递增，
// 启动迁移会把 meta_version 落后的行重算一遍（见 backfillSessionMeta）。
const metaVersion = 1

// DeriveSessionMeta 复刻列表转换里的派生规则：标题取第一条非空 user 消息
// （截 50 字），预览取 user/assistant 正文依次拼接（截 200 字）。
//
// 落库（SaveSession）与 API 转换（convertDBSessionToAPI）共用它，
// 避免"写进列的值"和"从消息现算的值"两套规则慢慢漂移。
func DeriveSessionMeta(messages []types.Message) SessionMeta {
	var meta SessionMeta
	meta.MessageCount = len(messages)
	var preview string
	for _, m := range messages {
		if m.Role == "user" || m.Role == "assistant" {
			if len(preview) < 200 {
				preview += m.Content + " "
			}
		}
		if len(m.ToolCalls) > 0 {
			meta.ToolCallCount += len(m.ToolCalls)
		}
		if meta.Title == "" && m.Role == "user" && m.Content != "" {
			meta.Title = strings.TrimSpace(m.Content)
			runes := []rune(meta.Title)
			if len(runes) > 50 {
				meta.Title = string(runes[:50]) + "..."
			}
		}
	}
	preview = strings.TrimSpace(preview)
	if runes := []rune(preview); len(runes) > 200 {
		preview = string(runes[:200]) + "..."
	}
	meta.Preview = preview
	return meta
}

// SessionSummary 是会话列表的轻量形态：不含消息正文，只带列表渲染需要的字段。
// 列表接口一律用它，避免为了渲染 20 行而把整个会话库的消息读进内存。
type SessionSummary struct {
	ID              string
	Name            string
	Profile         string
	Platform        string
	Model           string
	WorkDir         string
	WorkDirUserSet  bool
	WorkDirChosen   bool
	Archived        bool
	Title           string
	Preview         string
	MessageCount    int
	ToolCallCount   int
	InputTokens     int
	OutputTokens    int
	CacheReadTokens int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// sessionSummaryColumns 是 SessionSummary 的列清单，必须与 scanSessionSummaries 的扫描顺序一致。
const sessionSummaryColumns = `id, name, profile, platform, model, workdir, workdir_user_set, workdir_chosen, archived, title, preview, msg_count, tool_call_count, input_tokens, output_tokens, cache_read_tokens, created_at, updated_at`

func scanSessionSummaries(rows *sql.Rows) ([]*SessionSummary, error) {
	var out []*SessionSummary
	for rows.Next() {
		var sm SessionSummary
		var workDirUserSet, workDirChosen, archived int
		if err := rows.Scan(
			&sm.ID, &sm.Name, &sm.Profile, &sm.Platform, &sm.Model, &sm.WorkDir, &workDirUserSet, &workDirChosen, &archived,
			&sm.Title, &sm.Preview, &sm.MessageCount, &sm.ToolCallCount,
			&sm.InputTokens, &sm.OutputTokens, &sm.CacheReadTokens, &sm.CreatedAt, &sm.UpdatedAt,
		); err != nil {
			return nil, err
		}
		sm.WorkDirUserSet = workDirUserSet != 0
		sm.WorkDirChosen = workDirChosen != 0
		sm.Archived = archived != 0
		out = append(out, &sm)
	}
	return out, rows.Err()
}

// ListSessionSummaries 返回一页会话摘要，以及（同过滤条件下）的会话总数。
//
// 与 ListSessions 的关键差别：不读 messages 大字段，且 LIMIT/OFFSET 下推到 SQL。
// 因此单次请求的开销与会话库总量、单会话消息长度都无关（索引 idx_sessions_platform_updated
// 覆盖排序），彻底消除"取 20 条却付出全表代价"的问题。
//
// archived 过滤：0 = 只看未归档（默认侧栏）、1 = 只看已归档、-1 = 全部。
// COUNT 与 SELECT 必须用同一个条件，否则前端按 total 判断 hasMore 会错位。
func (s *Store) ListSessionSummaries(ctx context.Context, limit, offset, archived int) ([]*SessionSummary, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE (? < 0 OR archived = ?)`,
		archived, archived).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sessionSummaryColumns+` FROM sessions WHERE (? < 0 OR archived = ?) ORDER BY updated_at DESC LIMIT ? OFFSET ?`,
		archived, archived, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list, err := scanSessionSummaries(rows)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// CountSessions 返回会话总数与消息总数。
//
// 供 /api/status、/api/system/stats 这类"只要两个数字"的接口使用：它们原先
// 走 ListSessions（读全表、含每个会话的全部消息）只为取 len，会话一多同样是
// 秒级开销，而 /api/status 还可能被前端轮询。
func (s *Store) CountSessions(ctx context.Context) (int, int, error) {
	var sessions, messages int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(msg_count), 0) FROM sessions`).Scan(&sessions, &messages)
	if err != nil {
		return 0, 0, err
	}
	return sessions, messages, nil
}

// ListSessionSummariesByUserWorkDir 是 ListSessionsByUserWorkDir 的轻量版本：
// 同样的筛选条件（用户在新建时选定过工作目录的 web 会话），但不读消息正文。
// 已归档的会话不进目录分组面板（它是默认侧栏的扩展视图，归档即从默认视图消失）。
func (s *Store) ListSessionSummariesByUserWorkDir(ctx context.Context) ([]*SessionSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+sessionSummaryColumns+`
		FROM sessions
		WHERE (platform = '' OR platform = 'web') AND workdir != '' AND workdir_chosen = 1 AND archived = 0
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSessionSummaries(rows)
}

// GetSessionSummary 返回单个会话的轻量摘要（不含消息正文）。
//
// 用途：新建/切回某个会话后要补一次"这一条"的最新元信息（标题、消息数、
// workdir_chosen 等）。走 ListSessions 会把全表连同每个会话的消息一起读出来，
// 只为一个字段付这份代价不划算（见 CountSessions 的同类说明）。
// 第二个返回值为 false 表示会话不存在。
func (s *Store) GetSessionSummary(ctx context.Context, id string) (*SessionSummary, bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sessionSummaryColumns+` FROM sessions WHERE id = ?`, id)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	list, err := scanSessionSummaries(rows)
	if err != nil {
		return nil, false, err
	}
	if len(list) == 0 {
		return nil, false, nil
	}
	return list[0], true, nil
}

func NewStore(dbPath string) (*Store, error) {
	// DSN 只能写 modernc 的 `_pragma=NAME(VALUE)` 形式：该驱动只解析
	// _pragma / _time_format / _txlock，其余查询参数一律静默忽略（包括 mattn
	// 风格的 `_journal=WAL`、`_busy_timeout=5000`，以及 `mode=rwc`——打开标志
	// 硬编码为 READWRITE|CREATE）。
	// 原写法 `?mode=rwc&_journal=WAL&_busy_timeout=5000` 实际以
	// journal_mode=delete + busy_timeout=0 运行：desktop server / CLI chat /
	// sessions / gateway 多进程同时写同一个 sessions.db 时会立刻报
	// SQLITE_BUSY "database is locked"（与 kanban 10-01 事故同源）。
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 进程内仍用单连接串行化；跨进程的写冲突由上面的 WAL + busy_timeout 兜住。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	if err := initSchema(db); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &Store{db: db}, nil
}

func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		name TEXT DEFAULT '',
		profile TEXT NOT NULL,
		platform TEXT NOT NULL,
		model TEXT DEFAULT '',
		workdir TEXT DEFAULT '',
		messages TEXT,
		title TEXT DEFAULT '',
		preview TEXT DEFAULT '',
		msg_count INTEGER DEFAULT 0,
		tool_call_count INTEGER DEFAULT 0,
		meta_version INTEGER DEFAULT 0,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		cache_read_tokens INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_profile ON sessions(profile);
	CREATE INDEX IF NOT EXISTS idx_sessions_platform ON sessions(platform);
	CREATE INDEX IF NOT EXISTS idx_sessions_platform_updated ON sessions(platform, updated_at DESC);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	if err := addNameColumnIfNotExists(db); err != nil {
		return err
	}
	if err := addWorkDirColumnIfNotExists(db); err != nil {
		return err
	}
	if err := addWorkDirUserSetColumnIfNotExists(db); err != nil {
		return err
	}
	if err := addWorkDirChosenColumnIfNotExists(db); err != nil {
		return err
	}
	if err := addPlanModeColumnIfNotExists(db); err != nil {
		return err
	}
	if err := addSessionMetaColumnsIfNotExists(db); err != nil {
		return err
	}
	if err := addArchivedColumnIfNotExists(db); err != nil {
		return err
	}
	// 存量行没有冗余摘要，回填一次（仅处理 meta_version 落后的行，之后的启动是零成本）。
	return backfillSessionMeta(context.Background(), db)
}

// addSessionMetaColumnsIfNotExists 补齐列表页要用的冗余摘要列。
//
// 为什么要有这些列：列表接口原先 SELECT 出 messages 再在内存里切片分页，
// 于是"取 20 条"也要把全部会话的全部消息读盘并 json.Unmarshal 一遍，
// 再整表遍历一次算标题/预览/条数。会话越多、单会话消息越长，首屏越慢。
// 把这三个派生字段在写入时算好落成列，列表查询就只剩一次 LIMIT 扫描。
func addSessionMetaColumnsIfNotExists(db *sql.DB) error {
	columns := []struct {
		name string
		ddl  string
	}{
		{"title", `ALTER TABLE sessions ADD COLUMN title TEXT DEFAULT ''`},
		{"preview", `ALTER TABLE sessions ADD COLUMN preview TEXT DEFAULT ''`},
		{"msg_count", `ALTER TABLE sessions ADD COLUMN msg_count INTEGER DEFAULT 0`},
		{"tool_call_count", `ALTER TABLE sessions ADD COLUMN tool_call_count INTEGER DEFAULT 0`},
		{"meta_version", `ALTER TABLE sessions ADD COLUMN meta_version INTEGER DEFAULT 0`},
	}
	for _, col := range columns {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pragma_table_info('sessions') WHERE name = ?)`, col.name).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if _, err := db.Exec(col.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

// backfillSessionMeta 按批回填存量会话的冗余摘要列。
//
// 必须先把一批 rows 读完再写：连接池被限制成单连接（见 NewStore），
// 在 rows 未关闭时执行 UPDATE 会自锁。批与批之间用事务包住，
// 避免 sqlite 对每条 UPDATE 各做一次提交。
func backfillSessionMeta(ctx context.Context, db *sql.DB) error {
	const batchSize = 200
	for {
		rows, err := db.QueryContext(ctx, `SELECT id, messages FROM sessions WHERE meta_version < ? LIMIT ?`, metaVersion, batchSize)
		if err != nil {
			return err
		}
		type pendingMeta struct {
			id   string
			meta SessionMeta
		}
		var pending []pendingMeta
		for rows.Next() {
			var id string
			var messagesStr sql.NullString
			if err := rows.Scan(&id, &messagesStr); err != nil {
				rows.Close()
				return err
			}
			var messages []types.Message
			if messagesStr.Valid && messagesStr.String != "" {
				// 解析失败不阻断迁移：该行按空会话落摘要，避免每次启动重复重试。
				_ = json.Unmarshal([]byte(messagesStr.String), &messages)
			}
			pending = append(pending, pendingMeta{id: id, meta: DeriveSessionMeta(messages)})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		if len(pending) == 0 {
			return nil
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, p := range pending {
			if _, err := tx.ExecContext(ctx,
				`UPDATE sessions SET title = ?, preview = ?, msg_count = ?, tool_call_count = ?, meta_version = ? WHERE id = ?`,
				p.meta.Title, p.meta.Preview, p.meta.MessageCount, p.meta.ToolCallCount, metaVersion, p.id,
			); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}

		if len(pending) < batchSize {
			return nil
		}
	}
}

func addWorkDirColumnIfNotExists(db *sql.DB) error {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM pragma_table_info('sessions') WHERE name = 'workdir')`
	err := db.QueryRow(query).Scan(&exists)
	if err != nil {
		return err
	}

	if !exists {
		_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN workdir TEXT DEFAULT ''`)
		if err != nil {
			return err
		}
	}

	return nil
}

func addNameColumnIfNotExists(db *sql.DB) error {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM pragma_table_info('sessions') WHERE name = 'name')`
	err := db.QueryRow(query).Scan(&exists)
	if err != nil {
		return err
	}

	if !exists {
		_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN name TEXT DEFAULT ''`)
		if err != nil {
			return err
		}
	}

	return nil
}

func addWorkDirUserSetColumnIfNotExists(db *sql.DB) error {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM pragma_table_info('sessions') WHERE name = 'workdir_user_set')`
	err := db.QueryRow(query).Scan(&exists)
	if err != nil {
		return err
	}

	if !exists {
		_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN workdir_user_set INTEGER DEFAULT 0`)
		if err != nil {
			return err
		}
	}

	return nil
}

// addPlanModeColumnIfNotExists 补齐 plan_mode 列（会话级规划模式开关持久化）。
// addWorkDirChosenColumnIfNotExists 补齐"会话目录是否由用户选定"的标记列。
//
// 与 workdir_user_set 的区别：user_set 是**权限/语义**标记（工具沙箱边界、
// 目录级记忆 scope、删除会话时是否保留目录），在"新建时选目录"的流程里必须
// 对用户选定的目录成立；chosen 是**来源**标记（用户选的 vs 系统按会话分配的
// 默认目录），只用于侧栏分组与界面展示。两者在新建路径上同时为真，老数据里
// user_set=1 的会话也一并视为 chosen（它们确实是人选的/人改的）。
func addWorkDirChosenColumnIfNotExists(db *sql.DB) error {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM pragma_table_info('sessions') WHERE name = 'workdir_chosen')`
	err := db.QueryRow(query).Scan(&exists)
	if err != nil {
		return err
	}

	if !exists {
		_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN workdir_chosen INTEGER DEFAULT 0`)
		if err != nil {
			return err
		}
		// 存量回填：旧版里"用户设置过目录"的会话，在语义上就是"用户选定过的目录"。
		if _, err = db.Exec(`UPDATE sessions SET workdir_chosen = 1 WHERE workdir_user_set = 1`); err != nil {
			return err
		}
	}

	return nil
}

func addPlanModeColumnIfNotExists(db *sql.DB) error {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM pragma_table_info('sessions') WHERE name = 'plan_mode')`
	if err := db.QueryRow(query).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		_, err := db.Exec(`ALTER TABLE sessions ADD COLUMN plan_mode INTEGER DEFAULT 0`)
		if err != nil {
			return err
		}
	}
	return nil
}

// addArchivedColumnIfNotExists 补齐会话归档标记列。
// 归档是软隐藏：不删数据、不清工作目录，随时可在"已归档"视图里取消。
func addArchivedColumnIfNotExists(db *sql.DB) error {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM pragma_table_info('sessions') WHERE name = 'archived')`
	if err := db.QueryRow(query).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		_, err := db.Exec(`ALTER TABLE sessions ADD COLUMN archived INTEGER DEFAULT 0`)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveSession(ctx context.Context, session *Session) error {
	messages, err := json.Marshal(session.Messages)
	if err != nil {
		return err
	}

	// 派生摘要与消息正文一起落库：此刻消息就在手里，算这几个字段是白捡的，
	// 换来的是列表页永远不必再读 messages 大字段。meta_version 一并写入，
	// 否则本行会被启动迁移当成"未回填"反复重算。
	meta := DeriveSessionMeta(session.Messages)

	query := `
	INSERT OR REPLACE INTO sessions (id, name, profile, platform, model, workdir, workdir_user_set, workdir_chosen, plan_mode, archived, messages, title, preview, msg_count, tool_call_count, meta_version, input_tokens, output_tokens, cache_read_tokens, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`
	_, err = s.db.ExecContext(ctx, query,
		session.ID, session.Name, session.Profile, session.Platform, session.Model, session.WorkDir, session.WorkDirUserSet, session.WorkDirChosen, session.PlanMode, session.Archived,
		string(messages), meta.Title, meta.Preview, meta.MessageCount, meta.ToolCallCount, metaVersion,
		session.InputTokens, session.OutputTokens, session.CacheReadTokens)
	return err
}

// SaveSessionData saves session token data (used by Gateway for analytics)
// This method accepts a generic map to avoid import cycle issues
func (s *Store) SaveSessionData(ctx context.Context, data *SessionData) error {
	return s.saveSessionDataInternal(ctx, data.ID, data.Platform, data.InputTokens, data.OutputTokens, data.CacheReadTokens)
}

// SaveSessionDataFromMap saves session data from a map (for cross-package usage)
func (s *Store) SaveSessionDataFromMap(ctx context.Context, id, platform string, inputTokens, outputTokens, cacheTokens int) error {
	return s.saveSessionDataInternal(ctx, id, platform, inputTokens, outputTokens, cacheTokens)
}

// saveSessionDataInternal 增量累加一个会话的 token 计数。
//
// **不变量：这里绝不写 platform 列。** platform 是会话的"客户端身份"
// （web / wecom / qq / tui …）：前端侧栏、搜索与"按工作目录查看会话"面板
// 都按它区分 web 会话与各平台 bot 会话（`!s.source || s.source === 'web'`，
// 以及 `ListSessionSummariesByUserWorkDir` 只收 web 与空 platform 这两类）。
//
// 而调用方传进来的 platform 是**另一套语义**的值——server 的队列记账路径
// 传的是"当前 LLM 供应商名"（mimo / huoshan / …）。旧实现把它写进 UPDATE，
// 于是每跑完一个回合，web 会话的 platform 就被改写成供应商名：会话随即从
// 前端侧栏与目录分组里消失，侧栏里那条残留条目的标题也不再刷新
// （用户报"对话标题不更新了"）。token 记账只准动 token 列。
//
// platform 仅用于"会话行还不存在"时兜底 INSERT——网关会在会话行缺失时补建。
func (s *Store) saveSessionDataInternal(ctx context.Context, id, platform string, inputTokens, outputTokens, cacheTokens int) error {
	updateQuery := `
	UPDATE sessions SET 
		input_tokens = input_tokens + ?, 
		output_tokens = output_tokens + ?,
		cache_read_tokens = cache_read_tokens + ?,
		updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`
	result, err := s.db.ExecContext(ctx, updateQuery, inputTokens, outputTokens, cacheTokens, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		insertQuery := `
		INSERT INTO sessions (id, profile, platform, input_tokens, output_tokens, cache_read_tokens, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		`
		_, err = s.db.ExecContext(ctx, insertQuery, id, "", platform, inputTokens, outputTokens, cacheTokens)
		return err
	}

	return nil
}

func (s *Store) LoadSession(ctx context.Context, id string) (*Session, error) {
	query := `SELECT id, name, profile, platform, model, workdir, workdir_user_set, workdir_chosen, plan_mode, archived, messages, input_tokens, output_tokens, cache_read_tokens, created_at, updated_at FROM sessions WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var session Session
	var messagesStr string
	var workDirUserSet, workDirChosen int
	var planMode, archived int
	err := row.Scan(&session.ID, &session.Name, &session.Profile, &session.Platform, &session.Model, &session.WorkDir, &workDirUserSet, &workDirChosen, &planMode, &archived, &messagesStr, &session.InputTokens, &session.OutputTokens, &session.CacheReadTokens, &session.CreatedAt, &session.UpdatedAt)
	if err != nil {
		return nil, err
	}
	session.WorkDirUserSet = workDirUserSet != 0
	session.WorkDirChosen = workDirChosen != 0
	session.PlanMode = planMode != 0
	session.Archived = archived != 0

	if messagesStr != "" {
		if err := json.Unmarshal([]byte(messagesStr), &session.Messages); err != nil {
			// Log error but don't fail - messages are optional
			session.Messages = []types.Message{}
		}
	}

	return &session, nil
}

func (s *Store) ListSessions(ctx context.Context, profile string) ([]*Session, error) {
	var query string
	var rows *sql.Rows
	var err error

	if profile == "" {
		query = `SELECT id, name, profile, platform, model, workdir, workdir_user_set, workdir_chosen, plan_mode, archived, messages, input_tokens, output_tokens, cache_read_tokens, created_at, updated_at FROM sessions ORDER BY updated_at DESC`
		rows, err = s.db.QueryContext(ctx, query)
	} else {
		query = `SELECT id, name, profile, platform, model, workdir, workdir_user_set, workdir_chosen, plan_mode, archived, messages, input_tokens, output_tokens, cache_read_tokens, created_at, updated_at FROM sessions WHERE profile = ? ORDER BY updated_at DESC`
		rows, err = s.db.QueryContext(ctx, query, profile)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSessionListRows(rows)
}

// ListSessionsByUserWorkDir returns web chat sessions whose working directory
// was chosen by the user (workdir_chosen = 1), most recently active
// first. It powers the chat page's "sessions grouped by working directory"
// picker, so gateway/TUI sessions (whose workdir is not user-chosen in the
// web UI) are deliberately excluded.
func (s *Store) ListSessionsByUserWorkDir(ctx context.Context) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, profile, platform, model, workdir, workdir_user_set, workdir_chosen, plan_mode, archived, messages, input_tokens, output_tokens, cache_read_tokens, created_at, updated_at
		FROM sessions
		WHERE (platform = '' OR platform = 'web') AND workdir != '' AND workdir_chosen = 1
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSessionListRows(rows)
}

func scanSessionListRows(rows *sql.Rows) ([]*Session, error) {
	var sessions []*Session
	for rows.Next() {
		var session Session
		var messagesStr sql.NullString
		var workDirUserSet, workDirChosen int
		var planMode, archived int
		err := rows.Scan(&session.ID, &session.Name, &session.Profile, &session.Platform, &session.Model, &session.WorkDir, &workDirUserSet, &workDirChosen, &planMode, &archived, &messagesStr, &session.InputTokens, &session.OutputTokens, &session.CacheReadTokens, &session.CreatedAt, &session.UpdatedAt)
		if err != nil {
			return nil, err
		}
		session.WorkDirUserSet = workDirUserSet != 0
		session.WorkDirChosen = workDirChosen != 0
		session.PlanMode = planMode != 0
		session.Archived = archived != 0
		if messagesStr.Valid && messagesStr.String != "" {
			json.Unmarshal([]byte(messagesStr.String), &session.Messages)
		}
		sessions = append(sessions, &session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *Store) RenameSession(ctx context.Context, id, name string) error {
	query := `UPDATE sessions SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	result, err := s.db.ExecContext(ctx, query, name, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found: %s", id)
	}

	return nil
}

// ListWorkDirs returns distinct non-empty user-chosen working directories,
// ordered by the most recent session update time (newest first).
// Used by the directory picker to recommend previously used directories.
func (s *Store) ListWorkDirs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT workdir
		FROM sessions
		WHERE workdir != '' AND workdir_chosen = 1
		GROUP BY workdir
		ORDER BY MAX(updated_at) DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dirs []string
	for rows.Next() {
		var dir string
		if err := rows.Scan(&dir); err != nil {
			return nil, err
		}
		dirs = append(dirs, dir)
	}
	return dirs, rows.Err()
}

// UpdateWorkDir updates the working directory of a session.
//
// userSet=true 表示这是**用户选定**的目录（新建时选择），此时 workdir_chosen 一并
// 置位；userSet=false 只在把会话退回默认目录时使用，chosen 同时清零。清空目录
// （workDir == ""）意味着该会话之后按需重新分配默认目录。
func (s *Store) UpdateWorkDir(ctx context.Context, id, workDir string, userSet bool) error {
	query := `UPDATE sessions SET workdir = ?, workdir_user_set = ?, workdir_chosen = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	userSetInt := 0
	if userSet && workDir != "" {
		userSetInt = 1
	}
	result, err := s.db.ExecContext(ctx, query, workDir, userSetInt, userSetInt, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found: %s", id)
	}

	return nil
}

// UpdatePlanMode 已随计划模式（plan mode）清理删除 —— 2026-09-29。
// plan_mode 列、Session.PlanMode 字段与增删改查里的读写一律保留（免迁移），
// 只是不再有代码修改它。历史见 .workbuddy/reviews/2026-09-29-plan-mode-review.md。

// SetSessionArchived 归档 / 取消归档一个会话（软隐藏，不删除任何数据）。
//
// 刻意只 UPDATE 这一个字段而不走 SaveSession 整行覆写：SaveSession 需要
// LoadSession 先把消息大字段读出来再写回去，为改一个布尔值付这份 IO 不划算，
// 也平添一次"读-改-写"竞态窗口。updated_at 不动：归档不改变会话的最近活动
// 时间，取消归档后列表排序不应跳变。
func (s *Store) SetSessionArchived(ctx context.Context, id string, archived bool) error {
	archivedInt := 0
	if archived {
		archivedInt = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET archived = ? WHERE id = ?`, archivedInt, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found: %s", id)
	}

	return nil
}

// SetSessionsArchived 批量归档 / 取消归档。返回实际更新的条数。
// 单条 SQL 完成整批更新：比循环调用 SetSessionArchived 少 N-1 次提交，
// 且天然原子——要么全部生效，要么全部不动。
func (s *Store) SetSessionsArchived(ctx context.Context, ids []string, archived bool) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	archivedInt := 0
	if archived {
		archivedInt = 1
	}
	placeholders := strings.Repeat("?, ", len(ids)-1) + "?"
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, archivedInt)
	for _, id := range ids {
		args = append(args, id)
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET archived = ? WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found: %s", id)
	}

	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
