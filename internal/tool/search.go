package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	t "github.com/marsstein/liteclaw/internal/types"
)

type SearchTool struct{}

type searchMatch struct {
	file string
	line int
	text string
}

type searchArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
	MaxResults int `json:"max_results"`
}

func (SearchTool) Execute(ctx context.Context, call t.ToolCall) (string, error) {
	var args searchArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %w", args.Pattern, err)
	}

	root := args.Path
	if root == "" {
		root = "."
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}

	var matches []searchMatch
	count := 0

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			if d != nil && d.IsDir() && (d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == "__pycache__") {
				return filepath.SkipDir
			}
			return nil
		}
		if count >= maxResults {
			return filepath.SkipAll
		}

		// Glob filter.
		if args.Glob != "" {
			matched, _ := filepath.Match(args.Glob, d.Name())
			if !matched {
				return nil
			}
		}

		// Skip binary files.
		if isBinaryExt(d.Name()) {
			return nil
		}

		found := scanFile(path, root, re, maxResults-count)
		matches = append(matches, found...)
		count += len(found)
		if count >= maxResults {
			return filepath.SkipAll
		}
		return nil
	})

	if len(matches) == 0 {
		return fmt.Sprintf("No matches for %q", args.Pattern), nil
	}

	var b strings.Builder
	for _, m := range matches {
		text := m.text
		if len(text) > 200 {
			text = text[:200] + "..."
		}
		fmt.Fprintf(&b, "%s:%d: %s\n", m.file, m.line, strings.TrimSpace(text))
	}
	if count >= maxResults {
		fmt.Fprintf(&b, "\n[Truncated at %d results]\n", maxResults)
	}
	return b.String(), nil
}

func scanFile(path, root string, re *regexp.Regexp, limit int) []searchMatch {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var matches []searchMatch
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if re.MatchString(line) {
			rel, _ := filepath.Rel(root, path)
			matches = append(matches, searchMatch{file: rel, line: lineNum, text: line})
			if len(matches) >= limit {
				break
			}
		}
	}
	return matches
}

func isBinaryExt(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".ico", ".svg",
		".pdf", ".zip", ".tar", ".gz", ".bz2", ".xz",
		".exe", ".dll", ".so", ".dylib", ".o", ".a",
		".wasm", ".bin", ".dat", ".db", ".sqlite":
		return true
	}
	return false
}

func SearchDef() t.ToolDef {
	return t.ToolDef{
		Name:        "search",
		Description: "Search file contents using regex. Walks the directory tree, skipping hidden/binary/vendor dirs.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pattern": {"type": "string", "description": "Regex pattern to search for"},
				"path": {"type": "string", "description": "Root directory to search. Default: current directory"},
				"glob": {"type": "string", "description": "Filename glob filter (e.g. '*.go')"},
				"max_results": {"type": "integer", "description": "Max matches to return. Default: 50"}
			},
			"required": ["pattern"]
		}`),
		DangerLevel: t.DangerNone,
		ReadOnly:    true,
	}
}
