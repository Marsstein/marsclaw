package llm

import (
	"fmt"
	"sync"
)

// ModelPricing defines per-token costs in microdollars (1 USD = 1_000_000).
type ModelPricing struct {
	InputPerMToken  int64 // microdollars per million input tokens
	OutputPerMToken int64 // microdollars per million output tokens
}

// Known model pricing (microdollars per million tokens).
var defaultPricing = map[string]ModelPricing{
	"claude-sonnet-4-20250514":    {InputPerMToken: 3_000_000, OutputPerMToken: 15_000_000},
	"claude-haiku-4-20250514":     {InputPerMToken: 800_000, OutputPerMToken: 4_000_000},
	"claude-opus-4-20250514":      {InputPerMToken: 15_000_000, OutputPerMToken: 75_000_000},
	"gpt-4o":                      {InputPerMToken: 2_500_000, OutputPerMToken: 10_000_000},
	"gpt-4o-mini":                 {InputPerMToken: 150_000, OutputPerMToken: 600_000},
}

// CostTracker tracks cumulative costs in microdollars.
type CostTracker struct {
	mu      sync.Mutex
	pricing map[string]ModelPricing

	// Cumulative costs in microdollars.
	sessionCost int64
	dailyCost   int64

	// Budget limits in microdollars (0 = no limit).
	dailyLimit   int64
	monthlyLimit int64
}

// NewCostTracker creates a cost tracker with default pricing.
func NewCostTracker() *CostTracker {
	p := make(map[string]ModelPricing, len(defaultPricing))
	for k, v := range defaultPricing {
		p[k] = v
	}
	return &CostTracker{pricing: p}
}

// SetDailyLimit sets a daily cost limit in dollars.
func (ct *CostTracker) SetDailyLimit(dollars float64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.dailyLimit = int64(dollars * 1_000_000)
}

// Record logs token usage and returns the cost in microdollars.
func (ct *CostTracker) Record(model string, inputTokens, outputTokens int) int64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	pricing, ok := ct.pricing[model]
	if !ok {
		return 0
	}

	inputCost := int64(inputTokens) * pricing.InputPerMToken / 1_000_000
	outputCost := int64(outputTokens) * pricing.OutputPerMToken / 1_000_000
	cost := inputCost + outputCost

	ct.sessionCost += cost
	ct.dailyCost += cost

	return cost
}

// SessionCost returns the session total in dollars.
func (ct *CostTracker) SessionCost() float64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return float64(ct.sessionCost) / 1_000_000
}

// DailyCost returns the daily total in dollars.
func (ct *CostTracker) DailyCost() float64 {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return float64(ct.dailyCost) / 1_000_000
}

// OverBudget returns true if the daily limit has been exceeded.
func (ct *CostTracker) OverBudget() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.dailyLimit > 0 && ct.dailyCost >= ct.dailyLimit
}

// FormatCostLine returns a cost display string like Claude Code's inline display.
func (ct *CostTracker) FormatCostLine(model string, inputTokens, outputTokens int) string {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	session := float64(ct.sessionCost) / 1_000_000
	return fmt.Sprintf("── %s │ %s in / %s out │ $%.3f session ──",
		model,
		formatTokenCount(inputTokens),
		formatTokenCount(outputTokens),
		session,
	)
}

func formatTokenCount(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
