package orchestration

import (
	"context"
	"fmt"
	"time"

	"github.com/marsstein/marsclaw/internal/agent"
	t "github.com/marsstein/marsclaw/internal/types"
)

// PipelineStage defines one agent in a sequential pipeline.
type PipelineStage struct {
	Name  string
	Agent *agent.Agent
	Parts t.ContextParts
}

// RunPipeline chains agents sequentially: output of stage N becomes input of stage N+1.
func RunPipeline(ctx context.Context, stages []PipelineStage, input string) (*t.RunResult, error) {
	if len(stages) == 0 {
		return nil, fmt.Errorf("pipeline requires at least one stage")
	}

	var totalInput, totalOutput int
	var allTrace []t.TraceEntry
	current := input

	for i, stage := range stages {
		parts := t.ContextParts{
			SoulPrompt:  stage.Parts.SoulPrompt,
			AgentPrompt: stage.Parts.AgentPrompt,
			Memory:      stage.Parts.Memory,
			History: []t.Message{{
				Role:      t.RoleUser,
				Content:   current,
				Timestamp: time.Now(),
			}},
		}

		result := stage.Agent.Run(ctx, parts)
		totalInput += result.TotalInput
		totalOutput += result.TotalOutput
		allTrace = append(allTrace, result.Trace...)

		if result.Error != nil {
			return nil, fmt.Errorf("pipeline stage %d (%s) failed: %w", i, stage.Name, result.Error)
		}

		current = result.Response
	}

	return &t.RunResult{
		Response:    current,
		StopReason:  t.StopFinalResponse,
		TurnCount:   len(stages),
		TotalInput:  totalInput,
		TotalOutput: totalOutput,
		Trace:       allTrace,
	}, nil
}
