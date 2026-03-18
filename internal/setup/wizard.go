package setup

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marsstein/marsclaw/internal/channels"
)

// RunWizard runs an interactive setup wizard that creates ~/.marsclaw/config.yaml.
func RunWizard() error {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".marsclaw")
	configPath := filepath.Join(dir, "config.yaml")

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config already exists at %s\n", configPath)
		if !confirm("Overwrite?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("\n  \033[1m\033[36mMarsClaw Setup\033[0m\n\n")

	// Step 1: Provider selection.
	fmt.Println("  \033[36mStep 1/4 — LLM Provider\033[0m")
	fmt.Println()
	fmt.Println("    1) Anthropic (Claude) — best for coding")
	fmt.Println("    2) Google Gemini — uses GCP credits")
	fmt.Println("    3) OpenAI (GPT-4o)")
	fmt.Println("    4) Ollama — local, free, offline")
	fmt.Println()
	provider := prompt(reader, "  Choice [1]: ", "1")

	var providerName, model, envKey string
	switch provider {
	case "2":
		providerName = "gemini"
		model = prompt(reader, "  Model [gemini-2.5-flash]: ", "gemini-2.5-flash")
		envKey = "GEMINI_API_KEY"
	case "3":
		providerName = "openai"
		model = prompt(reader, "  Model [gpt-4o]: ", "gpt-4o")
		envKey = "OPENAI_API_KEY"
	case "4":
		providerName = "ollama"
		model = prompt(reader, "  Model [llama3.1]: ", "llama3.1")
		fmt.Println()
		fmt.Println("  \033[33mOllama Quick Start:\033[0m")
		fmt.Println("    1. Install: curl -fsSL https://ollama.ai/install.sh | sh")
		fmt.Printf("    2. Pull:    ollama pull %s\n", model)
		fmt.Println("    3. Run:     ollama serve  (runs on port 11434)")
		fmt.Println()

		// Check if ollama is running.
		if _, err := exec.LookPath("ollama"); err == nil {
			fmt.Println("  \033[32m✓ Ollama found on PATH\033[0m")
		} else {
			fmt.Println("  \033[33m⚠ Ollama not found. Install it first.\033[0m")
		}
		fmt.Println()
	default:
		providerName = "anthropic"
		model = prompt(reader, "  Model [claude-sonnet-4-20250514]: ", "claude-sonnet-4-20250514")
		envKey = "ANTHROPIC_API_KEY"
	}

	// Step 2: Channel setup.
	fmt.Println()
	fmt.Println("  \033[36mStep 2/4 — Channels\033[0m")
	fmt.Println()
	fmt.Println("    Connect MarsClaw to messaging platforms.")
	fmt.Println("    You can always add more later with: marsclaw channels add")
	fmt.Println()
	fmt.Println("    1) Telegram (Bot API)")
	fmt.Println("    2) Discord (Bot API)")
	fmt.Println("    3) Slack (Socket Mode)")
	fmt.Println("    4) WhatsApp (Cloud API)")
	fmt.Println("    5) Skip for now")
	fmt.Println()
	channelChoice := prompt(reader, "  Choice [5]: ", "5")

	if channelChoice != "5" {
		store := channels.NewStore()
		ch := channels.Channel{Enabled: true}

		switch channelChoice {
		case "1":
			ch.Provider = "telegram"
			fmt.Println()
			fmt.Println("  \033[33mGet token from @BotFather on Telegram\033[0m")
			ch.Token = prompt(reader, "  Enter Telegram bot token: ", "")
			ch.Name = prompt(reader, "  Channel name [default]: ", "default")
			ch.ID = "telegram-" + ch.Name
		case "2":
			ch.Provider = "discord"
			fmt.Println()
			fmt.Println("  \033[33mGet token from discord.com/developers/applications\033[0m")
			ch.Token = prompt(reader, "  Enter Discord bot token: ", "")
			ch.Name = prompt(reader, "  Channel name [default]: ", "default")
			ch.ID = "discord-" + ch.Name
		case "3":
			ch.Provider = "slack"
			fmt.Println()
			fmt.Println("  \033[33mGet tokens from api.slack.com/apps\033[0m")
			ch.BotToken = prompt(reader, "  Enter Slack bot token (xoxb-): ", "")
			ch.AppToken = prompt(reader, "  Enter Slack app token (xapp-): ", "")
			ch.Name = prompt(reader, "  Channel name [default]: ", "default")
			ch.ID = "slack-" + ch.Name
		case "4":
			ch.Provider = "whatsapp"
			fmt.Println()
			fmt.Println("  \033[33mGet credentials from developers.facebook.com\033[0m")
			ch.PhoneNumberID = prompt(reader, "  Enter Phone Number ID: ", "")
			ch.AccessToken = prompt(reader, "  Enter Access Token: ", "")
			ch.VerifyToken = prompt(reader, "  Enter Verify Token [marsclaw-verify]: ", "marsclaw-verify")
			ch.Name = prompt(reader, "  Channel name [default]: ", "default")
			ch.ID = "whatsapp-" + ch.Name
		}

		if ch.Token != "" || ch.BotToken != "" || ch.PhoneNumberID != "" {
			if err := store.Add(ch); err != nil {
				fmt.Printf("  \033[31mFailed to save channel: %v\033[0m\n", err)
			} else {
				fmt.Printf("  \033[32m✓ %s channel saved!\033[0m\n", ch.Provider)
			}
		}
	}

	// Step 3: MCP Integrations.
	fmt.Println()
	fmt.Println("  \033[36mStep 3/4 — MCP Integrations\033[0m")
	fmt.Println()
	fmt.Println("    MCP servers let MarsClaw use external tools (Zapier, databases, etc.)")
	fmt.Println()

	var mcpConfigs []string

	zapier := prompt(reader, "  Add Zapier MCP? (connects 8,000+ apps) [y/N]: ", "n")
	if strings.EqualFold(zapier, "y") || strings.EqualFold(zapier, "yes") {
		zapierURL := prompt(reader, "  Zapier MCP server URL (from mcp.zapier.com): ", "")
		if zapierURL != "" {
			mcpConfigs = append(mcpConfigs, fmt.Sprintf(`  - name: zapier
    command: npx
    args: ["-y", "@anthropic-ai/mcp-proxy", "%s"]`, zapierURL))
		}
	}

	customMCP := prompt(reader, "  Add a custom MCP server? [y/N]: ", "n")
	if strings.EqualFold(customMCP, "y") || strings.EqualFold(customMCP, "yes") {
		mcpName := prompt(reader, "  MCP server name: ", "custom")
		mcpCmd := prompt(reader, "  Command (e.g. npx, uvx, node): ", "npx")
		mcpArgs := prompt(reader, "  Args (comma-separated): ", "")
		args := strings.Split(mcpArgs, ",")
		argsQuoted := make([]string, len(args))
		for i, a := range args {
			argsQuoted[i] = fmt.Sprintf("%q", strings.TrimSpace(a))
		}
		mcpConfigs = append(mcpConfigs, fmt.Sprintf("  - name: %s\n    command: %s\n    args: [%s]",
			mcpName, mcpCmd, strings.Join(argsQuoted, ", ")))
	}

	// Step 4: Budget & Security.
	fmt.Println()
	fmt.Println("  \033[36mStep 4/4 — Budget & Security\033[0m")
	fmt.Println()
	budget := prompt(reader, "  Daily budget in USD (0 = unlimited) [0]: ", "0")
	strict := prompt(reader, "  Require approval for dangerous tools? [y/N]: ", "n")

	// Build config.
	var b strings.Builder
	b.WriteString("# MarsClaw configuration\n")
	b.WriteString("# Generated by: marsclaw init\n\n")
	b.WriteString("providers:\n")
	b.WriteString(fmt.Sprintf("  default: %s\n", providerName))

	switch providerName {
	case "anthropic":
		b.WriteString("  anthropic:\n")
		b.WriteString(fmt.Sprintf("    api_key_env: %s\n", envKey))
		b.WriteString(fmt.Sprintf("    default_model: %s\n", model))
	case "gemini":
		b.WriteString("  gemini:\n")
		b.WriteString(fmt.Sprintf("    api_key_env: %s\n", envKey))
		b.WriteString(fmt.Sprintf("    default_model: %s\n", model))
	case "openai":
		b.WriteString("  openai:\n")
		b.WriteString(fmt.Sprintf("    api_key_env: %s\n", envKey))
		b.WriteString(fmt.Sprintf("    default_model: %s\n", model))
	case "ollama":
		b.WriteString("  ollama:\n")
		b.WriteString(fmt.Sprintf("    default_model: %s\n", model))
	}

	b.WriteString("\nagent:\n")
	b.WriteString("  max_turns: 25\n")
	b.WriteString("  enable_streaming: true\n")

	b.WriteString("\ncost:\n")
	b.WriteString("  inline_display: true\n")
	b.WriteString(fmt.Sprintf("  daily_budget: %s\n", budget))

	b.WriteString("\nsecurity:\n")
	if strings.EqualFold(strict, "y") || strings.EqualFold(strict, "yes") {
		b.WriteString("  strict_approval: true\n")
	} else {
		b.WriteString("  strict_approval: false\n")
	}
	b.WriteString("  scan_credentials: true\n")
	b.WriteString("  path_traversal_guard: true\n")

	if len(mcpConfigs) > 0 {
		b.WriteString("\nmcp:\n")
		for _, mc := range mcpConfigs {
			b.WriteString(mc + "\n")
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	if err := os.WriteFile(configPath, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// Summary.
	fmt.Println()
	fmt.Println("  \033[1m\033[32m✓ Setup complete!\033[0m")
	fmt.Printf("  Config: %s\n", configPath)
	fmt.Println()

	if envKey != "" {
		val := os.Getenv(envKey)
		if val == "" {
			fmt.Printf("  Set your API key:\n")
			fmt.Printf("    \033[33mexport %s=\"your-key-here\"\033[0m\n\n", envKey)
		} else {
			fmt.Printf("  \033[32m✓\033[0m %s is set\n\n", envKey)
		}
	}

	fmt.Println("  \033[1mNext steps:\033[0m")
	fmt.Println("    marsclaw              — start chatting")
	fmt.Println("    marsclaw serve        — start Web UI + API")
	fmt.Println("    marsclaw channels add — connect Telegram, Discord, etc.")
	fmt.Println("    marsclaw telegram     — run Telegram bot")
	fmt.Println()

	return nil
}

func prompt(reader *bufio.Reader, label, defaultVal string) string {
	fmt.Print(label)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

func confirm(question string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N] ", question)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
