package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/marsstein/marsclaw/internal/agent"
	"github.com/marsstein/marsclaw/internal/security"
	"github.com/marsstein/marsclaw/internal/store"
	"github.com/marsstein/marsclaw/internal/tool"
	t "github.com/marsstein/marsclaw/internal/types"
)

// BotConfig configures the Telegram bot.
type BotConfig struct {
	Token    string
	Provider t.Provider
	Model    string
	Soul     string
	AgentCfg agent.AgentConfig
	Registry *tool.Registry
	Safety   *security.SafetyChecker
	Cost     t.CostRecorder
	Store    store.Store
	Logger   *slog.Logger
}

// Bot runs a Telegram bot that forwards messages to an Agent.
type Bot struct {
	client   *Client
	cfg      BotConfig
	store    store.Store
	logger   *slog.Logger

	mu       sync.Mutex
	sessions map[int64]string // chat_id -> session_id
}

// NewBot creates a Telegram bot.
func NewBot(cfg BotConfig) *Bot {
	return &Bot{
		client:   NewClient(cfg.Token),
		cfg:      cfg,
		store:    cfg.Store,
		logger:   cfg.Logger,
		sessions: make(map[int64]string),
	}
}

// Run starts the long-polling loop.
func (b *Bot) Run(ctx context.Context) error {
	b.logger.Info("telegram bot starting")

	var offset int64
	for {
		select {
		case <-ctx.Done():
			b.logger.Info("telegram bot stopping")
			return nil
		default:
		}

		updates, err := b.client.GetUpdates(ctx, offset, 30)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			b.logger.Error("get updates failed", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message == nil || u.Message.Text == "" {
				continue
			}
			b.handleMessage(ctx, u.Message)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *TGMessage) {
	chatID := msg.Chat.ID
	text := msg.Text

	// Handle commands.
	switch text {
	case "/start":
		b.client.SendMessage(ctx, chatID, "Welcome to MarsClaw! Send me any message and I'll help you.")
		return
	case "/clear":
		b.mu.Lock()
		delete(b.sessions, chatID)
		b.mu.Unlock()
		b.client.SendMessage(ctx, chatID, "Conversation cleared.")
		return
	case "/help":
		b.client.SendMessage(ctx, chatID, "/start - Welcome\n/clear - New conversation\n/help - This message\n\nJust type anything to chat!")
		return
	}

	// Show typing indicator.
	b.client.SendChatAction(ctx, chatID, "typing")

	sessionID := b.getOrCreateSession(ctx, chatID)

	// Load history.
	history, _ := b.store.GetMessages(ctx, sessionID)

	userMsg := t.Message{
		Role:      t.RoleUser,
		Content:   text,
		Timestamp: time.Now(),
	}
	history = append(history, userMsg)

	// Disable streaming for Telegram (no real-time display).
	agentCfg := b.cfg.AgentCfg
	agentCfg.EnableStreaming = false

	a := agent.New(
		b.cfg.Provider,
		agentCfg,
		b.cfg.Registry.Executors(),
		b.cfg.Registry.Defs(),
		agent.WithLogger(b.logger),
		agent.WithCostTracker(b.cfg.Cost),
		agent.WithSafety(b.cfg.Safety),
	)

	parts := t.ContextParts{
		SoulPrompt: b.cfg.Soul,
		History:    history,
	}

	// Keep typing indicator alive.
	typingCtx, stopTyping := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				b.client.SendChatAction(typingCtx, chatID, "typing")
			}
		}
	}()

	result := a.Run(ctx, parts)
	stopTyping()

	// Send response.
	response := result.Response
	if response == "" {
		response = "I couldn't generate a response."
	}

	if b.cfg.Cost != nil {
		costLine := b.cfg.Cost.FormatCostLine(b.cfg.Model, result.TotalInput, result.TotalOutput)
		response += "\n\n`" + costLine + "`"
	}

	if err := b.client.SendMessage(ctx, chatID, response); err != nil {
		b.logger.Error("send message failed", "chat_id", chatID, "error", err)
	}

	// Persist.
	newMsgs := []t.Message{userMsg}
	if len(result.History) > len(history) {
		newMsgs = result.History[len(history)-1:]
	}
	b.store.AppendMessages(ctx, sessionID, newMsgs)
}

func (b *Bot) getOrCreateSession(ctx context.Context, chatID int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if id, ok := b.sessions[chatID]; ok {
		return id
	}

	id := fmt.Sprintf("tg_%d_%d", chatID, time.Now().UnixNano())
	sess := &store.Session{
		ID:        id,
		Title:     fmt.Sprintf("Telegram chat %d", chatID),
		Source:    "telegram",
		Metadata:  map[string]string{"chat_id": fmt.Sprintf("%d", chatID)},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	b.store.CreateSession(ctx, sess)
	b.sessions[chatID] = id
	return id
}
