package agent

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ContextBuilder assembles the prompt sent to the LLM.
// Assembly order: SOUL.md → AGENTS.md → memory → history.
type ContextBuilder struct {
	counter TokenCounter
	config  AgentConfig
	tools   []ToolDef
}

// NewContextBuilder creates a context builder.
func NewContextBuilder(counter TokenCounter, config AgentConfig, tools []ToolDef) *ContextBuilder {
	return &ContextBuilder{counter: counter, config: config, tools: tools}
}

// Build assembles the final message list, fitting within token budget.
func (cb *ContextBuilder) Build(parts ContextParts) []Message {
	maxInput := cb.config.MaxInputTokens
	systemBudget := int(float64(maxInput) * cb.config.SystemPromptBudget)
	historyBudget := int(float64(maxInput) * cb.config.HistoryBudget)

	systemContent := cb.assembleSystemPrompt(parts, systemBudget)

	messages := []Message{{
		Role:      RoleSystem,
		Content:   systemContent,
		Timestamp: time.Now(),
	}}

	trimmed := cb.fitHistory(parts.History, historyBudget)
	messages = append(messages, trimmed...)

	return messages
}

func (cb *ContextBuilder) assembleSystemPrompt(parts ContextParts, budget int) string {
	sections := []string{parts.SoulPrompt, parts.AgentPrompt, parts.Memory}

	var b strings.Builder
	for _, s := range sections {
		if s == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(s)
	}

	result := b.String()

	tokens := cb.counter.CountTokens(
		[]Message{{Role: RoleSystem, Content: result}},
		cb.tools,
	)
	if tokens <= budget {
		return result
	}

	ratio := float64(budget) / float64(tokens)
	cutLen := int(float64(len(result)) * ratio * 0.95)
	if cutLen < 0 {
		cutLen = 0
	}
	// Ensure we don't cut in the middle of a multi-byte rune.
	for cutLen > 0 && !utf8.RuneStart(result[cutLen]) {
		cutLen--
	}
	return result[:cutLen] + "\n\n[System prompt truncated to fit context window]"
}

func (cb *ContextBuilder) fitHistory(history []Message, budget int) []Message {
	if len(history) == 0 {
		return nil
	}

	tokens := cb.counter.CountTokens(history, nil)
	if tokens <= budget {
		return history
	}

	var anchor []Message
	remaining := history
	if history[0].Role == RoleUser {
		anchor = history[:1]
		remaining = history[1:]
	}

	anchorTokens := cb.counter.CountTokens(anchor, nil)
	placeholder := Message{
		Role:      RoleSystem,
		Content:   "[Earlier conversation omitted to fit context window]",
		Timestamp: time.Now(),
	}
	placeholderTokens := cb.counter.CountTokens([]Message{placeholder}, nil)
	available := budget - anchorTokens - placeholderTokens

	if available <= 0 {
		return anchor
	}

	kept := make([]Message, 0, len(remaining))
	used := 0
	for i := len(remaining) - 1; i >= 0; i-- {
		msgTokens := cb.counter.CountTokens([]Message{remaining[i]}, nil)
		if used+msgTokens > available {
			break
		}
		kept = append(kept, remaining[i])
		used += msgTokens
	}

	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}

	if droppedCount := len(remaining) - len(kept); droppedCount > 0 {
		result := make([]Message, 0, len(anchor)+1+len(kept))
		result = append(result, anchor...)
		result = append(result, placeholder)
		result = append(result, kept...)
		return result
	}

	result := make([]Message, 0, len(anchor)+len(kept))
	result = append(result, anchor...)
	result = append(result, kept...)
	return result
}

// TruncateToolResult cuts a tool result to a max length.
func TruncateToolResult(content string, maxLen int) string {
	if maxLen <= 0 || len(content) <= maxLen {
		return content
	}

	headLen := maxLen * 7 / 10
	tailLen := maxLen * 3 / 10
	head := content[:headLen]
	tail := content[len(content)-tailLen:]
	omitted := len(content) - headLen - tailLen

	return fmt.Sprintf("%s\n\n[... %d characters omitted ...]\n\n%s", head, omitted, tail)
}
