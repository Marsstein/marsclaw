package channels

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// RunAdd runs the interactive channel setup wizard.
func RunAdd(store *Store) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("\n  \033[1mMarsClaw — Add Channel\033[0m\n\n")

	// Show available providers.
	fmt.Println("  \033[36mSelect a channel:\033[0m")
	for i, p := range SupportedProviders {
		fmt.Printf("    %d) %s (%s)\n", i+1, p.Name, p.Method)
	}
	fmt.Println()

	choice := prompt(reader, "  Choice [1]: ", "1")
	idx := 0
	if n := choice[0] - '1'; n < byte(len(SupportedProviders)) {
		idx = int(n)
	}
	provider := SupportedProviders[idx]

	fmt.Printf("\n  \033[36m%s Setup\033[0m\n", provider.Name)

	ch := Channel{
		Provider: provider.ID,
		Enabled:  true,
	}

	switch provider.ID {
	case "telegram":
		fmt.Println("  ┌─────────────────────────────────────────┐")
		fmt.Println("  │ 1) Open Telegram → chat with @BotFather │")
		fmt.Println("  │ 2) Send /newbot (or /mybots)            │")
		fmt.Println("  │ 3) Copy the token (123456:ABC...)       │")
		fmt.Println("  │                                         │")
		fmt.Println("  │ Tip: set TELEGRAM_BOT_TOKEN in env      │")
		fmt.Println("  └─────────────────────────────────────────┘")
		fmt.Println()

		token := os.Getenv("TELEGRAM_BOT_TOKEN")
		if token != "" {
			fmt.Printf("  Found TELEGRAM_BOT_TOKEN in environment.\n")
			if confirm(reader, "  Use it?") {
				ch.Token = token
			}
		}
		if ch.Token == "" {
			ch.Token = prompt(reader, "  \033[33mEnter Telegram bot token:\033[0m ", "")
			if ch.Token == "" {
				fmt.Println("  Aborted.")
				return nil
			}
		}

		name := prompt(reader, "  Channel name [default]: ", "default")
		ch.Name = name
		ch.ID = "telegram-" + name

	case "discord":
		fmt.Println("  ┌──────────────────────────────────────────────────┐")
		fmt.Println("  │ 1) Go to discord.com/developers/applications    │")
		fmt.Println("  │ 2) Create app → Bot → Copy token                │")
		fmt.Println("  │ 3) Enable MESSAGE CONTENT intent                │")
		fmt.Println("  │ 4) Invite bot to your server with messages perm │")
		fmt.Println("  │                                                  │")
		fmt.Println("  │ Tip: set DISCORD_BOT_TOKEN in env               │")
		fmt.Println("  └──────────────────────────────────────────────────┘")
		fmt.Println()

		token := os.Getenv("DISCORD_BOT_TOKEN")
		if token != "" {
			fmt.Printf("  Found DISCORD_BOT_TOKEN in environment.\n")
			if confirm(reader, "  Use it?") {
				ch.Token = token
			}
		}
		if ch.Token == "" {
			ch.Token = prompt(reader, "  \033[33mEnter Discord bot token:\033[0m ", "")
			if ch.Token == "" {
				fmt.Println("  Aborted.")
				return nil
			}
		}

		name := prompt(reader, "  Channel name [default]: ", "default")
		ch.Name = name
		ch.ID = "discord-" + name

	case "slack":
		fmt.Println("  ┌───────────────────────────────────────────────┐")
		fmt.Println("  │ 1) Go to api.slack.com/apps → Create New App │")
		fmt.Println("  │ 2) Enable Socket Mode → get App Token (xapp-)│")
		fmt.Println("  │ 3) OAuth → Install → get Bot Token (xoxb-)   │")
		fmt.Println("  │ 4) Add scopes: chat:write, app_mentions:read │")
		fmt.Println("  │                                               │")
		fmt.Println("  │ Tip: set SLACK_BOT_TOKEN in env              │")
		fmt.Println("  └───────────────────────────────────────────────┘")
		fmt.Println()

		botToken := os.Getenv("SLACK_BOT_TOKEN")
		if botToken != "" {
			fmt.Printf("  Found SLACK_BOT_TOKEN in environment.\n")
			if confirm(reader, "  Use it?") {
				ch.BotToken = botToken
			}
		}
		if ch.BotToken == "" {
			ch.BotToken = prompt(reader, "  \033[33mEnter Slack bot token (xoxb-):\033[0m ", "")
			if ch.BotToken == "" {
				fmt.Println("  Aborted.")
				return nil
			}
		}

		appToken := os.Getenv("SLACK_APP_TOKEN")
		if appToken != "" {
			ch.AppToken = appToken
		} else {
			ch.AppToken = prompt(reader, "  Enter Slack app token (xapp-, optional): ", "")
		}

		name := prompt(reader, "  Channel name [default]: ", "default")
		ch.Name = name
		ch.ID = "slack-" + name

	case "whatsapp":
		fmt.Println("  ┌────────────────────────────────────────────────────┐")
		fmt.Println("  │ 1) Go to developers.facebook.com → Create App     │")
		fmt.Println("  │ 2) Add WhatsApp product → get Phone Number ID     │")
		fmt.Println("  │ 3) Generate permanent access token                │")
		fmt.Println("  │ 4) Set webhook URL to: https://your-domain/webhook│")
		fmt.Println("  └────────────────────────────────────────────────────┘")
		fmt.Println()

		ch.PhoneNumberID = prompt(reader, "  \033[33mEnter Phone Number ID:\033[0m ", "")
		if ch.PhoneNumberID == "" {
			fmt.Println("  Aborted.")
			return nil
		}
		ch.AccessToken = prompt(reader, "  \033[33mEnter Access Token:\033[0m ", "")
		ch.VerifyToken = prompt(reader, "  Enter Verify Token (for webhook): ", "marsclaw-verify")

		name := prompt(reader, "  Channel name [default]: ", "default")
		ch.Name = name
		ch.ID = "whatsapp-" + name

	case "instagram":
		fmt.Println("  ┌─────────────────────────────────────────────────────┐")
		fmt.Println("  │ 1) Go to developers.facebook.com → Create App      │")
		fmt.Println("  │ 2) Add Instagram product (Messenger API)           │")
		fmt.Println("  │ 3) Connect Instagram Professional account          │")
		fmt.Println("  │ 4) Generate Page Access Token (long-lived)         │")
		fmt.Println("  │ 5) Subscribe to messages webhook                   │")
		fmt.Println("  │ 6) Webhook URL: https://your-domain/webhook/ig     │")
		fmt.Println("  │                                                     │")
		fmt.Println("  │ Requires: Instagram Professional/Business account  │")
		fmt.Println("  │ Tip: set INSTAGRAM_ACCESS_TOKEN in env             │")
		fmt.Println("  └─────────────────────────────────────────────────────┘")
		fmt.Println()

		token := os.Getenv("INSTAGRAM_ACCESS_TOKEN")
		if token != "" {
			fmt.Printf("  Found INSTAGRAM_ACCESS_TOKEN in environment.\n")
			if confirm(reader, "  Use it?") {
				ch.AccessToken = token
			}
		}
		if ch.AccessToken == "" {
			ch.AccessToken = prompt(reader, "  \033[33mEnter Page Access Token:\033[0m ", "")
			if ch.AccessToken == "" {
				fmt.Println("  Aborted.")
				return nil
			}
		}

		ch.PageID = prompt(reader, "  \033[33mEnter Instagram Page ID:\033[0m ", "")
		ch.VerifyToken = prompt(reader, "  Enter Verify Token (for webhook): ", "marsclaw-verify")

		name := prompt(reader, "  Channel name [default]: ", "default")
		ch.Name = name
		ch.ID = "instagram-" + name
	}

	if err := store.Add(ch); err != nil {
		return fmt.Errorf("save channel: %w", err)
	}

	fmt.Printf("\n  \033[32m✓ %s channel %q saved!\033[0m\n", provider.Name, ch.Name)
	fmt.Printf("  Config: ~/.marsclaw/channels.json\n\n")

	// Show how to run.
	switch provider.ID {
	case "telegram":
		fmt.Printf("  Run:  marsclaw telegram\n")
		fmt.Printf("  Or:   TELEGRAM_BOT_TOKEN=%s marsclaw telegram\n\n", maskToken(ch.Token))
	case "discord":
		fmt.Printf("  Run:  marsclaw discord\n")
		fmt.Printf("  Or:   DISCORD_BOT_TOKEN=%s marsclaw discord\n\n", maskToken(ch.Token))
	case "slack":
		fmt.Printf("  Run:  marsclaw slack\n\n")
	case "whatsapp":
		fmt.Printf("  Mount webhook:  marsclaw serve\n")
		fmt.Printf("  Webhook URL:    https://your-domain/webhook/whatsapp\n\n")
	case "instagram":
		fmt.Printf("  Mount webhook:  marsclaw serve\n")
		fmt.Printf("  Webhook URL:    https://your-domain/webhook/ig\n\n")
	}

	return nil
}

