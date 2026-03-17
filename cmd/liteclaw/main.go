package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/marsstein/liteclaw/internal/agent"
	"github.com/marsstein/liteclaw/internal/config"
	"github.com/marsstein/liteclaw/internal/llm"
	"github.com/marsstein/liteclaw/internal/security"
	"github.com/marsstein/liteclaw/internal/terminal"
	"github.com/marsstein/liteclaw/internal/tool"
	t "github.com/marsstein/liteclaw/internal/types"
)

var (
	version = "dev"
	commit  = "none"
)

type CLI struct {
	Config  string           `help:"Path to config file." type:"path" short:"c"`
	Model   string           `help:"LLM model to use." short:"m"`
	Verbose bool             `help:"Enable debug logging." short:"v"`
	Version kong.VersionFlag `help:"Print version."`

	Chat ChatCmd `cmd:"" default:"withargs" help:"Chat with LiteClaw (interactive or single prompt)."`
}

type ChatCmd struct {
	Prompt []string `arg:"" optional:"" help:"Prompt to send. Omit for interactive mode."`
}

func main() {
	cli := CLI{}
	ctx := kong.Parse(&cli,
		kong.Name("liteclaw"),
		kong.Description("Lightweight, secure, multi-agent AI runtime."),
		kong.Vars{"version": fmt.Sprintf("liteclaw %s (%s)", version, commit)},
	)

	if err := run(ctx, &cli); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

const defaultSoul = `You are LiteClaw, a fast and capable AI coding assistant.

Rules:
- Be concise and direct. Lead with the answer.
- Use tools to read files before editing them.
- Use edit_file for surgical changes, write_file for new files.
- Run shell commands to verify your work (tests, build).
- Never guess file contents — always read first.
- When you're done, say what you did in 1-2 sentences.`

func run(_ *kong.Context, cli *CLI) error {
	level := slog.LevelWarn
	if cli.Verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(cli.Config)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	model := cli.Model
	if model == "" && cfg.Providers.Anthropic != nil {
		model = cfg.Providers.Anthropic.DefaultModel
	}
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	provider, err := createProvider(cfg, model)
	if err != nil {
		return err
	}

	cost := llm.NewCostTracker()
	if cfg.Cost.DailyBudget > 0 {
		cost.SetDailyLimit(cfg.Cost.DailyBudget)
	}

	// Register built-in tools.
	cwd, _ := os.Getwd()
	registry := tool.DefaultRegistry(cwd)

	agentCfg := agent.DefaultAgentConfig()
	agentCfg.Model = model
	agentCfg.EnableStreaming = true

	allowedDirs := cfg.Security.AllowedDirs
	if len(allowedDirs) == 0 && cfg.Security.PathTraversalGuard {
		allowedDirs = []string{cwd}
	}

	safetyCfg := security.SafetyConfig{
		StrictApproval:     cfg.Security.StrictApproval,
		ScanCredentials:    cfg.Security.ScanCredentials,
		PathTraversalGuard: cfg.Security.PathTraversalGuard,
		AllowedDirs:        allowedDirs,
	}

	approvalFn := func(call t.ToolCall, reason string) bool {
		fmt.Fprintf(os.Stderr, "\n%s⚠ Tool %q requires approval (%s)%s\n", "\033[33m", call.Name, reason, "\033[0m")
		fmt.Fprintf(os.Stderr, "  Arguments: %s\n", string(call.Arguments))
		fmt.Fprintf(os.Stderr, "Allow? [y/N] ")
		var response string
		fmt.Scanln(&response)
		return strings.EqualFold(response, "y") || strings.EqualFold(response, "yes")
	}

	safetyChecker := security.NewSafetyChecker(safetyCfg, registry.Defs(), approvalFn)

	a := agent.New(
		provider,
		agentCfg,
		registry.Executors(),
		registry.Defs(),
		agent.WithLogger(logger),
		agent.WithCostTracker(cost),
		agent.WithSafety(safetyChecker),
		agent.WithStreamHandler(streamHandler()),
	)

	// Interactive mode.
	chatCmd := cli.Chat
	if len(chatCmd.Prompt) == 0 {
		sess := terminal.NewSession(a, cost, model, defaultSoul)
		return sess.Run(context.Background())
	}

	// Single-shot mode.
	prompt := strings.Join(chatCmd.Prompt, " ")

	runCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	parts := t.ContextParts{
		SoulPrompt: defaultSoul,
		History:    []t.Message{{Role: t.RoleUser, Content: prompt}},
	}

	result := a.Run(runCtx, parts)
	fmt.Println()

	if cfg.Cost.InlineDisplay {
		fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m\n", cost.FormatCostLine(model, result.TotalInput, result.TotalOutput))
	}

	if result.Error != nil {
		return result.Error
	}
	return nil
}

func streamHandler() func(t.StreamEvent) {
	return func(ev t.StreamEvent) {
		switch ev.Type {
		case t.StreamText:
			fmt.Print(ev.Delta)
		case t.StreamToolStart:
			if ev.ToolCall != nil {
				fmt.Fprintf(os.Stderr, "\033[33m⚡ %s\033[0m\n", ev.ToolCall.Name)
			}
		case t.StreamToolDone:
			if ev.ToolCall != nil {
				fmt.Fprintf(os.Stderr, "\033[32m✓ %s\033[0m\n", ev.ToolCall.Name)
			}
		}
	}
}

func createProvider(cfg *config.Config, model string) (t.Provider, error) {
	switch cfg.Providers.Default {
	case "anthropic", "":
		envKey := "ANTHROPIC_API_KEY"
		if cfg.Providers.Anthropic != nil && cfg.Providers.Anthropic.APIKeyEnv != "" {
			envKey = cfg.Providers.Anthropic.APIKeyEnv
		}
		apiKey := os.Getenv(envKey)
		if apiKey == "" {
			return nil, fmt.Errorf("set %s environment variable", envKey)
		}
		return llm.NewAnthropicProvider(apiKey, model), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %q", cfg.Providers.Default)
	}
}
