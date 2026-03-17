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

// Debater defines one participant in a debate.
type Debater struct {
	Name     string
	Position string // e.g., "argue for X", "argue against X"
	Agent    *agent.Agent
	Parts    t.ContextParts
}

// DebateConfig configures a multi-round debate with a judge.
type DebateConfig struct {
	Debaters []Debater
	Judge    *agent.Agent
	JudgeParts t.ContextParts
	Rounds   int
}

// RunDebate runs multiple debate rounds, then has a judge synthesize.
func RunDebate(ctx context.Context, cfg DebateConfig, topic string) (*t.RunResult, error) {
	if len(cfg.Debaters) < 2 {
		return nil, fmt.Errorf("debate requires at least 2 debaters")
	}
	rounds := cfg.Rounds
	if rounds <= 0 {
		rounds = 2
	}

	var totalInput, totalOutput int
	var allTrace []t.TraceEntry
	var transcript strings.Builder

	fmt.Fprintf(&transcript, "# Debate: %s\n\n", topic)

	previousArgs := ""

	for round := 1; round <= rounds; round++ {
		fmt.Fprintf(&transcript, "## Round %d\n\n", round)

		type debaterResult struct {
			name     string
			response string
			err      error
			result   *t.RunResult
		}
		results := make([]debaterResult, len(cfg.Debaters))
		var wg sync.WaitGroup

		for i, d := range cfg.Debaters {
			wg.Add(1)
			go func(idx int, d Debater) {
				defer wg.Done()

				prompt := fmt.Sprintf("Topic: %s\nYour position: %s", topic, d.Position)
				if previousArgs != "" {
					prompt += fmt.Sprintf("\n\nPrevious round arguments:\n%s\n\nRespond to the other debaters' points.", previousArgs)
				}

				parts := t.ContextParts{
					SoulPrompt:  d.Parts.SoulPrompt,
					AgentPrompt: d.Parts.AgentPrompt,
					Memory:      d.Parts.Memory,
					History: []t.Message{{
						Role:      t.RoleUser,
						Content:   prompt,
						Timestamp: time.Now(),
					}},
				}

				r := d.Agent.Run(ctx, parts)
				results[idx] = debaterResult{name: d.Name, response: r.Response, err: r.Error, result: r}
			}(i, d)
		}

		wg.Wait()

		var roundArgs strings.Builder
		for _, r := range results {
			if r.err != nil {
				fmt.Fprintf(&transcript, "### %s\n[Error: %v]\n\n", r.name, r.err)
				continue
			}
			totalInput += r.result.TotalInput
			totalOutput += r.result.TotalOutput
			allTrace = append(allTrace, r.result.Trace...)
			fmt.Fprintf(&transcript, "### %s\n%s\n\n", r.name, r.response)
			fmt.Fprintf(&roundArgs, "**%s**: %s\n\n", r.name, r.response)
		}
		previousArgs = roundArgs.String()
	}

	// Judge synthesizes.
	judgeParts := t.ContextParts{
		SoulPrompt:  cfg.JudgeParts.SoulPrompt,
		AgentPrompt: cfg.JudgeParts.AgentPrompt + "\n\nYou are the judge. Evaluate the arguments from all debaters and provide a balanced synthesis with your verdict.",
		Memory:      cfg.JudgeParts.Memory,
		History: []t.Message{{
			Role:      t.RoleUser,
			Content:   transcript.String() + "\n\nProvide your verdict and synthesis.",
			Timestamp: time.Now(),
		}},
	}

	judgeResult := cfg.Judge.Run(ctx, judgeParts)
	totalInput += judgeResult.TotalInput
	totalOutput += judgeResult.TotalOutput
	allTrace = append(allTrace, judgeResult.Trace...)

	return &t.RunResult{
		Response:    judgeResult.Response,
		StopReason:  judgeResult.StopReason,
		TurnCount:   cfg.Rounds*len(cfg.Debaters) + 1,
		TotalInput:  totalInput,
		TotalOutput: totalOutput,
		Trace:       allTrace,
	}, judgeResult.Error
}
