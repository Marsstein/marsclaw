package store

import (
	"context"
	"time"

	t "github.com/marsstein/marsclaw/internal/types"
)

// Session represents a persistent conversation.
type Session struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Source    string            `json:"source"` // "cli", "server", "telegram"
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Store is the interface for session persistence.
type Store interface {
	CreateSession(ctx context.Context, session *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	ListSessions(ctx context.Context) ([]*Session, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateTitle(ctx context.Context, id, title string) error

	AppendMessages(ctx context.Context, sessionID string, msgs []t.Message) error
	GetMessages(ctx context.Context, sessionID string) ([]t.Message, error)
}
