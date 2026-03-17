package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/marsstein/liteclaw/internal/agent"
	"github.com/marsstein/liteclaw/internal/config"
	"github.com/marsstein/liteclaw/internal/discord"
	"github.com/marsstein/liteclaw/internal/hooks"
	"github.com/marsstein/liteclaw/internal/llm"
	"github.com/marsstein/liteclaw/internal/mcp"
	"github.com/marsstein/liteclaw/internal/memory"
	"github.com/marsstein/liteclaw/internal/scheduler"
	"github.com/marsstein/liteclaw/internal/security"
	"github.com/marsstein/liteclaw/internal/server"
	"github.com/marsstein/liteclaw/internal/setup"
	"github.com/marsstein/liteclaw/internal/slack"
	"github.com/marsstein/liteclaw/internal/store"
	tgbot "github.com/marsstein/liteclaw/internal/telegram"
	"github.com/marsstein/liteclaw/internal/terminal"
	"github.com/marsstein/liteclaw/internal/tool"
	t "github.com/marsstein/liteclaw/internal/types"
	"github.com/marsstein/liteclaw/internal/whatsapp"
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

	Chat     ChatCmd     `cmd:"" default:"withargs" help:"Chat with LiteClaw (interactive or single prompt)."`
	Serve    ServeCmd    `cmd:"" help:"Start the HTTP server with Web UI."`
	Telegram TelegramCmd `cmd:"" help:"Run as a Telegram bot."`
	Discord  DiscordCmd  `cmd:"" help:"Run as a Discord bot."`
	Slack    SlackCmd    `cmd:"" help:"Run as a Slack bot."`
	Init     InitCmd     `cmd:"" help:"Interactive setup wizard."`
}

type ChatCmd struct {
	Prompt []string `arg:"" optional:"" help:"Prompt to send. Omit for interactive mode."`
}

type ServeCmd struct {
	Addr string `help:"Listen address." default:":8080" short:"a"`
}

type TelegramCmd struct {
	Token string `help:"Telegram bot token." env:"TELEGRAM_BOT_TOKEN" required:""`
}

type DiscordCmd struct {
	Token string `help:"Discord bot token." env:"DISCORD_BOT_TOKEN" required:""`
}

type SlackCmd struct {
	BotToken string `help:"Slack bot token (xoxb-)." env:"SLACK_BOT_TOKEN" required:""`
	AppToken string `help:"Slack app token (xapp-)." env:"SLACK_APP_TOKEN"`
}

type InitCmd struct{}

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

func run(kongCtx *kong.Context, cli *CLI) error {
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
	if model == "" {
		model = resolveModel(cfg)
	}

	cwd, _ := os.Getwd()
	registry := tool.DefaultRegistry(cwd)

	agentCfg := agent.DefaultAgentConfig()
	agentCfg.Model = model
	agentCfg.EnableStreaming = true

	// Connect MCP servers if configured.
	var mcpClients []*mcp.Client
	if len(cfg.MCP) > 0 {
		mcpConfigs := make([]mcp.ServerConfig, len(cfg.MCP))
		for i, c := range cfg.MCP {
			mcpConfigs[i] = mcp.ServerConfig{
				Name:    c.Name,
				Command: c.Command,
				Args:    c.Args,
				Env:     c.Env,
			}
		}

		mcpCtx, mcpCancel := context.WithTimeout(context.Background(), 30_000_000_000) // 30s
		mcpDefs, mcpExecs, clients, mcpErr := mcp.RegisterMCPServers(mcpCtx, mcpConfigs)
		mcpCancel()
		if mcpErr != nil {
			logger.Warn("mcp server connection failed", "error", mcpErr)
		} else {
			mcpClients = clients
			for _, def := range mcpDefs {
				registry.Register(def, mcpExecs[def.Name])
			}
			logger.Info("mcp servers connected", "tools", len(mcpDefs))
		}
	}
	defer func() {
		for _, c := range mcpClients {
			c.Close()
		}
	}()

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
		fmt.Fprintf(os.Stderr, "\n\033[33m⚠ Tool %q requires approval (%s)\033[0m\n", call.Name, reason)
		fmt.Fprintf(os.Stderr, "  Arguments: %s\n", string(call.Arguments))
		fmt.Fprintf(os.Stderr, "Allow? [y/N] ")
		var response string
		fmt.Scanln(&response)
		return strings.EqualFold(response, "y") || strings.EqualFold(response, "yes")
	}

	safetyChecker := security.NewSafetyChecker(safetyCfg, registry.Defs(), approvalFn)

	cost := llm.NewCostTracker()
	if cfg.Cost.DailyBudget > 0 {
		cost.SetDailyLimit(cfg.Cost.DailyBudget)
	}

	// Discover project-level SOUL.md / AGENTS.md.
	soulPrompt, agentPrompt := agent.DiscoverProjectPrompts(cwd)
	if soulPrompt != "" {
		logger.Info("discovered SOUL.md")
	} else {
		soulPrompt = defaultSoul
	}

	switch kongCtx.Command() {
	case "init":
		return setup.RunWizard()
	case "serve":
		return runServe(cli, cfg, model, logger, registry, safetyChecker, cost, agentCfg, soulPrompt, agentPrompt)
	case "telegram":
		return runTelegram(cli, cfg, model, logger, registry, safetyChecker, cost, agentCfg, soulPrompt)
	case "discord":
		return runDiscord(cli, cfg, model, logger, registry, safetyChecker, cost, agentCfg, soulPrompt)
	case "slack":
		return runSlack(cli, cfg, model, logger, registry, safetyChecker, cost, agentCfg, soulPrompt)
	default:
		return runChat(cli, cfg, model, logger, registry, safetyChecker, cost, agentCfg, soulPrompt, agentPrompt)
	}
}

