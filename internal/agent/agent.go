package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/marsstein/liteclaw/internal/llm"
)

// AgentConfig controls the agent loop behavior.
type AgentConfig struct {
	Model         string `json:"model"`
	FallbackModel string `json:"fallback_model,omitempty"`

	MaxTurns                int `json:"max_turns"`
	MaxConsecutiveToolCalls int `json:"max_consecutive_tool_calls"`

	MaxInputTokens  int `json:"max_input_tokens"`
	MaxOutputTokens int `json:"max_output_tokens"`

	SystemPromptBudget float64 `json:"system_prompt_budget"`
	HistoryBudget      float64 `json:"history_budget"`
	ReservedForOutput  float64 `json:"reserved_for_output"`

	MaxToolResultLen int `json:"max_tool_result_len"`

	LLMTimeout  time.Duration `json:"llm_timeout"`
	ToolTimeout time.Duration `json:"tool_timeout"`

	MaxRetries     int           `json:"max_retries"`
	RetryBaseDelay time.Duration `json:"retry_base_delay"`

	EnableStreaming bool    `json:"enable_streaming"`
	Temperature    float64 `json:"temperature"`
}

// DefaultAgentConfig returns a sane production configuration.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		MaxTurns:                25,
		MaxConsecutiveToolCalls: 15,
		MaxInputTokens:         180_000,
		MaxOutputTokens:        16_384,
		SystemPromptBudget:     0.25,
		HistoryBudget:          0.65,
		ReservedForOutput:      0.10,
		MaxToolResultLen:       30_000,
		LLMTimeout:             120 * time.Second,
		ToolTimeout:            60 * time.Second,
		MaxRetries:             3,
		RetryBaseDelay:         1 * time.Second,
		EnableStreaming:        true,
		Temperature:            0.0,
	}
}

// SafetyChecker validates tool calls and scans for credentials.
type SafetyChecker interface {
	ValidateToolCall(call ToolCall) error
	ScanCredentials(content string) (string, bool)
}

// Agent is the core agent loop. One Agent per conversation.
type Agent struct {
	provider       Provider
	config         AgentConfig
	tools          map[string]ToolExecutor
	toolDefs       []ToolDef
	contextBuilder *ContextBuilder
	safety         SafetyChecker
	cost           CostRecorder
	logger         *slog.Logger

	onStream func(StreamEvent)

	mu          sync.Mutex
	history     []Message
	totalInput  int
	totalOutput int
}

// Option configures the agent.
type Option func(*Agent)

func WithLogger(l *slog.Logger) Option              { return func(a *Agent) { a.logger = l } }
func WithStreamHandler(fn func(StreamEvent)) Option  { return func(a *Agent) { a.onStream = fn } }
func WithCostTracker(ct CostRecorder) Option          { return func(a *Agent) { a.cost = ct } }
func WithSafety(sc SafetyChecker) Option              { return func(a *Agent) { a.safety = sc } }

// New creates an agent with the given provider, tools, and config.
func New(
	provider Provider,
	config AgentConfig,
	tools map[string]ToolExecutor,
	toolDefs []ToolDef,
	opts ...Option,
) *Agent {
	a := &Agent{
		provider: provider,
		config:   config,
		tools:    tools,
		toolDefs: toolDefs,
		history:  make([]Message, 0, 64),
	}

	for _, opt := range opts {
		opt(a)
	}

	if a.logger == nil {
		a.logger = slog.Default()
	}

	a.contextBuilder = NewContextBuilder(provider, config, toolDefs)

	return a
}

// AddTools dynamically adds tools to the agent (used by supervisor pattern).
func (a *Agent) AddTools(defs []ToolDef, executors map[string]ToolExecutor) {
	a.toolDefs = append(a.toolDefs, defs...)
	for name, exec := range executors {
		a.tools[name] = exec
	}
	a.contextBuilder = NewContextBuilder(a.provider, a.config, a.toolDefs)
}

