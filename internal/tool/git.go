package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	t "github.com/marsstein/marsclaw/internal/types"
)

// GitTool provides safe, read-only git operations.
type GitTool struct {
	WorkDir string
}

type gitArgs struct {
	Subcommand string `json:"subcommand"`
	Args       string `json:"args"`
}

func (g GitTool) Execute(ctx context.Context, call t.ToolCall) (string, error) {
	var args gitArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	allowed := map[string]bool{
		"status": true, "log": true, "diff": true, "blame": true,
		"show": true, "branch": true, "tag": true, "stash list": true,
		"shortlog": true, "rev-parse": true,
	}

	sub := strings.TrimSpace(args.Subcommand)
	if !allowed[sub] {
		return "", fmt.Errorf("git subcommand %q not allowed (read-only: status, log, diff, blame, show, branch, tag, shortlog)", sub)
	}

	cmdArgs := []string{sub}
	if args.Args != "" {
		cmdArgs = append(cmdArgs, strings.Fields(args.Args)...)
	}

	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	if g.WorkDir != "" {
		cmd.Dir = g.WorkDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Sprintf("git error: %s", stderr.String()), nil
		}
		return fmt.Sprintf("git error: %v", err), nil
	}

	return stdout.String(), nil
}

func GitDef() t.ToolDef {
	return t.ToolDef{
		Name:        "git",
		Description: "Run read-only git commands: status, log, diff, blame, show, branch, tag, shortlog. Safe — cannot modify the repo.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"subcommand": {
					"type": "string",
					"enum": ["status", "log", "diff", "blame", "show", "branch", "tag", "shortlog", "rev-parse", "stash list"],
					"description": "Git subcommand to run"
				},
				"args": {
					"type": "string",
					"description": "Additional arguments (e.g., '--oneline -10' for log, 'HEAD~3..HEAD' for diff, '-L 10,20:main.go' for blame)"
				}
			},
			"required": ["subcommand"]
		}`),
		DangerLevel: t.DangerNone,
		ReadOnly:    true,
	}
}