// RunList lists all configured channels.
func RunList(store *Store) error {
	channels, err := store.List()
	if err != nil {
		return err
	}

	if len(channels) == 0 {
		fmt.Println("\n  No channels configured.")
		fmt.Println("  Run: marsclaw channels add\n")
		return nil
	}

	fmt.Print("\n  \033[1mConfigured Channels\033[0m\n\n")
	for _, ch := range channels {
		status := "\033[32m●\033[0m"
		if !ch.Enabled {
			status = "\033[90m○\033[0m"
		}

		token := ""
		switch ch.Provider {
		case "telegram":
			token = maskToken(ch.Token)
		case "discord":
			token = maskToken(ch.Token)
		case "slack":
			token = maskToken(ch.BotToken)
		case "whatsapp":
			token = ch.PhoneNumberID
		case "instagram":
			token = maskToken(ch.AccessToken)
		}

		fmt.Printf("  %s  %-12s %-15s %s\n", status, ch.Provider, ch.Name, token)
	}
	fmt.Println()

	return nil
}

// RunRemove removes a channel by ID.
func RunRemove(store *Store, id string) error {
	if id == "" {
		channels, err := store.List()
		if err != nil {
			return err
		}
		if len(channels) == 0 {
			fmt.Println("No channels to remove.")
			return nil
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Println("\n  Select channel to remove:")
		for i, ch := range channels {
			fmt.Printf("    %d) %s — %s\n", i+1, ch.Provider, ch.Name)
		}
		choice := prompt(reader, "\n  Choice: ", "")
		idx := 0
		if len(choice) > 0 {
			idx = int(choice[0]-'1')
		}
		if idx < 0 || idx >= len(channels) {
			fmt.Println("  Invalid choice.")
			return nil
		}
		id = channels[idx].ID
	}

	if err := store.Remove(id); err != nil {
		return err
	}
	fmt.Printf("  \033[32m✓ Channel %q removed.\033[0m\n\n", id)
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

func confirm(reader *bufio.Reader, question string) bool {
	fmt.Printf("%s [Y/n] ", question)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}

func maskToken(token string) string {
	if len(token) < 8 {
		return "****"
	}
	return token[:4] + "..." + token[len(token)-4:]
}
