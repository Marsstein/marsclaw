package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client is a minimal Telegram Bot API client.
type Client struct {
	token  string
	http   *http.Client
	apiURL string
}

// NewClient creates a Telegram API client.
func NewClient(token string) *Client {
	return &Client{
		token:  token,
		http:   &http.Client{},
		apiURL: "https://api.telegram.org/bot" + token,
	}
}

// Update represents a Telegram update.
type Update struct {
	UpdateID int64      `json:"update_id"`
	Message  *TGMessage `json:"message"`
}

// TGMessage represents a Telegram message.
type TGMessage struct {
	MessageID int64  `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
	From      *User  `json:"from"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// User represents a Telegram user.
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type apiResponse struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
}

// GetUpdates fetches updates using long polling.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	body, _ := json.Marshal(map[string]any{
		"offset":  offset,
		"timeout": timeout,
		"allowed_updates": []string{"message"},
	})

	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL+"/getUpdates", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if !apiResp.OK {
		return nil, fmt.Errorf("telegram API error")
	}

	var updates []Update
	if err := json.Unmarshal(apiResp.Result, &updates); err != nil {
		return nil, fmt.Errorf("unmarshal updates: %w", err)
	}
	return updates, nil
}

// SendMessage sends a text message to a chat.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	return c.sendChunked(ctx, chatID, text)
}

// SendChatAction shows a "typing" indicator.
func (c *Client) SendChatAction(ctx context.Context, chatID int64, action string) error {
	body, _ := json.Marshal(map[string]any{
		"chat_id": chatID,
		"action":  action,
	})
	return c.post(ctx, "/sendChatAction", body)
}

func (c *Client) sendChunked(ctx context.Context, chatID int64, text string) error {
	const maxLen = 4096
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxLen {
			// Try to split at last newline before limit.
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

		body, _ := json.Marshal(map[string]any{
			"chat_id":    chatID,
			"text":       chunk,
			"parse_mode": "Markdown",
		})
		if err := c.post(ctx, "/sendMessage", body); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) post(ctx context.Context, method string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram %s error %d: %s", method, resp.StatusCode, string(errBody))
	}
	return nil
}
