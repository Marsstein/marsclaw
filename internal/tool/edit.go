package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	t "github.com/marsstein/marsclaw/internal/types"
)

type EditFileTool struct{}

type editFileArgs struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func (EditFileTool) Execute(_ context.Context, call t.ToolCall) (string, error) {
	var args editFileArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	info, err := os.Stat(args.Path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", args.Path, err)
	}

	data, err := os.ReadFile(args.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", args.Path, err)
	}

	content := string(data)
	count := strings.Count(content, args.OldString)

	if count == 0 {
		return fmt.Sprintf("Error: old_string not found in %s", args.Path), nil
	}
	if count > 1 {
		return fmt.Sprintf("Error: old_string appears %d times in %s. Provide more context to make it unique.", count, args.Path), nil
	}

	updated := strings.Replace(content, args.OldString, args.NewString, 1)

	if err := os.WriteFile(args.Path, []byte(updated), info.Mode()); err != nil {
		return "", fmt.Errorf("write %s: %w", args.Path, err)
	}

	return fmt.Sprintf("Replaced 1 occurrence in %s", args.Path), nil
}

func EditFileDef() t.ToolDef {
	return t.ToolDef{
		Name:        "edit_file",
		Description: "Replace an exact string in a file. old_string must appear exactly once. Provide enough context to be unique.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "File path to edit"},
				"old_string": {"type": "string", "description": "Exact string to find and replace (must be unique in file)"},
				"new_string": {"type": "string", "description": "Replacement string"}
			},
			"required": ["path", "old_string", "new_string"]
		}`),
		DangerLevel: t.DangerMedium,
	}
}
