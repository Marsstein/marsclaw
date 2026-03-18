package channels

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Channel represents a configured messaging channel.
type Channel struct {
	ID       string `json:"id"`
	Provider string `json:"provider"` // telegram, discord, slack, whatsapp, instagram
	Name     string `json:"name"`
	Token    string `json:"token,omitempty"`
	BotToken string `json:"bot_token,omitempty"` // slack
	AppToken string `json:"app_token,omitempty"` // slack
	// WhatsApp / Instagram fields (shared Meta platform)
	PhoneNumberID string `json:"phone_number_id,omitempty"`
	AccessToken   string `json:"access_token,omitempty"`
	VerifyToken   string `json:"verify_token,omitempty"`
	PageID        string `json:"page_id,omitempty"`        // instagram
	Enabled       bool   `json:"enabled"`
}

// SupportedProviders lists all available channel providers.
var SupportedProviders = []ProviderInfo{
	{ID: "telegram", Name: "Telegram", Method: "Bot API", TokenLabel: "Bot token (from @BotFather)", TokenEnv: "TELEGRAM_BOT_TOKEN"},
	{ID: "discord", Name: "Discord", Method: "Bot API", TokenLabel: "Bot token", TokenEnv: "DISCORD_BOT_TOKEN"},
	{ID: "slack", Name: "Slack", Method: "Socket Mode", TokenLabel: "Bot token (xoxb-)", TokenEnv: "SLACK_BOT_TOKEN"},
	{ID: "whatsapp", Name: "WhatsApp", Method: "Cloud API", TokenLabel: "Access token", TokenEnv: ""},
	{ID: "instagram", Name: "Instagram", Method: "Messenger API", TokenLabel: "Page access token", TokenEnv: "INSTAGRAM_ACCESS_TOKEN"},
}

// ProviderInfo describes a supported channel provider.
type ProviderInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Method     string `json:"method"`
	TokenLabel string `json:"token_label"`
	TokenEnv   string `json:"token_env"`
}

// Store manages channel configurations on disk.
type Store struct {
	path string
}

// NewStore creates a channel store at the default location.
func NewStore() *Store {
	home, _ := os.UserHomeDir()
	return &Store{path: filepath.Join(home, ".marsclaw", "channels.json")}
}

// List returns all configured channels.
func (s *Store) List() ([]Channel, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var channels []Channel
	if err := json.Unmarshal(data, &channels); err != nil {
		return nil, err
	}
	return channels, nil
}

// Add saves a new channel configuration.
func (s *Store) Add(ch Channel) error {
	channels, err := s.List()
	if err != nil {
		return err
	}

	// Replace if same ID exists.
	found := false
	for i, c := range channels {
		if c.ID == ch.ID {
			channels[i] = ch
			found = true
			break
		}
	}
	if !found {
		channels = append(channels, ch)
	}

	return s.save(channels)
}

// Remove deletes a channel by ID.
func (s *Store) Remove(id string) error {
	channels, err := s.List()
	if err != nil {
		return err
	}

	filtered := make([]Channel, 0, len(channels))
	for _, c := range channels {
		if c.ID != id {
			filtered = append(filtered, c)
		}
	}

	return s.save(filtered)
}

// Get returns a channel by ID.
func (s *Store) Get(id string) (*Channel, error) {
	channels, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, c := range channels {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, fmt.Errorf("channel %q not found", id)
}

func (s *Store) save(channels []Channel) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(channels, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
