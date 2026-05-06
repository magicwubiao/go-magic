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

type Store struct {
	db *sql.DB
}

type Session struct {
	ID        string          `json:"id"`
	Profile   string          `json:"profile"`
	Platform  string          `json:"platform"`
	Messages  []types.Message `json:"messages"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := initSchema(db); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &Store{db: db}, nil
}

func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		profile TEXT NOT NULL,
		platform TEXT NOT NULL,
		messages TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_profile ON sessions(profile);
	CREATE INDEX IF NOT EXISTS idx_sessions_platform ON sessions(platform);
	`
	_, err := db.Exec(schema)
	return err
}

func (s *Store) SaveSession(ctx context.Context, session *Session) error {
	messages, err := json.Marshal(session.Messages)
	if err != nil {
		return err
	}

	query := `
	INSERT OR REPLACE INTO sessions (id, profile, platform, messages, updated_at)
	VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`
	_, err = s.db.ExecContext(ctx, query, session.ID, session.Profile, session.Platform, string(messages))
	return err
}

func (s *Store) LoadSession(ctx context.Context, id string) (*Session, error) {
	query := `SELECT id, profile, platform, messages, created_at, updated_at FROM sessions WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var session Session
	var messagesStr string
	err := row.Scan(&session.ID, &session.Profile, &session.Platform, &messagesStr, &session.CreatedAt, &session.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if messagesStr != "" {
		json.Unmarshal([]byte(messagesStr), &session.Messages)
	}

	return &session, nil
}

func (s *Store) ListSessions(ctx context.Context, profile string) ([]*Session, error) {
	query := `SELECT id, profile, platform, created_at, updated_at FROM sessions WHERE profile = ? ORDER BY updated_at DESC`
	rows, err := s.db.QueryContext(ctx, query, profile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var session Session
		err := rows.Scan(&session.ID, &session.Profile, &session.Platform, &session.CreatedAt, &session.UpdatedAt)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, &session)
	}

	return sessions, nil
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
