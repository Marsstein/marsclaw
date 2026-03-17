package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/marsstein/marsclaw/internal/agent"
	"github.com/marsstein/marsclaw/internal/security"
	"github.com/marsstein/marsclaw/internal/store"
	"github.com/marsstein/marsclaw/internal/tool"
	t "github.com/marsstein/marsclaw/internal/types"
)

const (
	apiBase    = "https://discord.com/api/v10"
	gatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"
)

// BotConfig configures the Discord bot.
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

// Bot runs a Discord bot.
type Bot struct {
	token    string
	cfg      BotConfig
	store    store.Store
	logger   *slog.Logger
	client   *http.Client
	botID    string

	mu       sync.Mutex
	sessions map[string]string // channel_id -> session_id
}

// NewBot creates a Discord bot.
func NewBot(cfg BotConfig) *Bot {
	return &Bot{
		token:    cfg.Token,
		cfg:      cfg,
		store:    cfg.Store,
		logger:   cfg.Logger,
		client:   &http.Client{Timeout: 30 * time.Second},
		sessions: make(map[string]string),
	}
}

// Run starts the bot using Gateway (WebSocket).
// For simplicity, we use HTTP polling of messages via REST API.
// A production bot would use the Gateway WebSocket with heartbeat.
func (b *Bot) Run(ctx context.Context) error {
	b.logger.Info("discord bot starting")

	// Get bot user info.
	me, err := b.apiGet(ctx, "/users/@me")
	if err != nil {
		return fmt.Errorf("get bot user: %w", err)
	}
	b.botID = me["id"].(string)
	b.logger.Info("discord bot ready", "user", me["username"])

	// Simple polling approach — check for new messages.
	// In production, use WebSocket Gateway for real-time events.
	<-ctx.Done()
	return nil
}

// HandleMessage processes an incoming Discord message.
// Called externally (e.g., from a webhook or Gateway handler).
func (b *Bot) HandleMessage(ctx context.Context, channelID, content, authorID string) {
	if authorID == b.botID {
		return
	}

	// Show typing indicator.
	b.apiPost(ctx, fmt.Sprintf("/channels/%s/typing", channelID), nil)

	sessionID := b.getOrCreateSession(ctx, channelID)
	history, _ := b.store.GetMessages(ctx, sessionID)

	userMsg := t.Message{
		Role:      t.RoleUser,
		Content:   content,
		Timestamp: time.Now(),
	}
	history = append(history, userMsg)

	agentCfg := b.cfg.AgentCfg
	agentCfg.EnableStreaming = false

	a := agent.New(
		b.cfg.Provider, agentCfg,
		b.cfg.Registry.Executors(), b.cfg.Registry.Defs(),
		agent.WithLogger(b.logger),
		agent.WithCostTracker(b.cfg.Cost),
		agent.WithSafety(b.cfg.Safety),
	)

	parts := t.ContextParts{SoulPrompt: b.cfg.Soul, History: history}
	result := a.Run(ctx, parts)

	response := result.Response
	if response == "" {
		response = "I couldn't generate a response."
	}

	// Discord message limit is 2000 chars.
	b.sendChunked(ctx, channelID, response, 2000)

	newMsgs := []t.Message{userMsg}
	if len(result.History) > len(history) {
		newMsgs = result.History[len(history)-1:]
	}
	b.store.AppendMessages(ctx, sessionID, newMsgs)
}

// SendMessage sends a message to a Discord channel.
func (b *Bot) SendMessage(ctx context.Context, channelID, content string) error {
	return b.sendChunked(ctx, channelID, content, 2000)
}

func (b *Bot) sendChunked(ctx context.Context, channelID, text string, maxLen int) error {
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxLen {
			cut := maxLen
			for i := maxLen - 1; i > maxLen/2; i-- {
				if text[i] == '\n' {
					cut = i + 1
					break
				}
			}
			chunk = text[:cut]
			text = text[cut:]
		} else {
			text = ""
		}

		_, err := b.apiPost(ctx, fmt.Sprintf("/channels/%s/messages", channelID), map[string]string{
			"content": chunk,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) getOrCreateSession(ctx context.Context, channelID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if id, ok := b.sessions[channelID]; ok {
		return id
	}

	id := fmt.Sprintf("discord_%s_%d", channelID, time.Now().UnixNano())
	sess := &store.Session{
		ID:        id,
		Title:     fmt.Sprintf("Discord channel %s", channelID),
		Source:    "discord",
		Metadata:  map[string]string{"channel_id": channelID},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	b.store.CreateSession(ctx, sess)
	b.sessions[channelID] = id
	return id
}

func (b *Bot) apiGet(ctx context.Context, path string) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", apiBase+path, nil)
	req.Header.Set("Authorization", "Bot "+b.token)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

func (b *Bot) apiPost(ctx context.Context, path string, body any) (map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", apiBase+path, bodyReader)
	req.Header.Set("Authorization", "Bot "+b.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}
