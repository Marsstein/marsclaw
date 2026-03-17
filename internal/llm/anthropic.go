package llm

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	t "github.com/marsstein/liteclaw/internal/types"
)

// AnthropicProvider implements types.Provider using the Anthropic API.
type AnthropicProvider struct {
	client *anthropic.Client
	model  string
}

// NewAnthropicProvider creates a provider with the given API key and model.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicProvider{client: &client, model: model}
}

// Call sends a synchronous request to the Anthropic API.
func (p *AnthropicProvider) Call(ctx context.Context, req *t.ProviderRequest) (*t.LLMResponse, error) {
	params := p.buildParams(req)

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, classifyError(err)
	}

	return p.parseResponse(msg), nil
}

// Stream sends a streaming request to the Anthropic API.
func (p *AnthropicProvider) Stream(ctx context.Context, req *t.ProviderRequest, events chan<- t.StreamEvent) (*t.LLMResponse, error) {
	defer close(events)

	params := p.buildParams(req)
	stream := p.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var textBuilder strings.Builder
	var toolCalls []t.ToolCall
	var inputTokens, outputTokens int64
	var model string

	type toolBuilder struct {
		id   string
		name string
		json strings.Builder
	}
	activeTools := make(map[int64]*toolBuilder)

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case "message_start":
			inputTokens = event.Message.Usage.InputTokens
			model = string(event.Message.Model)

		case "content_block_start":
			cb := event.ContentBlock
			switch cb.Type {
			case "tool_use":
				activeTools[event.Index] = &toolBuilder{id: cb.ID, name: cb.Name}
				events <- t.StreamEvent{
					Type:     t.StreamToolStart,
					ToolCall: &t.ToolCall{ID: cb.ID, Name: cb.Name},
				}
			}

		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				text := event.Delta.Text
				textBuilder.WriteString(text)
				events <- t.StreamEvent{Type: t.StreamText, Delta: text}
			case "input_json_delta":
				if tb, ok := activeTools[event.Index]; ok {
					tb.json.WriteString(event.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			if tb, ok := activeTools[event.Index]; ok {
				raw := tb.json.String()
				if raw == "" {
					raw = "{}"
				}
				toolCalls = append(toolCalls, t.ToolCall{
					ID:        tb.id,
					Name:      tb.name,
					Arguments: json.RawMessage(raw),
				})
				delete(activeTools, event.Index)
			}

		case "message_delta":
			outputTokens = event.Usage.OutputTokens
		}
	}

	if err := stream.Err(); err != nil {
		return nil, classifyError(err)
	}

	return &t.LLMResponse{
		Content:      textBuilder.String(),
		ToolCalls:    toolCalls,
		InputTokens:  int(inputTokens),
		OutputTokens: int(outputTokens),
		Model:        model,
	}, nil
}

// CountTokens estimates token count (4 chars/token heuristic).
func (p *AnthropicProvider) CountTokens(messages []t.Message, tools []t.ToolDef) int {
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

// MaxContextWindow returns the context window for the configured model.
func (p *AnthropicProvider) MaxContextWindow() int {
	return 200_000
}

func (p *AnthropicProvider) buildParams(req *t.ProviderRequest) anthropic.MessageNewParams {
	model := req.Model
	if model == "" {
		model = p.model
	}

	var system []anthropic.TextBlockParam
	var messages []anthropic.MessageParam

	for _, m := range req.Messages {
		switch m.Role {
		case t.RoleSystem:
			system = append(system, anthropic.TextBlockParam{Text: m.Content})

		case t.RoleUser:
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content),
			))

		case t.RoleAssistant:
			var blocks []anthropic.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				var input any
				if len(tc.Arguments) > 0 {
					if err := json.Unmarshal(tc.Arguments, &input); err != nil {
						input = map[string]any{}
					}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Name))
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			}

		case t.RoleTool:
			if m.ToolResult != nil {
				messages = append(messages, anthropic.NewUserMessage(
					anthropic.NewToolResultBlock(m.ToolResult.CallID, m.ToolResult.Content, m.ToolResult.IsError),
				))
			}
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		Messages:  messages,
		MaxTokens: int64(req.MaxTokens),
	}

	if len(system) > 0 {
		params.System = system
	}

	if req.Temperature > 0 {
		params.Temperature = anthropic.Float(req.Temperature)
	}

	if len(req.Stop) > 0 {
		params.StopSequences = req.Stop
	}

	if len(req.Tools) > 0 {
		var tools []anthropic.ToolUnionParam
		for _, td := range req.Tools {
			var props any
			if len(td.Parameters) > 0 {
				if err := json.Unmarshal(td.Parameters, &props); err != nil {
					props = map[string]any{"type": "object"}
				}
			}

			tools = append(tools, anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name:        td.Name,
					Description: anthropic.String(td.Description),
					InputSchema: anthropic.ToolInputSchemaParam{Properties: props},
				},
			})
		}
		params.Tools = tools
	}

	return params
}

func (p *AnthropicProvider) parseResponse(msg *anthropic.Message) *t.LLMResponse {
	resp := &t.LLMResponse{
		InputTokens:  int(msg.Usage.InputTokens),
		OutputTokens: int(msg.Usage.OutputTokens),
		Model:        string(msg.Model),
	}

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			resp.Content += block.Text
		case "tool_use":
			raw, _ := json.Marshal(block.Input)
			resp.ToolCalls = append(resp.ToolCalls, t.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: raw,
			})
		}
	}

	return resp
}

func classifyError(err error) error {
	msg := err.Error()
	for _, pattern := range []string{"429", "503", "529", "overloaded", "rate limit"} {
		if strings.Contains(msg, pattern) {
			return &RetryableError{Message: msg}
		}
	}
	return err
}
