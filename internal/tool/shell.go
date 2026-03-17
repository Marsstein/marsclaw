package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	t "github.com/marsstein/marsclaw/internal/types"
)

type ShellTool struct {
	WorkDir string
}

type shellArgs struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

func (s ShellTool) Execute(ctx context.Context, call t.ToolCall) (string, error) {
	var args shellArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	timeout := time.Duration(args.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", args.Command)
	if s.WorkDir != "" {
		cmd.Dir = s.WorkDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var b strings.Builder
	if stdout.Len() > 0 {
		b.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("STDERR:\n")
		b.WriteString(stderr.String())
	}

	if err != nil {
		if cmdCtx.Err() != nil {
			return fmt.Sprintf("Command timed out after %s\n%s", timeout, b.String()), nil
		}
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return fmt.Sprintf("Exit code: %d\n%s", exitCode, b.String()), nil
	}

	return b.String(), nil
}

func ShellDef() t.ToolDef {
	return t.ToolDef{
		Name:        "shell",
		Description: "Execute a shell command and return its output. Use for running build commands, git, tests, etc.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {"type": "string", "description": "Shell command to execute"},
				"timeout": {"type": "integer", "description": "Timeout in seconds. Default: 30, max: 300"}
			},
			"required": ["command"]
		}`),
		DangerLevel: t.DangerHigh,
	}
}
