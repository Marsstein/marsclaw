package whatsapp

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

const graphAPI = "https://graph.facebook.com/v21.0"

// BotConfig configures the WhatsApp bot.
type BotConfig struct {
	PhoneNumberID string // WhatsApp Business phone number ID
	AccessToken   string // permanent access token
	VerifyToken   string // webhook verification token
	Provider      t.Provider
	Model         string
	Soul          string
	AgentCfg      agent.AgentConfig
	Registry      *tool.Registry
	Safety        *security.SafetyChecker
	Cost          t.CostRecorder
	Store         store.Store
	Logger        *slog.Logger
}

// Bot handles WhatsApp Cloud API webhook events.
type Bot struct {
	cfg    BotConfig
	store  store.Store
	logger *slog.Logger
	client *http.Client

	mu       sync.Mutex
	sessions map[string]string // phone_number -> session_id
}

// NewBot creates a WhatsApp bot.
func NewBot(cfg BotConfig) *Bot {
	return &Bot{
		cfg:      cfg,
		store:    cfg.Store,
		logger:   cfg.Logger,
		client:   &http.Client{Timeout: 30 * time.Second},
		sessions: make(map[string]string),
	}
}

// WebhookHandler returns an http.Handler for the WhatsApp webhook.
func (b *Bot) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			b.handleVerify(w, r)
		case http.MethodPost:
			b.handleWebhook(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func (b *Bot) handleVerify(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	if mode == "subscribe" && token == b.cfg.VerifyToken {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(challenge))
		return
	}
	http.Error(w, "forbidden", http.StatusForbidden)
}

func (b *Bot) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Always respond 200 quickly to avoid retries.
	w.WriteHeader(http.StatusOK)

	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		b.logger.Error("parse webhook", "error", err)
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue
			}
			for _, msg := range change.Value.Messages {
				if msg.Type != "text" {
					continue
				}
				go b.handleMessage(r.Context(), msg.From, msg.Text.Body)
			}
		}
	}
}

// HandleMessage processes an incoming WhatsApp message.
func (b *Bot) handleMessage(ctx context.Context, from, text string) {
	b.logger.Info("whatsapp message", "from", from)

	sessionID := b.getOrCreateSession(ctx, from)
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

	// WhatsApp message limit is 4096 chars.
	b.sendChunked(ctx, from, response, 4096)

	newMsgs := []t.Message{userMsg}
	if len(result.History) > len(history) {
		newMsgs = result.History[len(history)-1:]
	}
	b.store.AppendMessages(ctx, sessionID, newMsgs)
}

// SendMessage sends a text message to a WhatsApp number.
func (b *Bot) SendMessage(ctx context.Context, to, text string) error {
	return b.sendChunked(ctx, to, text, 4096)
}

func (b *Bot) sendChunked(ctx context.Context, to, text string, maxLen int) error {
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

		if err := b.sendText(ctx, to, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) sendText(ctx context.Context, to, text string) error {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]string{"body": text},
	}

	data, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/%s/messages", graphAPI, b.cfg.PhoneNumberID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.cfg.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("whatsapp API %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (b *Bot) getOrCreateSession(ctx context.Context, phone string) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if id, ok := b.sessions[phone]; ok {
		return id
	}

	id := fmt.Sprintf("wa_%s_%d", phone, time.Now().UnixNano())
	sess := &store.Session{
		ID:        id,
		Title:     fmt.Sprintf("WhatsApp %s", phone),
		Source:    "whatsapp",
		Metadata:  map[string]string{"phone": phone},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	b.store.CreateSession(ctx, sess)
	b.sessions[phone] = id
	return id
}

// Webhook payload types.

type webhookPayload struct {
	Entry []webhookEntry `json:"entry"`
}

type webhookEntry struct {
	Changes []webhookChange `json:"changes"`
}

type webhookChange struct {
	Field string       `json:"field"`
	Value webhookValue `json:"value"`
}

type webhookValue struct {
	Messages []webhookMessage `json:"messages"`
}

type webhookMessage struct {
	From string      `json:"from"`
	Type string      `json:"type"`
	Text webhookText `json:"text"`
}

type webhookText struct {
	Body string `json:"body"`
}
