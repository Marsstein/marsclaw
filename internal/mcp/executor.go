package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	t "github.com/marsstein/marsclaw/internal/types"
)

// MCPExecutor adapts an MCP client's tools as ToolExecutors.
type MCPExecutor struct {
	client     *Client
	serverName string
}

// NewMCPExecutor creates a tool executor for an MCP server's tools.
func NewMCPExecutor(client *Client) *MCPExecutor {
	return &MCPExecutor{client: client, serverName: client.name}
}

// Execute calls the MCP tool.
func (e *MCPExecutor) Execute(ctx context.Context, call t.ToolCall) (string, error) {
	// Strip server name prefix to get the original tool name.
	originalName := strings.TrimPrefix(call.Name, e.serverName+"_")
	return e.client.CallTool(ctx, originalName, call.Arguments)
}

// RegisterMCPServers connects to all configured MCP servers and returns their tools.
func RegisterMCPServers(ctx context.Context, configs []ServerConfig) ([]t.ToolDef, map[string]t.ToolExecutor, []*Client, error) {
	var allDefs []t.ToolDef
	executors := make(map[string]t.ToolExecutor)
	var clients []*Client

	for _, cfg := range configs {
		client, err := NewClient(ctx, cfg)
		if err != nil {
			// Close already-opened clients on error.
			for _, c := range clients {
				c.Close()
			}
			return nil, nil, nil, fmt.Errorf("mcp server %q: %w", cfg.Name, err)
		}
		clients = append(clients, client)

		exec := NewMCPExecutor(client)
		for _, def := range client.Tools() {
			allDefs = append(allDefs, def)
			executors[def.Name] = exec
		}
	}

	return allDefs, executors, clients, nil
}

// FormatToolArgs formats MCP tool arguments for display.
func FormatToolArgs(args json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return string(args)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}

// Ensure MCPExecutor implements ToolExecutor.
var _ t.ToolExecutor = (*MCPExecutor)(nil)
