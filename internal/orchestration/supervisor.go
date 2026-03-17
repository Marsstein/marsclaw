package orchestration

import (
	"context"
	"log/slog"
	"time"

	"github.com/marsstein/liteclaw/internal/agent"
	t "github.com/marsstein/liteclaw/internal/types"
)

// SupervisorConfig configures a supervisor pattern.
type SupervisorConfig struct {
	Coordinator *agent.Agent
	CoordParts  t.ContextParts
	Specialists []agent.SubAgentDef
	Logger      *slog.Logger
}

// RunSupervisor runs a coordinator that delegates tasks to specialist sub-agents via tool calling.
func RunSupervisor(ctx context.Context, cfg SupervisorConfig, task string) (*t.RunResult, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Register specialists as tools the coordinator can call.
	toolDefs, executors := agent.RegisterSubAgents(cfg.Specialists, logger)
	cfg.Coordinator.AddTools(toolDefs, executors)

	parts := t.ContextParts{
		SoulPrompt:  cfg.CoordParts.SoulPrompt,
		AgentPrompt: cfg.CoordParts.AgentPrompt + "\n\nYou are a supervisor. Delegate tasks to your specialist agents. Synthesize their outputs into a final response.",
		Memory:      cfg.CoordParts.Memory,
		History: []t.Message{{
			Role:      t.RoleUser,
			Content:   task,
			Timestamp: time.Now(),
		}},
	}

	return cfg.Coordinator.Run(ctx, parts), nil
}
