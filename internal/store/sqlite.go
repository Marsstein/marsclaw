package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	t "github.com/marsstein/liteclaw/internal/types"
)

// SQLiteStore implements Store using a local SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLite opens (or creates) a SQLite database at the given path.
func NewSQLite(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *SQLiteStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id         TEXT PRIMARY KEY,
			title      TEXT NOT NULL DEFAULT '',
			source     TEXT NOT NULL DEFAULT 'cli',
			metadata   TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			role       TEXT NOT NULL,
			content    TEXT,
			tool_calls TEXT,
			tool_result TEXT,
			timestamp  DATETIME NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, id);
	`)
	return err
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) CreateSession(ctx context.Context, session *Session) error {
	meta, _ := json.Marshal(session.Metadata)
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, title, source, metadata, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		session.ID, session.Title, session.Source, string(meta), session.CreatedAt, session.UpdatedAt,
	)
	return err
}

func (s *SQLiteStore) GetSession(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, title, source, metadata, created_at, updated_at FROM sessions WHERE id = ?", id)

	var sess Session
	var meta string
	if err := row.Scan(&sess.ID, &sess.Title, &sess.Source, &meta, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	json.Unmarshal([]byte(meta), &sess.Metadata)
	return &sess, nil
}

func (s *SQLiteStore) ListSessions(ctx context.Context) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, title, source, metadata, created_at, updated_at FROM sessions ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var sess Session
		var meta string
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Source, &meta, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(meta), &sess.Metadata)
		sessions = append(sessions, &sess)
	}
	return sessions, rows.Err()
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	return err
}

func (s *SQLiteStore) UpdateTitle(ctx context.Context, id, title string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?",
		title, time.Now(), id)
	return err
}

func (s *SQLiteStore) AppendMessages(ctx context.Context, sessionID string, msgs []t.Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO messages (session_id, role, content, tool_calls, tool_result, timestamp) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, m := range msgs {
		var toolCalls, toolResult sql.NullString
		if len(m.ToolCalls) > 0 {
			b, _ := json.Marshal(m.ToolCalls)
			toolCalls = sql.NullString{String: string(b), Valid: true}
		}
		if m.ToolResult != nil {
			b, _ := json.Marshal(m.ToolResult)
			toolResult = sql.NullString{String: string(b), Valid: true}
		}
		if _, err := stmt.ExecContext(ctx, sessionID, string(m.Role), m.Content, toolCalls, toolResult, m.Timestamp); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, "UPDATE sessions SET updated_at = ? WHERE id = ?", time.Now(), sessionID); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetMessages(ctx context.Context, sessionID string) ([]t.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT role, content, tool_calls, tool_result, timestamp FROM messages WHERE session_id = ? ORDER BY id", sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []t.Message
	for rows.Next() {
		var m t.Message
		var role string
		var toolCalls, toolResult sql.NullString
		if err := rows.Scan(&role, &m.Content, &toolCalls, &toolResult, &m.Timestamp); err != nil {
			return nil, err
		}
		m.Role = t.Role(role)
		if toolCalls.Valid {
			json.Unmarshal([]byte(toolCalls.String), &m.ToolCalls)
		}
		if toolResult.Valid {
			json.Unmarshal([]byte(toolResult.String), &m.ToolResult)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