func runChat(cli *CLI, cfg *config.Config, model string, logger *slog.Logger,
	registry *tool.Registry, safety *security.SafetyChecker, cost *llm.CostTracker, agentCfg agent.AgentConfig,
	soul, agentPrompt string) error {

	provider, err := createProvider(cfg, model)
	if err != nil {
		return err
	}

	a := agent.New(
		provider, agentCfg,
		registry.Executors(), registry.Defs(),
		agent.WithLogger(logger),
		agent.WithCostTracker(cost),
		agent.WithSafety(safety),
		agent.WithStreamHandler(streamHandler()),
	)

	if len(cli.Chat.Prompt) == 0 {
		sess := terminal.NewSession(a, cost, model, soul)
		return sess.Run(context.Background())
	}

	prompt := strings.Join(cli.Chat.Prompt, " ")
	runCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	parts := t.ContextParts{
		SoulPrompt:  soul,
		AgentPrompt: agentPrompt,
		History:     []t.Message{{Role: t.RoleUser, Content: prompt}},
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

func runServe(cli *CLI, cfg *config.Config, model string, logger *slog.Logger,
	registry *tool.Registry, safety *security.SafetyChecker, cost *llm.CostTracker, agentCfg agent.AgentConfig,
	soul, agentPrompt string) error {

	provider, err := createProvider(cfg, model)
	if err != nil {
		return err
	}

	db, err := openStore()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Start scheduler if tasks configured.
	if len(cfg.Scheduler.Tasks) > 0 {
		go runScheduler(ctx, cfg, provider, agentCfg, registry, safety, cost, logger)
	}

	// Mount WhatsApp webhook if configured.
	var waBot *whatsapp.Bot
	if cfg.WhatsApp != nil && cfg.WhatsApp.PhoneNumberID != "" {
		waBot = whatsapp.NewBot(whatsapp.BotConfig{
			PhoneNumberID: cfg.WhatsApp.PhoneNumberID,
			AccessToken:   cfg.WhatsApp.AccessToken,
			VerifyToken:   cfg.WhatsApp.VerifyToken,
			Provider:      provider,
			Model:         model,
			Soul:          soul,
			AgentCfg:      agentCfg,
			Registry:      registry,
			Safety:        safety,
			Cost:          cost,
			Store:         db,
			Logger:        logger,
		})
	}

	srv := server.New(server.Config{
		Addr:     cli.Serve.Addr,
		Provider: provider,
		Model:    model,
		Soul:     soul,
		AgentCfg: agentCfg,
		Registry: registry,
		Safety:   safety,
		Cost:     cost,
		Store:    db,
		Logger:   logger,
	})

	if waBot != nil {
		srv.Mount("/webhook/whatsapp", waBot.WebhookHandler())
		logger.Info("whatsapp webhook mounted at /webhook/whatsapp")
	}

	fmt.Fprintf(os.Stderr, "\033[1m\033[36mLiteClaw server running at http://localhost%s\033[0m\n", cli.Serve.Addr)
	return srv.ListenAndServe(ctx)
}

func runTelegram(cli *CLI, cfg *config.Config, model string, logger *slog.Logger,
	registry *tool.Registry, safety *security.SafetyChecker, cost *llm.CostTracker, agentCfg agent.AgentConfig,
	soul string) error {

	provider, err := createProvider(cfg, model)
	if err != nil {
		return err
	}

	db, err := openStore()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	bot := tgbot.NewBot(tgbot.BotConfig{
		Token:    cli.Telegram.Token,
		Provider: provider,
		Model:    model,
		Soul:     soul,
		AgentCfg: agentCfg,
		Registry: registry,
		Safety:   safety,
		Cost:     cost,
		Store:    db,
		Logger:   logger,
	})

	return bot.Run(ctx)
}

func runDiscord(cli *CLI, cfg *config.Config, model string, logger *slog.Logger,
	registry *tool.Registry, safety *security.SafetyChecker, cost *llm.CostTracker, agentCfg agent.AgentConfig,
	soul string) error {

	provider, err := createProvider(cfg, model)
	if err != nil {
		return err
	}

	db, err := openStore()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	bot := discord.NewBot(discord.BotConfig{
		Token:    cli.Discord.Token,
		Provider: provider,
		Model:    model,
		Soul:     soul,
		AgentCfg: agentCfg,
		Registry: registry,
		Safety:   safety,
		Cost:     cost,
		Store:    db,
		Logger:   logger,
	})

	return bot.Run(ctx)
}

func runSlack(cli *CLI, cfg *config.Config, model string, logger *slog.Logger,
	registry *tool.Registry, safety *security.SafetyChecker, cost *llm.CostTracker, agentCfg agent.AgentConfig,
	soul string) error {

	provider, err := createProvider(cfg, model)
	if err != nil {
		return err
	}

	db, err := openStore()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	bot := slack.NewBot(slack.BotConfig{
		BotToken: cli.Slack.BotToken,
		AppToken: cli.Slack.AppToken,
		Provider: provider,
		Model:    model,
		Soul:     soul,
		AgentCfg: agentCfg,
		Registry: registry,
		Safety:   safety,
		Cost:     cost,
		Store:    db,
		Logger:   logger,
	})

	return bot.Run(ctx)
}

func runScheduler(ctx context.Context, cfg *config.Config, provider t.Provider,
	agentCfg agent.AgentConfig, registry *tool.Registry, safety *security.SafetyChecker,
	cost *llm.CostTracker, logger *slog.Logger) {

	agentCfg.EnableStreaming = false
	a := agent.New(
		provider, agentCfg,
		registry.Executors(), registry.Defs(),
		agent.WithLogger(logger),
		agent.WithCostTracker(cost),
		agent.WithSafety(safety),
	)

	tasks := make([]scheduler.Task, len(cfg.Scheduler.Tasks))
	for i, tc := range cfg.Scheduler.Tasks {
		tasks[i] = scheduler.Task{
			ID:       tc.ID,
			Name:     tc.Name,
			Schedule: tc.Schedule,
			Prompt:   tc.Prompt,
			Channel:  tc.Channel,
			Enabled:  tc.Enabled,
		}
	}

	sender := func(ctx context.Context, channel, message string) error {
		logger.Info("scheduled task output", "channel", channel, "len", len(message))
		return nil
	}

	s := scheduler.New(tasks, a, defaultSoul, sender, logger)
	if err := s.Run(ctx); err != nil {
		logger.Error("scheduler error", "error", err)
	}
}

func openStore() (*store.SQLiteStore, error) {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".liteclaw", "liteclaw.db")
	return store.NewSQLite(dbPath)
}

func resolveModel(cfg *config.Config) string {
	switch cfg.Providers.Default {
	case "anthropic", "":
		if cfg.Providers.Anthropic != nil && cfg.Providers.Anthropic.DefaultModel != "" {
			return cfg.Providers.Anthropic.DefaultModel
		}
		return "claude-sonnet-4-20250514"
	case "openai":
		if cfg.Providers.OpenAI != nil && cfg.Providers.OpenAI.DefaultModel != "" {
			return cfg.Providers.OpenAI.DefaultModel
		}
		return "gpt-4o"
	case "ollama":
		if cfg.Providers.Ollama != nil && cfg.Providers.Ollama.DefaultModel != "" {
			return cfg.Providers.Ollama.DefaultModel
		}
		return "llama3.1"
	}
	return "claude-sonnet-4-20250514"
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

	case "openai":
		envKey := "OPENAI_API_KEY"
		if cfg.Providers.OpenAI != nil && cfg.Providers.OpenAI.APIKeyEnv != "" {
			envKey = cfg.Providers.OpenAI.APIKeyEnv
		}
		apiKey := os.Getenv(envKey)
		if apiKey == "" {
			return nil, fmt.Errorf("set %s environment variable", envKey)
		}
		baseURL := ""
		if cfg.Providers.OpenAI != nil {
			baseURL = cfg.Providers.OpenAI.BaseURL
		}
		return llm.NewOpenAIProvider(apiKey, baseURL, model), nil

	case "ollama":
		baseURL := ""
		if cfg.Providers.Ollama != nil {
			baseURL = cfg.Providers.Ollama.BaseURL
		}
		return llm.NewOllamaProvider(baseURL, model), nil

	default:
		return nil, fmt.Errorf("unsupported provider: %q", cfg.Providers.Default)
	}
}

// Ensure imports are used.
var (
	_ = memory.NewManager
	_ = (*whatsapp.Bot)(nil)
	_ = (*hooks.Manager)(nil)
)
