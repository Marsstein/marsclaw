// Package types holds shared data structures used across agent, llm, and security packages.
package types

import (
	"context"
	"encoding/json"
	"time"
)

// Role identifies who produced a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// StopReason captures why the agent loop terminated.
type StopReason string

const (
	StopFinalResponse   StopReason = "final_response"
	StopMaxTurns        StopReason = "max_turns"
	StopBudgetExceeded  StopReason = "budget_exceeded"
	StopError           StopReason = "error"
	StopHumanDenied     StopReason = "human_denied"
	StopContextOverflow StopReason = "context_overflow"
)

// ToolCall represents an LLM's request to invoke a tool.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is what we feed back after executing a tool.
type ToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// Message is a single entry in the conversation.
type Message struct {
	Role       Role        `json:"role"`
	Content    string      `json:"content,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolResult *ToolResult `json:"tool_result,omitempty"`
	TokenCount int         `json:"token_count,omitempty"`
	Timestamp  time.Time   `json:"timestamp"`
}

// ToolDef describes a tool the LLM can invoke.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	DangerLevel DangerLevel     `json:"danger_level"`
	ReadOnly    bool            `json:"read_only"`
}

// DangerLevel controls whether human approval is required.
type DangerLevel int

const (
	DangerNone   DangerLevel = iota // execute freely
	DangerLow                       // log but execute
	DangerMedium                    // ask if strict mode
	DangerHigh                      // always ask
)

// LLMResponse is what comes back from a single LLM API call.
type LLMResponse struct {
	Content      string     `json:"content,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	InputTokens  int        `json:"input_tokens"`
	OutputTokens int        `json:"output_tokens"`
	Model        string     `json:"model"`
}

// StreamEvent is emitted during streaming generation.
type StreamEvent struct {
	Type     StreamEventType `json:"type"`
	Delta    string          `json:"delta,omitempty"`
	ToolCall *ToolCall       `json:"tool_call,omitempty"`
	Done     bool            `json:"done"`
}

// StreamEventType classifies streaming events.
type StreamEventType string

const (
	StreamText      StreamEventType = "text"
	StreamToolStart StreamEventType = "tool_start"
	StreamToolDone  StreamEventType = "tool_done"
	StreamError     StreamEventType = "error"
)

// RunResult is the final output of an agent run.
type RunResult struct {
	Response    string        `json:"response"`
	StopReason  StopReason    `json:"stop_reason"`
	TurnCount   int           `json:"turn_count"`
	TotalInput  int           `json:"total_input_tokens"`
	TotalOutput int           `json:"total_output_tokens"`
	Duration    time.Duration `json:"duration"`
	Error       error         `json:"error,omitempty"`
	Trace       []TraceEntry  `json:"trace"`
	History     []Message     `json:"history,omitempty"`
}

// TraceEntry records one step in the agent loop for observability.
type TraceEntry struct {
	Step      int           `json:"step"`
	Phase     string        `json:"phase"`
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration"`
	Input     int           `json:"input_tokens,omitempty"`
	Output    int           `json:"output_tokens,omitempty"`
	ToolName  string        `json:"tool_name,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// ContextParts holds the raw material before context assembly.
type ContextParts struct {
	SoulPrompt  string    // core identity (SOUL.md)
	AgentPrompt string    // agent-specific instructions (AGENTS.md)
	Memory      string    // injected memory/knowledge
	History     []Message // conversation history
}

// ProviderRequest is what we send to the LLM.
type ProviderRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stop        []string  `json:"stop,omitempty"`
}

// Provider is the interface any LLM backend must implement.
type Provider interface {
	Call(ctx context.Context, req *ProviderRequest) (*LLMResponse, error)
	Stream(ctx context.Context, req *ProviderRequest, events chan<- StreamEvent) (*LLMResponse, error)
	CountTokens(messages []Message, tools []ToolDef) int
	MaxContextWindow() int
}

// TokenCounter estimates token counts. Subset of Provider.
type TokenCounter interface {
	CountTokens(messages []Message, tools []ToolDef) int
}

// ToolExecutor runs a single tool call.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCall) (string, error)
}

// CostRecorder tracks token costs.
type CostRecorder interface {
	Record(model string, inputTokens, outputTokens int) int64
	FormatCostLine(model string, inputTokens, outputTokens int) string
	OverBudget() bool
}

// ApprovalFunc is called when human-in-the-loop approval is needed.
type ApprovalFunc func(call ToolCall, reason string) bool
