package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	t "github.com/marsstein/liteclaw/internal/types"
)

type WriteFileTool struct{}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (WriteFileTool) Execute(_ context.Context, call t.ToolCall) (string, error) {
	var args writeFileArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	dir := filepath.Dir(args.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create directory %s: %w", dir, err)
	}

	perm := os.FileMode(0o644)
	if info, err := os.Stat(args.Path); err == nil {
		perm = info.Mode()
	}

	if err := os.WriteFile(args.Path, []byte(args.Content), perm); err != nil {
		return "", fmt.Errorf("write %s: %w", args.Path, err)
	}

	return fmt.Sprintf("Written %d bytes to %s", len(args.Content), args.Path), nil
}

func WriteFileDef() t.ToolDef {
	return t.ToolDef{
		Name:        "write_file",
		Description: "Write content to a file. Creates parent directories if needed. Overwrites existing files.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "File path to write to"},
				"content": {"type": "string", "description": "Content to write"}
			},
			"required": ["path", "content"]
		}`),
		DangerLevel: t.DangerMedium,
	}
}
