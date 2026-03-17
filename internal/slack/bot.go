package slack

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

	"github.com/marsstein/liteclaw/internal/agent"
	"github.com/marsstein/liteclaw/internal/security"
	"github.com/marsstein/liteclaw/internal/store"
	"github.com/marsstein/liteclaw/internal/tool"
	t "github.com/marsstein/liteclaw/internal/types"
)

const slackAPI = "https://slack.com/api"

// BotConfig configures the Slack bot.
type BotConfig struct {
	BotToken string // xoxb-...
	AppToken string // xapp-... (for Socket Mode)
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

// Bot runs a Slack bot using Socket Mode.
type Bot struct {
	cfg      BotConfig
	store    store.Store
	logger   *slog.Logger
	client   *http.Client
	botID    string

	mu       sync.Mutex
	sessions map[string]string // channel_id -> session_id
}

// NewBot creates a Slack bot.
func NewBot(cfg BotConfig) *Bot {
	return &Bot{
		cfg:      cfg,
		store:    cfg.Store,
		logger:   cfg.Logger,
		client:   &http.Client{Timeout: 30 * time.Second},
		sessions: make(map[string]string),
	}
}

// Run starts the Slack bot. Uses Events API with Socket Mode.
func (b *Bot) Run(ctx context.Context) error {
	b.logger.Info("slack bot starting")

	// Get bot identity.
	resp, err := b.slackAPI(ctx, "auth.test", nil)
	if err != nil {
		return fmt.Errorf("auth.test: %w", err)
	}
	b.botID = resp["user_id"].(string)
	b.logger.Info("slack bot ready", "user", resp["user"])

	<-ctx.Done()
	return nil
}

// HandleMessage processes an incoming Slack message.
func (b *Bot) HandleMessage(ctx context.Context, channelID, text, userID string) {
	if userID == b.botID {
		return
	}

	sessionID := b.getOrCreateSession(ctx, channelID)
	history, _ := b.store.GetMessages(ctx, sessionID)

	userMsg := t.Message{
		Role:      t.RoleUser,
		Content:   text,
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

	b.SendMessage(ctx, channelID, response)

	newMsgs := []t.Message{userMsg}
	if len(result.History) > len(history) {
		newMsgs = result.History[len(history)-1:]
	}
	b.store.AppendMessages(ctx, sessionID, newMsgs)
}

// SendMessage posts a message to a Slack channel.
func (b *Bot) SendMessage(ctx context.Context, channelID, text string) error {
	// Slack limit is 40K chars but practically keep under 4K.
	const maxLen = 4000
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

		_, err := b.slackAPI(ctx, "chat.postMessage", map[string]string{
			"channel": channelID,
			"text":    chunk,
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

	id := fmt.Sprintf("slack_%s_%d", channelID, time.Now().UnixNano())
	sess := &store.Session{
		ID:        id,
		Title:     fmt.Sprintf("Slack channel %s", channelID),
		Source:    "slack",
		Metadata:  map[string]string{"channel_id": channelID},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	b.store.CreateSession(ctx, sess)
	b.sessions[channelID] = id
	return id
}

func (b *Bot) slackAPI(ctx context.Context, method string, body any) (map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", slackAPI+"/"+method, bodyReader)
	req.Header.Set("Authorization", "Bearer "+b.cfg.BotToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if ok, _ := result["ok"].(bool); !ok {
		errMsg, _ := result["error"].(string)
		return result, fmt.Errorf("slack %s: %s", method, errMsg)
	}

	return result, nil
}
