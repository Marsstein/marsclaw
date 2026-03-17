package agent

// Re-export types from the shared types package for convenience.
// This allows callers to use agent.Message, agent.ToolCall, etc.
import t "github.com/marsstein/liteclaw/internal/types"

type (
	Role            = t.Role
	StopReason      = t.StopReason
	ToolCall        = t.ToolCall
	ToolResult      = t.ToolResult
	Message         = t.Message
	ToolDef         = t.ToolDef
	DangerLevel     = t.DangerLevel
	LLMResponse     = t.LLMResponse
	StreamEvent     = t.StreamEvent
	StreamEventType = t.StreamEventType
	RunResult       = t.RunResult
	TraceEntry      = t.TraceEntry
	ContextParts    = t.ContextParts
	ProviderRequest = t.ProviderRequest
	Provider        = t.Provider
	TokenCounter    = t.TokenCounter
	ToolExecutor    = t.ToolExecutor
	CostRecorder    = t.CostRecorder
	ApprovalFunc    = t.ApprovalFunc
)

const (
	RoleSystem    = t.RoleSystem
	RoleUser      = t.RoleUser
	RoleAssistant = t.RoleAssistant
	RoleTool      = t.RoleTool

	StopFinalResponse   = t.StopFinalResponse
	StopMaxTurns        = t.StopMaxTurns
	StopBudgetExceeded  = t.StopBudgetExceeded
	StopError           = t.StopError
	StopHumanDenied     = t.StopHumanDenied
	StopContextOverflow = t.StopContextOverflow

	StreamText      = t.StreamText
	StreamToolStart = t.StreamToolStart
	StreamToolDone  = t.StreamToolDone
	StreamError     = t.StreamError

	DangerNone   = t.DangerNone
	DangerLow    = t.DangerLow
	DangerMedium = t.DangerMedium
	DangerHigh   = t.DangerHigh
)
