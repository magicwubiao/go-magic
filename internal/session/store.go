package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	Messages        []types.Message `json:"messages"`
	InputTokens     int             `json:"input_tokens"`
	OutputTokens    int             `json:"output_tokens"`
	CacheReadTokens int             `json:"cache_read_tokens"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?mode=rwc&_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

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
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		cache_read_tokens INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_profile ON sessions(profile);
	CREATE INDEX IF NOT EXISTS idx_sessions_platform ON sessions(platform);
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
	return addWorkDirUserSetColumnIfNotExists(db)
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

func (s *Store) SaveSession(ctx context.Context, session *Session) error {
	messages, err := json.Marshal(session.Messages)
	if err != nil {
		return err
	}

	query := `
	INSERT OR REPLACE INTO sessions (id, name, profile, platform, model, workdir, workdir_user_set, messages, input_tokens, output_tokens, cache_read_tokens, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`
	_, err = s.db.ExecContext(ctx, query, session.ID, session.Name, session.Profile, session.Platform, session.Model, session.WorkDir, session.WorkDirUserSet, string(messages), session.InputTokens, session.OutputTokens, session.CacheReadTokens)
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

func (s *Store) saveSessionDataInternal(ctx context.Context, id, platform string, inputTokens, outputTokens, cacheTokens int) error {
	updateQuery := `
	UPDATE sessions SET 
		platform = ?, 
		input_tokens = input_tokens + ?, 
		output_tokens = output_tokens + ?,
		cache_read_tokens = cache_read_tokens + ?,
		updated_at = CURRENT_TIMESTAMP
	WHERE id = ?
	`
	result, err := s.db.ExecContext(ctx, updateQuery, platform, inputTokens, outputTokens, cacheTokens, id)
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
	query := `SELECT id, name, profile, platform, model, workdir, workdir_user_set, messages, input_tokens, output_tokens, cache_read_tokens, created_at, updated_at FROM sessions WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var session Session
	var messagesStr string
	var workDirUserSet int
	err := row.Scan(&session.ID, &session.Name, &session.Profile, &session.Platform, &session.Model, &session.WorkDir, &workDirUserSet, &messagesStr, &session.InputTokens, &session.OutputTokens, &session.CacheReadTokens, &session.CreatedAt, &session.UpdatedAt)
	if err != nil {
		return nil, err
	}
	session.WorkDirUserSet = workDirUserSet != 0

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
		query = `SELECT id, name, profile, platform, model, workdir, workdir_user_set, messages, input_tokens, output_tokens, cache_read_tokens, created_at, updated_at FROM sessions ORDER BY updated_at DESC`
		rows, err = s.db.QueryContext(ctx, query)
	} else {
		query = `SELECT id, name, profile, platform, model, workdir, workdir_user_set, messages, input_tokens, output_tokens, cache_read_tokens, created_at, updated_at FROM sessions WHERE profile = ? ORDER BY updated_at DESC`
		rows, err = s.db.QueryContext(ctx, query, profile)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var session Session
		var messagesStr sql.NullString
		var workDirUserSet int
		err := rows.Scan(&session.ID, &session.Name, &session.Profile, &session.Platform, &session.Model, &session.WorkDir, &workDirUserSet, &messagesStr, &session.InputTokens, &session.OutputTokens, &session.CacheReadTokens, &session.CreatedAt, &session.UpdatedAt)
		if err != nil {
			return nil, err
		}
		session.WorkDirUserSet = workDirUserSet != 0
		if messagesStr.Valid && messagesStr.String != "" {
			json.Unmarshal([]byte(messagesStr.String), &session.Messages)
		}
		sessions = append(sessions, &session)
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

// ListWorkDirs returns distinct non-empty user-set working directories,
// ordered by the most recent session update time (newest first).
// Used by the directory picker to recommend previously used directories.
func (s *Store) ListWorkDirs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT workdir
		FROM sessions
		WHERE workdir != '' AND workdir_user_set = 1
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
// If userSet is true, marks the workdir as user-set (immutable thereafter from the API).
func (s *Store) UpdateWorkDir(ctx context.Context, id, workDir string, userSet bool) error {
	query := `UPDATE sessions SET workdir = ?, workdir_user_set = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	userSetInt := 0
	if userSet {
		userSetInt = 1
	}
	result, err := s.db.ExecContext(ctx, query, workDir, userSetInt, id)
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
