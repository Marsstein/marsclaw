package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	t "github.com/marsstein/liteclaw/internal/types"
)

type ListFilesTool struct{}

type listFilesArgs struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
	MaxDepth int   `json:"max_depth"`
}

func (ListFilesTool) Execute(_ context.Context, call t.ToolCall) (string, error) {
	var args listFilesArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	root := args.Path
	if root == "" {
		root = "."
	}

	maxDepth := args.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}

	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Sprintf("%s is not a directory", root), nil
	}

	rootDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))
	var results []string
	count := 0
	maxFiles := 500

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if count >= maxFiles {
			return filepath.SkipAll
		}

		depth := strings.Count(filepath.Clean(path), string(filepath.Separator)) - rootDepth
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden dirs (except root).
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}

		// Pattern matching.
		if args.Pattern != "" {
			matched, _ := filepath.Match(args.Pattern, d.Name())
			if !matched && !d.IsDir() {
				return nil
			}
		}

		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}

		prefix := ""
		if d.IsDir() {
			prefix = "📁 "
		}

		results = append(results, fmt.Sprintf("%s%s", prefix, rel))
		count++
		return nil
	})

	if len(results) == 0 {
		return "No files found.", nil
	}

	var b strings.Builder
	for _, r := range results {
		b.WriteString(r)
		b.WriteString("\n")
	}
	if count >= maxFiles {
		fmt.Fprintf(&b, "\n[Truncated at %d entries]\n", maxFiles)
	}
	return b.String(), nil
}

func ListFilesDef() t.ToolDef {
	return t.ToolDef{
		Name:        "list_files",
		Description: "List files in a directory, optionally filtered by glob pattern. Shows directory tree up to max_depth.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Directory path. Default: current directory"},
				"pattern": {"type": "string", "description": "Glob pattern to filter files (e.g. '*.go', '*.ts')"},
				"max_depth": {"type": "integer", "description": "Max directory depth. Default: 3"}
			},
			"required": []
		}`),
		DangerLevel: t.DangerNone,
		ReadOnly:    true,
	}
}
