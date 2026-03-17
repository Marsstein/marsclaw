package orchestration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/marsstein/liteclaw/internal/agent"
	t "github.com/marsstein/liteclaw/internal/types"
)

// ParallelAgent defines one agent in a fan-out group.
type ParallelAgent struct {
	Name  string
	Agent *agent.Agent
	Parts t.ContextParts
}

// ParallelConfig configures a parallel fan-out execution.
type ParallelConfig struct {
	Agents     []ParallelAgent
	Aggregator *agent.Agent
	AggParts   t.ContextParts
}

// RunParallel fans out a task to N agents concurrently, then aggregates results.
func RunParallel(ctx context.Context, cfg ParallelConfig, task string) (*t.RunResult, error) {
	if len(cfg.Agents) == 0 {
		return nil, fmt.Errorf("parallel requires at least one agent")
	}

	type agentResult struct {
		name   string
		result *t.RunResult
		err    error
	}

	results := make([]agentResult, len(cfg.Agents))
	var wg sync.WaitGroup

	for i, pa := range cfg.Agents {
		wg.Add(1)
		go func(idx int, pa ParallelAgent) {
			defer wg.Done()
			parts := t.ContextParts{
				SoulPrompt:  pa.Parts.SoulPrompt,
				AgentPrompt: pa.Parts.AgentPrompt,
				Memory:      pa.Parts.Memory,
				History: []t.Message{{
					Role:      t.RoleUser,
					Content:   task,
					Timestamp: time.Now(),
				}},
			}
			r := pa.Agent.Run(ctx, parts)
			results[idx] = agentResult{name: pa.Name, result: r, err: r.Error}
		}(i, pa)
	}

	wg.Wait()

	// Collect responses.
	var totalInput, totalOutput int
	var allTrace []t.TraceEntry
	var responses strings.Builder

	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(&responses, "## %s\n[Error: %v]\n\n", r.name, r.err)
			continue
		}
		totalInput += r.result.TotalInput
		totalOutput += r.result.TotalOutput
		allTrace = append(allTrace, r.result.Trace...)
		fmt.Fprintf(&responses, "## %s\n%s\n\n", r.name, r.result.Response)
	}

	// If no aggregator, return combined responses.
	if cfg.Aggregator == nil {
		return &t.RunResult{
			Response:    responses.String(),
			StopReason:  t.StopFinalResponse,
			TurnCount:   1,
			TotalInput:  totalInput,
			TotalOutput: totalOutput,
			Trace:       allTrace,
		}, nil
	}

	// Run aggregator with all responses as context.
	aggParts := t.ContextParts{
		SoulPrompt:  cfg.AggParts.SoulPrompt,
		AgentPrompt: cfg.AggParts.AgentPrompt,
		Memory:      cfg.AggParts.Memory,
		History: []t.Message{{
			Role:      t.RoleUser,
			Content:   fmt.Sprintf("Task: %s\n\nResponses from agents:\n\n%s\n\nSynthesize these into a single response.", task, responses.String()),
			Timestamp: time.Now(),
		}},
	}

	aggResult := cfg.Aggregator.Run(ctx, aggParts)
	totalInput += aggResult.TotalInput
	totalOutput += aggResult.TotalOutput
	allTrace = append(allTrace, aggResult.Trace...)

	return &t.RunResult{
		Response:    aggResult.Response,
		StopReason:  aggResult.StopReason,
		TurnCount:   len(cfg.Agents) + 1,
		TotalInput:  totalInput,
		TotalOutput: totalOutput,
		Trace:       allTrace,
	}, aggResult.Error
}