// Run executes the full agent loop for a single user turn.
func (a *Agent) Run(ctx context.Context, parts ContextParts) *RunResult {
	start := time.Now()
	result := &RunResult{Trace: make([]TraceEntry, 0, 16)}

	a.mu.Lock()
	a.history = parts.History
	a.mu.Unlock()

	for turn := range a.config.MaxTurns {
		result.TurnCount = turn + 1

		a.logger.Info("agent turn", "turn", turn+1, "history_len", len(a.history))

		// Phase 1: Build context.
		messages := a.contextBuilder.Build(ContextParts{
			SoulPrompt:  parts.SoulPrompt,
			AgentPrompt: parts.AgentPrompt,
			Memory:      parts.Memory,
			History:     a.history,
		})

		// Budget guard: check cost limit before calling.
		if a.cost != nil && a.cost.OverBudget() {
			result.Response = "Daily cost budget exceeded. Please try again tomorrow."
			result.StopReason = StopBudgetExceeded
			break
		}

		// Phase 2: Call LLM.
		llmResp, err := a.callLLM(ctx, messages, result)
		if err != nil {
			result.StopReason = StopError
			result.Error = err
			break
		}

		// Track tokens and cost.
		a.totalInput += llmResp.InputTokens
		a.totalOutput += llmResp.OutputTokens
		result.TotalInput = a.totalInput
		result.TotalOutput = a.totalOutput
		if a.cost != nil {
			a.cost.Record(llmResp.Model, llmResp.InputTokens, llmResp.OutputTokens)
		}

		// Phase 3: Check budget.
		if a.totalInput > a.config.MaxInputTokens {
			if llmResp.Content != "" {
				result.Response = llmResp.Content
			} else {
				result.Response = "I've reached the context limit for this conversation."
			}
			result.StopReason = StopBudgetExceeded
			break
		}

		// Phase 4: Route response.
		if len(llmResp.ToolCalls) == 0 {
			result.Response = llmResp.Content
			result.StopReason = StopFinalResponse
			a.history = append(a.history, Message{
				Role:      RoleAssistant,
				Content:   llmResp.Content,
				Timestamp: time.Now(),
			})
			break
		}

		a.history = append(a.history, Message{
			Role:      RoleAssistant,
			Content:   llmResp.Content,
			ToolCalls: llmResp.ToolCalls,
			Timestamp: time.Now(),
		})

		if consecutive := a.countConsecutiveToolTurns(); consecutive >= a.config.MaxConsecutiveToolCalls {
			a.history = append(a.history, Message{
				Role:      RoleSystem,
				Content:   "You have made many consecutive tool calls. Please synthesize what you've learned and respond to the user now.",
				Timestamp: time.Now(),
			})
			continue
		}

		stop, toolStop := a.executeToolCalls(ctx, llmResp.ToolCalls, result)
		if stop {
			result.StopReason = toolStop
			break
		}
	}

	if result.StopReason == "" {
		result.StopReason = StopMaxTurns
		if result.Response == "" {
			result.Response = "I've reached the maximum number of turns for this conversation."
		}
	}

	result.Duration = time.Since(start)
	result.History = a.history
	a.logger.Info("agent run complete",
		"stop_reason", result.StopReason,
		"turns", result.TurnCount,
		"duration", result.Duration,
	)

	return result
}

func (a *Agent) callLLM(ctx context.Context, messages []Message, result *RunResult) (*LLMResponse, error) {
	llmStart := time.Now()

	req := &ProviderRequest{
		Model:       a.config.Model,
		Messages:    messages,
		Tools:       a.toolDefs,
		MaxTokens:   a.config.MaxOutputTokens,
		Temperature: a.config.Temperature,
	}

	callCtx, cancel := context.WithTimeout(ctx, a.config.LLMTimeout)
	defer cancel()

	rc := llm.RetryConfig{
		MaxRetries: a.config.MaxRetries,
		BaseDelay:  a.config.RetryBaseDelay,
	}

	var resp *LLMResponse
	var err error

	if a.config.EnableStreaming && a.onStream != nil {
		resp, err = llm.WithRetry(callCtx, rc, func(ctx context.Context) (*LLMResponse, error) {
			events := make(chan StreamEvent, 64)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for ev := range events {
					a.onStream(ev)
				}
			}()
			r, e := a.provider.Stream(ctx, req, events)
			<-done
			return r, e
		})
	} else {
		resp, err = llm.WithRetry(callCtx, rc, func(ctx context.Context) (*LLMResponse, error) {
			return a.provider.Call(ctx, req)
		})
	}

	entry := TraceEntry{
		Step:      len(result.Trace),
		Phase:     "llm_call",
		Timestamp: llmStart,
		Duration:  time.Since(llmStart),
	}
	if resp != nil {
		entry.Input = resp.InputTokens
		entry.Output = resp.OutputTokens
	}
	if err != nil {
		entry.Error = err.Error()
	}
	result.Trace = append(result.Trace, entry)

	return resp, err
}

