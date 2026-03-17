package hooks

import (
	"context"
	"log/slog"

	t "github.com/marsstein/marsclaw/internal/types"
)

// Event identifies when a hook fires.
type Event string

const (
	BeforeToolCall Event = "before_tool_call"
	AfterToolCall  Event = "after_tool_call"
	BeforeLLMCall  Event = "before_llm_call"
	AfterLLMCall   Event = "after_llm_call"
	OnError        Event = "on_error"
)

// Hook is a function called at a specific point in the agent loop.
type Hook func(ctx context.Context, data *HookData) error

// HookData carries context about the event.
type HookData struct {
	Event    Event       `json:"event"`
	ToolCall *t.ToolCall `json:"tool_call,omitempty"`
	Result   string      `json:"result,omitempty"`
	Error    string      `json:"error,omitempty"`
	Model    string      `json:"model,omitempty"`
}

// Manager holds registered hooks.
type Manager struct {
	hooks  map[Event][]Hook
	logger *slog.Logger
}

// NewManager creates a hook manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		hooks:  make(map[Event][]Hook),
		logger: logger,
	}
}

// Register adds a hook for the given event.
func (m *Manager) Register(event Event, hook Hook) {
	m.hooks[event] = append(m.hooks[event], hook)
}

// Fire executes all hooks for the given event.
// Returns the first error encountered, but runs all hooks.
func (m *Manager) Fire(ctx context.Context, data *HookData) error {
	hooks := m.hooks[data.Event]
	if len(hooks) == 0 {
		return nil
	}

	var firstErr error
	for _, h := range hooks {
		if err := h(ctx, data); err != nil {
			if m.logger != nil {
				m.logger.Warn("hook error", "event", data.Event, "error", err)
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// HasHooks returns true if any hooks are registered for the event.
func (m *Manager) HasHooks(event Event) bool {
	return len(m.hooks[event]) > 0
}

// LoggingHook returns a hook that logs tool calls.
func LoggingHook(logger *slog.Logger) Hook {
	return func(ctx context.Context, data *HookData) error {
		switch data.Event {
		case BeforeToolCall:
			if data.ToolCall != nil {
				logger.Info("tool call", "tool", data.ToolCall.Name)
			}
		case AfterToolCall:
			if data.ToolCall != nil {
				logger.Info("tool result", "tool", data.ToolCall.Name, "len", len(data.Result))
			}
		case OnError:
			logger.Error("agent error", "error", data.Error)
		}
		return nil
	}
}
