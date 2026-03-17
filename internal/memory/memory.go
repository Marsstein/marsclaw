package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Entry is a single piece of persistent knowledge.
type Entry struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`    // episodic, semantic, procedural
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	Score     float64   `json:"score"`     // relevance score (0-1)
	AccessCnt int       `json:"access_cnt"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Kind classifies memory entries.
type Kind string

const (
	KindEpisodic   Kind = "episodic"   // conversation summaries, past events
	KindSemantic   Kind = "semantic"   // facts, knowledge, user preferences
	KindProcedural Kind = "procedural" // how-to, workflows, learned patterns
)

// Store persists memory entries.
type Store interface {
	Save(ctx context.Context, entry *Entry) error
	Get(ctx context.Context, id string) (*Entry, error)
	Search(ctx context.Context, query string, limit int) ([]*Entry, error)
	List(ctx context.Context, kind Kind, limit int) ([]*Entry, error)
	Delete(ctx context.Context, id string) error
}

// Manager handles cross-session memory with bounded storage.
type Manager struct {
	store    Store
	maxChars map[Kind]int
}

// Config controls memory bounds.
type Config struct {
	EpisodicMaxChars  int
	SemanticMaxChars  int
	ProceduralMaxChars int
}

// NewManager creates a memory manager.
func NewManager(store Store, cfg Config) *Manager {
	return &Manager{
		store: store,
		maxChars: map[Kind]int{
			KindEpisodic:   cfg.EpisodicMaxChars,
			KindSemantic:   cfg.SemanticMaxChars,
			KindProcedural: cfg.ProceduralMaxChars,
		},
	}
}

// Remember stores a new memory entry.
func (m *Manager) Remember(ctx context.Context, kind Kind, content string, tags []string) (*Entry, error) {
	entry := &Entry{
		ID:        fmt.Sprintf("mem_%d", time.Now().UnixNano()),
		Kind:      kind,
		Content:   content,
		Tags:      tags,
		Score:     1.0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := m.store.Save(ctx, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// Recall searches memory for relevant entries.
func (m *Manager) Recall(ctx context.Context, query string, limit int) ([]*Entry, error) {
	if limit <= 0 {
		limit = 10
	}
	entries, err := m.store.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	// Bump access counts.
	for _, e := range entries {
		e.AccessCnt++
		m.store.Save(ctx, e)
	}
	return entries, nil
}

// Forget removes a memory entry.
func (m *Manager) Forget(ctx context.Context, id string) error {
	return m.store.Delete(ctx, id)
}

// Inject builds a memory context string for agent prompts.
func (m *Manager) Inject(ctx context.Context, query string) string {
	entries, err := m.store.Search(ctx, query, 20)
	if err != nil || len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<memory>\n")

	budgets := make(map[Kind]int)
	for k, v := range m.maxChars {
		budgets[k] = v
	}

	for _, e := range entries {
		remaining := budgets[e.Kind]
		if remaining <= 0 {
			continue
		}

		line := fmt.Sprintf("[%s] %s\n", e.Kind, e.Content)
		if len(line) > remaining {
			line = line[:remaining] + "...\n"
		}
		b.WriteString(line)
		budgets[e.Kind] -= len(line)
	}

	b.WriteString("</memory>")
	return b.String()
}

// ToolDefs returns tool definitions for agent-driven memory.
func ToolDefs() json.RawMessage {
	schema := `{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"enum": ["remember", "recall", "forget"],
				"description": "Action to perform"
			},
			"kind": {
				"type": "string",
				"enum": ["episodic", "semantic", "procedural"],
				"description": "Memory type (for remember)"
			},
			"content": {
				"type": "string",
				"description": "Content to remember or query to recall"
			},
			"tags": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Tags for the memory entry"
			},
			"id": {
				"type": "string",
				"description": "Memory ID (for forget)"
			}
		},
		"required": ["action"]
	}`
	return json.RawMessage(schema)
}
