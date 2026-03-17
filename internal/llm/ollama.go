package llm

import (
	"context"

	t "github.com/marsstein/liteclaw/internal/types"
)

// OllamaProvider implements types.Provider using Ollama's OpenAI-compatible API.
type OllamaProvider struct {
	inner *OpenAIProvider
}

// NewOllamaProvider creates a provider that talks to a local Ollama instance.
func NewOllamaProvider(baseURL, model string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	return &OllamaProvider{
		inner: NewOpenAIProvider("ollama", baseURL, model),
	}
}

func (p *OllamaProvider) Call(ctx context.Context, req *t.ProviderRequest) (*t.LLMResponse, error) {
	return p.inner.Call(ctx, req)
}

func (p *OllamaProvider) Stream(ctx context.Context, req *t.ProviderRequest, events chan<- t.StreamEvent) (*t.LLMResponse, error) {
	return p.inner.Stream(ctx, req, events)
}

func (p *OllamaProvider) CountTokens(messages []t.Message, tools []t.ToolDef) int {
	return p.inner.CountTokens(messages, tools)
}

func (p *OllamaProvider) MaxContextWindow() int {
	return 32_000 // conservative default for local models
}
