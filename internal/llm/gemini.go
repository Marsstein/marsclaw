package llm

import (
	"context"
	"strings"

	t "github.com/marsstein/marsclaw/internal/types"
)

// GeminiProvider implements types.Provider using Google's OpenAI-compatible API.
type GeminiProvider struct {
	inner *OpenAIProvider
	model string
}

// NewGeminiProvider creates a provider that talks to Google Gemini via its
// OpenAI-compatible endpoint. Uses your Google Cloud API key — burns GCP credits,
// not a separate bill.
func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	return &GeminiProvider{
		inner: NewOpenAIProvider(apiKey, "https://generativelanguage.googleapis.com/v1beta/openai", model),
		model: model,
	}
}

func (p *GeminiProvider) Call(ctx context.Context, req *t.ProviderRequest) (*t.LLMResponse, error) {
	return p.inner.Call(ctx, req)
}

func (p *GeminiProvider) Stream(ctx context.Context, req *t.ProviderRequest, events chan<- t.StreamEvent) (*t.LLMResponse, error) {
	return p.inner.Stream(ctx, req, events)
}

func (p *GeminiProvider) CountTokens(messages []t.Message, tools []t.ToolDef) int {
	return p.inner.CountTokens(messages, tools)
}

func (p *GeminiProvider) MaxContextWindow() int {
	switch {
	case strings.Contains(p.model, "gemini-2.5-pro"):
		return 1_048_576
	case strings.Contains(p.model, "gemini-2.5-flash"):
		return 1_048_576
	case strings.Contains(p.model, "gemini-2.0-flash"):
		return 1_048_576
	default:
		return 1_048_576
	}
}