func (a *Agent) executeToolCalls(ctx context.Context, calls []ToolCall, result *RunResult) (bool, StopReason) {
	for _, call := range calls {
		if a.onStream != nil {
			a.onStream(StreamEvent{Type: StreamToolStart, ToolCall: &call})
		}

		toolStart := time.Now()

		// Safety validation.
		if a.safety != nil {
			if err := a.safety.ValidateToolCall(call); err != nil {
				errMsg := err.Error()
				isHumanDenied := false
				if se, ok := err.(interface{ IsDenied() bool }); ok {
					isHumanDenied = se.IsDenied()
				}
				if isHumanDenied {
					a.appendToolResult(call.ID, errMsg, true)
					return true, StopHumanDenied
				}
				a.appendToolResult(call.ID, fmt.Sprintf("Error: %s", errMsg), true)
				a.recordToolTrace(result, call, toolStart, err)
				continue
			}
		}

		executor, ok := a.tools[call.Name]
		if !ok {
			errMsg := fmt.Sprintf("Error: tool %q has no registered executor", call.Name)
			a.appendToolResult(call.ID, errMsg, true)
			a.recordToolTrace(result, call, toolStart, fmt.Errorf("no executor for %q", call.Name))
			continue
		}

		toolCtx, toolCancel := context.WithTimeout(ctx, a.config.ToolTimeout)
		output, err := executor.Execute(toolCtx, call)
		toolCancel()

		if err != nil {
			if toolCtx.Err() != nil {
				output = fmt.Sprintf("Error: tool %q timed out after %s", call.Name, a.config.ToolTimeout)
			} else {
				output = fmt.Sprintf("Error: tool %q failed: %s", call.Name, err.Error())
			}
			a.appendToolResult(call.ID, output, true)
			a.recordToolTrace(result, call, toolStart, err)
			if a.onStream != nil {
				a.onStream(StreamEvent{Type: StreamToolDone, ToolCall: &call})
			}
			continue
		}

		// Credential scanning.
		if a.safety != nil {
			var credFound bool
			output, credFound = a.safety.ScanCredentials(output)
			if credFound {
				a.logger.Warn("credentials redacted", "tool", call.Name)
			}
		}

		output = TruncateToolResult(output, a.config.MaxToolResultLen)
		a.appendToolResult(call.ID, output, false)
		a.recordToolTrace(result, call, toolStart, nil)

		if a.onStream != nil {
			a.onStream(StreamEvent{Type: StreamToolDone, ToolCall: &call})
		}
	}

	return false, ""
}

func (a *Agent) appendToolResult(callID, content string, isError bool) {
	a.history = append(a.history, Message{
		Role: RoleTool,
		ToolResult: &ToolResult{
			CallID:  callID,
			Content: content,
			IsError: isError,
		},
		Timestamp: time.Now(),
	})
}

func (a *Agent) recordToolTrace(result *RunResult, call ToolCall, start time.Time, err error) {
	entry := TraceEntry{
		Step:      len(result.Trace),
		Phase:     "tool_call",
		Timestamp: start,
		Duration:  time.Since(start),
		ToolName:  call.Name,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	result.Trace = append(result.Trace, entry)
}

func (a *Agent) countConsecutiveToolTurns() int {
	count := 0
	for i := len(a.history) - 1; i >= 0; i-- {
		msg := a.history[i]
		switch {
		case msg.Role == RoleAssistant && len(msg.ToolCalls) > 0:
			count++
		case msg.Role == RoleTool:
			continue // skip tool results, only count assistant tool rounds
		default:
			return count
		}
	}
	return count
}
