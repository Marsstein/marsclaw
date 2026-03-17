package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SQLiteStore implements memory.Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a memory store using an existing SQLite connection.
func NewSQLiteStore(db *sql.DB) (*SQLiteStore, error) {
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS memory_entries (
			id         TEXT PRIMARY KEY,
			kind       TEXT NOT NULL,
			content    TEXT NOT NULL,
			tags       TEXT DEFAULT '[]',
			score      REAL DEFAULT 1.0,
			access_cnt INTEGER DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_memory_kind ON memory_entries(kind);
	`)
	return err
}

func (s *SQLiteStore) Save(ctx context.Context, entry *Entry) error {
	tags, _ := json.Marshal(entry.Tags)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO memory_entries (id, kind, content, tags, score, access_cnt, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			content = excluded.content,
			tags = excluded.tags,
			score = excluded.score,
			access_cnt = excluded.access_cnt,
			updated_at = excluded.updated_at`,
		entry.ID, entry.Kind, entry.Content, string(tags),
		entry.Score, entry.AccessCnt,
		entry.CreatedAt.Format(time.RFC3339),
		entry.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (*Entry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, kind, content, tags, score, access_cnt, created_at, updated_at
		 FROM memory_entries WHERE id = ?`, id)
	return scanEntry(row)
}

func (s *SQLiteStore) Search(ctx context.Context, query string, limit int) ([]*Entry, error) {
	// Simple keyword search — split query into words, match any.
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return s.List(ctx, "", limit)
	}

	conditions := make([]string, len(words))
	args := make([]any, len(words))
	for i, w := range words {
		conditions[i] = "LOWER(content) LIKE ?"
		args[i] = "%" + w + "%"
	}

	args = append(args, limit)
	q := fmt.Sprintf(`
		SELECT id, kind, content, tags, score, access_cnt, created_at, updated_at
		FROM memory_entries
		WHERE %s
		ORDER BY score DESC, updated_at DESC
		LIMIT ?`, strings.Join(conditions, " OR "))

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

func (s *SQLiteStore) List(ctx context.Context, kind Kind, limit int) ([]*Entry, error) {
	var q string
	var args []any

	if kind != "" {
		q = `SELECT id, kind, content, tags, score, access_cnt, created_at, updated_at
			 FROM memory_entries WHERE kind = ? ORDER BY updated_at DESC LIMIT ?`
		args = []any{kind, limit}
	} else {
		q = `SELECT id, kind, content, tags, score, access_cnt, created_at, updated_at
			 FROM memory_entries ORDER BY updated_at DESC LIMIT ?`
		args = []any{limit}
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM memory_entries WHERE id = ?`, id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEntry(row scanner) (*Entry, error) {
	var e Entry
	var tags, createdAt, updatedAt string
	err := row.Scan(&e.ID, &e.Kind, &e.Content, &tags, &e.Score, &e.AccessCnt, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(tags), &e.Tags)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &e, nil
}

func scanEntries(rows *sql.Rows) ([]*Entry, error) {
	var entries []*Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return entries, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
