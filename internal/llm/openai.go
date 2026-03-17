package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	t "github.com/marsstein/marsclaw/internal/types"
)

// OpenAIProvider implements types.Provider for OpenAI-compatible APIs.
type OpenAIProvider struct {
	client  *http.Client
	apiKey  string
	baseURL string
	model   string
}

// NewOpenAIProvider creates a provider for OpenAI or any compatible API.
func NewOpenAIProvider(apiKey, baseURL, model string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAIProvider{
		client:  &http.Client{},
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
	}
}

// --- OpenAI request/response types ---

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Tools       []oaiTool    `json:"tools,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
}

type oaiMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []oaiToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type oaiToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function oaiFunction  `json:"function"`
}

type oaiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiTool struct {
	Type     string          `json:"type"`
	Function oaiToolFunction `json:"function"`
}

type oaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
	Usage   oaiUsage    `json:"usage"`
	Model   string      `json:"model"`
}

type oaiChoice struct {
	Message      oaiMessage `json:"message"`
	Delta        oaiMessage `json:"delta"`
	FinishReason string     `json:"finish_reason"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Call sends a synchronous request.
func (p *OpenAIProvider) Call(ctx context.Context, req *t.ProviderRequest) (*t.LLMResponse, error) {
	oaiReq := p.buildRequest(req, false)
	body, err := p.doRequest(ctx, oaiReq)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var resp oaiResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return p.parseResponse(&resp), nil
}

// Stream sends a streaming request.
func (p *OpenAIProvider) Stream(ctx context.Context, req *t.ProviderRequest, events chan<- t.StreamEvent) (*t.LLMResponse, error) {
	defer close(events)

	oaiReq := p.buildRequest(req, true)
	body, err := p.doRequest(ctx, oaiReq)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var textBuilder strings.Builder
	var toolCalls []t.ToolCall
	activeTools := make(map[int]*oaiToolCall)
	var model string
	var inputTokens, outputTokens int

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk oaiResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage.PromptTokens > 0 {
			inputTokens = chunk.Usage.PromptTokens
			outputTokens = chunk.Usage.CompletionTokens
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta

		if delta.Content != "" {
			textBuilder.WriteString(delta.Content)
			events <- t.StreamEvent{Type: t.StreamText, Delta: delta.Content}
		}

		for i, tc := range delta.ToolCalls {
			if tc.ID != "" {
				activeTools[i] = &oaiToolCall{ID: tc.ID, Type: "function", Function: oaiFunction{Name: tc.Function.Name}}
				events <- t.StreamEvent{
					Type:     t.StreamToolStart,
					ToolCall: &t.ToolCall{ID: tc.ID, Name: tc.Function.Name},
				}
			}
			if at, ok := activeTools[i]; ok {
				at.Function.Arguments += tc.Function.Arguments
			}
		}
	}

	for _, tc := range activeTools {
		args := tc.Function.Arguments
		if args == "" {
			args = "{}"
		}
		toolCalls = append(toolCalls, t.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(args),
		})
	}

	return &t.LLMResponse{
		Content:      textBuilder.String(),
		ToolCalls:    toolCalls,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Model:        model,
	}, nil
}

// CountTokens estimates token count.
func (p *OpenAIProvider) CountTokens(messages []t.Message, tools []t.ToolDef) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
		for _, tc := range m.ToolCalls {
			total += len(tc.Arguments) / 4
		}
		if m.ToolResult != nil {
			total += len(m.ToolResult.Content) / 4
		}
	}
	for _, tool := range tools {
		total += len(tool.Description)/4 + len(tool.Parameters)/4
	}
	return total
}

// MaxContextWindow returns the context window size.
func (p *OpenAIProvider) MaxContextWindow() int {
	switch {
	case strings.Contains(p.model, "gpt-4o"):
		return 128_000
	case strings.Contains(p.model, "gpt-4"):
		return 128_000
	default:
		return 128_000
	}
}

func (p *OpenAIProvider) buildRequest(req *t.ProviderRequest, stream bool) *oaiRequest {
	model := req.Model
	if model == "" {
		model = p.model
	}

	var messages []oaiMessage
	for _, m := range req.Messages {
		switch m.Role {
		case t.RoleSystem:
			messages = append(messages, oaiMessage{Role: "system", Content: m.Content})
		case t.RoleUser:
			messages = append(messages, oaiMessage{Role: "user", Content: m.Content})
		case t.RoleAssistant:
			msg := oaiMessage{Role: "assistant", Content: m.Content}
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, oaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: oaiFunction{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				})
			}
			messages = append(messages, msg)
		case t.RoleTool:
			if m.ToolResult != nil {
				messages = append(messages, oaiMessage{
					Role:       "tool",
					Content:    m.ToolResult.Content,
					ToolCallID: m.ToolResult.CallID,
				})
			}
		}
	}

	oaiReq := &oaiRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
		Stream:    stream,
	}

	if req.Temperature > 0 {
		temp := req.Temperature
		oaiReq.Temperature = &temp
	}

	for _, td := range req.Tools {
		oaiReq.Tools = append(oaiReq.Tools, oaiTool{
			Type: "function",
			Function: oaiToolFunction{
				Name:        td.Name,
				Description: td.Description,
				Parameters:  td.Parameters,
			},
		})
	}

	return oaiReq
}

func (p *OpenAIProvider) doRequest(ctx context.Context, oaiReq *oaiRequest) (io.ReadCloser, error) {
	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, classifyError(err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		msg := fmt.Sprintf("OpenAI API error %d: %s", resp.StatusCode, string(errBody))
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			return nil, &RetryableError{StatusCode: resp.StatusCode, Message: msg}
		}
		return nil, fmt.Errorf("%s", msg)
	}

	return resp.Body, nil
}

func (p *OpenAIProvider) parseResponse(resp *oaiResponse) *t.LLMResponse {
	result := &t.LLMResponse{
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		Model:        resp.Model,
	}

	if len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		result.Content = msg.Content
		for _, tc := range msg.ToolCalls {
			result.ToolCalls = append(result.ToolCalls, t.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			})
		}
	}

	return result
}
