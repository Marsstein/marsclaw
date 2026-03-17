package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	t "github.com/marsstein/liteclaw/internal/types"
)

type ReadFileTool struct{}

type readFileArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

const maxReadFileSize = 10 * 1024 * 1024 // 10MB

func (ReadFileTool) Execute(_ context.Context, call t.ToolCall) (string, error) {
	var args readFileArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	info, err := os.Stat(args.Path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", args.Path, err)
	}
	if info.Size() > maxReadFileSize {
		return fmt.Sprintf("Error: file %s is %d bytes (max %d). Use offset/limit for large files.", args.Path, info.Size(), maxReadFileSize), nil
	}

	data, err := os.ReadFile(args.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", args.Path, err)
	}

	lines := strings.Split(string(data), "\n")

	offset := args.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(lines) {
		return fmt.Sprintf("File has %d lines, offset %d is beyond end.", len(lines), offset), nil
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 2000
	}

	end := offset + limit
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i := offset; i < end; i++ {
		fmt.Fprintf(&b, "%4d│ %s\n", i+1, lines[i])
	}

	if end < len(lines) {
		fmt.Fprintf(&b, "\n[... %d more lines ...]\n", len(lines)-end)
	}

	return b.String(), nil
}

func ReadFileDef() t.ToolDef {
	return t.ToolDef{
		Name:        "read_file",
		Description: "Read a file from the filesystem. Returns numbered lines. Use offset/limit for large files.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute or relative file path"},
				"offset": {"type": "integer", "description": "Line offset to start from (0-based). Default: 0"},
				"limit": {"type": "integer", "description": "Max lines to return. Default: 2000"}
			},
			"required": ["path"]
		}`),
		DangerLevel: t.DangerNone,
		ReadOnly:    true,
	}
}
