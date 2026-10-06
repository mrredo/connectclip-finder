package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"time"

	"connectclip-finder/internal/model"
)

// TelegramNotifier sends formatted alerts via the Telegram Bot API.
type TelegramNotifier struct {
	botToken   string
	chatID     string
	targetName string
	client     *http.Client
}

// NewTelegramNotifier creates a new notifier instance.
func NewTelegramNotifier(botToken, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		botToken:   botToken,
		chatID:     chatID,
		targetName: "Oticon ConnectClip",
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// SetTargetName updates the target product name for alerts.
func (t *TelegramNotifier) SetTargetName(name string) {
	if name != "" {
		t.targetName = name
	}
}

// IsConfigured returns true if botToken and chatID are provided.
func (t *TelegramNotifier) IsConfigured() bool {
	return t.botToken != "" && t.chatID != ""
}

// TestConnection validates the Telegram bot token and chat ID.
func (t *TelegramNotifier) TestConnection(ctx context.Context) error {
	if !t.IsConfigured() {
		return fmt.Errorf("telegram bot token or chat ID is not configured")
	}

	testMsg := fmt.Sprintf("🔔 <b>%s Monitor initialized</b>\nTelegram notifications are active and connected.", html.EscapeString(t.targetName))
	return t.sendMessage(ctx, testMsg)
}

// SendListingAlert sends an alert with photo if available, falling back to text.
func (t *TelegramNotifier) SendListingAlert(ctx context.Context, listing *model.Listing) error {
	if !t.IsConfigured() {
		return nil
	}

	text := FormatTelegramMessageWithTarget(listing, t.targetName)

	// If listing has an image, attempt sendPhoto
	if photoURL := listing.PrimaryImage(); photoURL != "" {
		err := t.sendPhoto(ctx, photoURL, text)
		if err == nil {
			return nil
		}
		// If sendPhoto failed (e.g. image format issue), fall back to text message
	}

	return t.sendMessage(ctx, text)
}

func (t *TelegramNotifier) sendMessage(ctx context.Context, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)
	payload := map[string]any{
		"chat_id":                  t.chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram sendMessage error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (t *TelegramNotifier) sendPhoto(ctx context.Context, photoURL, caption string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", t.botToken)
	payload := map[string]any{
		"chat_id":    t.chatID,
		"photo":      photoURL,
		"caption":    caption,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram photo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram sendPhoto error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
