package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// SubAgentDef defines a child agent that can be invoked as a tool.
type SubAgentDef struct {
	Name        string
	Description string
	Agent       *Agent
	Parts       ContextParts
}

// SubAgentExecutor wraps a child agent as a ToolExecutor.
type SubAgentExecutor struct {
	child  *Agent
	parts  ContextParts
	logger *slog.Logger
}

// NewSubAgentExecutor creates a tool executor that runs a child agent.
func NewSubAgentExecutor(child *Agent, parts ContextParts, logger *slog.Logger) *SubAgentExecutor {
	return &SubAgentExecutor{child: child, parts: parts, logger: logger}
}

// Execute runs the child agent with the tool call arguments as the user message.
func (se *SubAgentExecutor) Execute(ctx context.Context, call ToolCall) (string, error) {
	var args struct {
		Task    string          `json:"task"`
		Context json.RawMessage `json:"context,omitempty"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return "", fmt.Errorf("invalid sub-agent arguments: %w", err)
	}

	childParts := ContextParts{
		SoulPrompt:  se.parts.SoulPrompt,
		AgentPrompt: se.parts.AgentPrompt,
		Memory:      se.parts.Memory,
		History: []Message{{
			Role:      RoleUser,
			Content:   args.Task,
			Timestamp: time.Now(),
		}},
	}

	if len(args.Context) > 0 {
		childParts.Memory += "\n\n# Delegated Context\n" + string(args.Context)
	}

	result := se.child.Run(ctx, childParts)
	if result.Error != nil {
		return "", fmt.Errorf("sub-agent %q failed: %w", call.Name, result.Error)
	}

	se.logger.Info("sub-agent complete",
		"agent", call.Name,
		"stop_reason", result.StopReason,
		"turns", result.TurnCount,
	)

	return result.Response, nil
}

// RegisterSubAgents converts SubAgentDefs into tools and executors.
func RegisterSubAgents(defs []SubAgentDef, logger *slog.Logger) ([]ToolDef, map[string]ToolExecutor) {
	toolDefs := make([]ToolDef, 0, len(defs))
	executors := make(map[string]ToolExecutor, len(defs))

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"task": {
				"type": "string",
				"description": "The task or question to delegate to this agent"
			},
			"context": {
				"type": "object",
				"description": "Optional additional context for the agent"
			}
		},
		"required": ["task"]
	}`)

	for _, def := range defs {
		toolDefs = append(toolDefs, ToolDef{
			Name:        def.Name,
			Description: def.Description,
			Parameters:  schema,
			DangerLevel: DangerNone,
		})
		executors[def.Name] = NewSubAgentExecutor(def.Agent, def.Parts, logger)
	}

	return toolDefs, executors
}
