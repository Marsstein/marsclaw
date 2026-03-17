package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"

	t "github.com/marsstein/liteclaw/internal/types"
)

// Client connects to an MCP server via stdio (JSON-RPC 2.0).
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	nextID atomic.Int64
	tools  []t.ToolDef
	name   string
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ServerConfig defines how to connect to an MCP server.
type ServerConfig struct {
	Name    string   `json:"name" koanf:"name"`
	Command string   `json:"command" koanf:"command"`
	Args    []string `json:"args" koanf:"args"`
	Env     []string `json:"env,omitempty" koanf:"env"`
}

// NewClient starts an MCP server process and connects via stdio.
func NewClient(ctx context.Context, cfg ServerConfig) (*Client, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = append(cmd.Environ(), cfg.Env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp start %q: %w", cfg.Command, err)
	}

	c := &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		name:   cfg.Name,
	}

	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp initialize: %w", err)
	}

	if err := c.discoverTools(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp discover tools: %w", err)
	}

	return c, nil
}

func (c *Client) initialize(ctx context.Context) error {
	_, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "liteclaw",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return err
	}

	// Send initialized notification (no response expected).
	c.mu.Lock()
	defer c.mu.Unlock()
	notif := jsonRPCRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
	data, _ := json.Marshal(notif)
	data = append(data, '\n')
	_, err = c.stdin.Write(data)
	return err
}

func (c *Client) discoverTools(ctx context.Context) error {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return err
	}

	var resp struct {
		Tools []mcpToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("parse tools: %w", err)
	}

	c.tools = make([]t.ToolDef, 0, len(resp.Tools))
	for _, tool := range resp.Tools {
		c.tools = append(c.tools, t.ToolDef{
			Name:        c.name + "_" + tool.Name,
			Description: fmt.Sprintf("[%s] %s", c.name, tool.Description),
			Parameters:  tool.InputSchema,
			DangerLevel: t.DangerMedium,
		})
	}

	return nil
}

// Tools returns the discovered tool definitions.
func (c *Client) Tools() []t.ToolDef { return c.tools }

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, toolName string, args json.RawMessage) (string, error) {
	var parsedArgs any
	if len(args) > 0 {
		json.Unmarshal(args, &parsedArgs)
	}

	result, err := c.call(ctx, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": parsedArgs,
	})
	if err != nil {
		return "", err
	}

	var toolResult mcpToolResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return string(result), nil
	}

	var output string
	for _, c := range toolResult.Content {
		if c.Type == "text" {
			output += c.Text
		}
	}

	if toolResult.IsError {
		return "", fmt.Errorf("mcp tool error: %s", output)
	}

	return output, nil
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read response lines until we get a matching ID.
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // skip non-JSON lines (notifications, logs)
		}

		if resp.ID != id {
			continue // skip notifications or mismatched IDs
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		return resp.Result, nil
	}
}

// Close stops the MCP server process.
func (c *Client) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}
