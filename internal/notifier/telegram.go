package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Notifier sends messages via Telegram Bot API.
type Notifier struct {
	botToken string
	chatID   string
	client   *http.Client
}

func New(botToken, chatID string) *Notifier {
	return &Notifier{
		botToken: botToken,
		chatID:   chatID,
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

// Send sends a text message to the configured chat.
func (n *Notifier) Send(ctx context.Context, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.botToken)

	payload := map[string]any{
		"chat_id":    n.chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// SendWithMessageID sends a text message and returns the message ID.
func (n *Notifier) SendWithMessageID(ctx context.Context, text string) (int, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.botToken)

	payload := map[string]any{
		"chat_id":    n.chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("telegram error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode: %w", err)
	}

	return result.Result.MessageID, nil
}

// FormatRegistrationOpened formats the REGISTRATION_OPENED notification message.
func (n *Notifier) FormatRegistrationOpened(externalID string, data map[string]any) string {
	title, _ := data["title"].(string)
	regURL, _ := data["registration_url"].(string)

	msg := "🚀 <b>Открылась регистрация!</b>\n\n"
	if title != "" {
		msg += fmt.Sprintf("📦 %s\n", title)
	} else {
		msg += fmt.Sprintf("📦 Объект: %s\n", externalID)
	}
	if regURL != "" {
		msg += fmt.Sprintf("🔗 <a href=\"%s\">Зарегистрироваться</a>\n", regURL)
	}
	msg += fmt.Sprintf("\n🕐 %s", time.Now().Format("02.01.2006 15:04:05"))
	return msg
}

// FormatRegistrationClosed formats the REGISTRATION_CLOSED notification message.
func (n *Notifier) FormatRegistrationClosed(externalID string) string {
	return fmt.Sprintf(
		"🔒 <b>Регистрация закрыта</b>\n\n📦 Объект: %s\n🕐 %s",
		externalID, time.Now().Format("02.01.2006 15:04:05"),
	)
}

// FormatServiceUnavailable formats the service unavailable notification.
func (n *Notifier) FormatServiceUnavailable(minutes int) string {
	return fmt.Sprintf("⚠️ Сервис мониторинга недоступен более %d мин.", minutes)
}

// FormatRegistrationSuccess formats the successful registration notification.
func (n *Notifier) FormatRegistrationSuccess(externalID string) string {
	return fmt.Sprintf("✅ <b>Регистрация выполнена</b>\n\n📦 Объект: %s\n🕐 %s",
		externalID, time.Now().Format("02.01.2006 15:04:05"))
}

// FormatRegistrationError formats the failed registration notification.
func (n *Notifier) FormatRegistrationError(externalID string, err error) string {
	return fmt.Sprintf("❌ <b>Ошибка регистрации</b>\n\n📦 Объект: %s\n⚠️ %v\n🕐 %s",
		externalID, err, time.Now().Format("02.01.2006 15:04:05"))
}

type Update struct {
	ID      int `json:"update_id"`
	Message struct {
		Text           string `json:"text"`
		MessageID      int    `json:"message_id"`
		ReplyToMessage struct {
			MessageID int `json:"message_id"`
		} `json:"reply_to_message,omitempty"`
		From struct {
			ID int `json:"id"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

func (n *Notifier) GetUpdates(ctx context.Context, offset int) ([]Update, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", n.botToken)

	// Telegram long-poll timeout. Must be comfortably below the HTTP client
	// Timeout (15 s) to avoid the client cancelling the request before
	// Telegram returns; 8 s gives 7 s of headroom for network latency.
	payload := map[string]any{
		"offset":  offset,
		"timeout": 8,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("telegram error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return result.Result, nil
}

func (n *Notifier) WaitForCode(ctx context.Context, messageID int) (string, error) {
	offset := 0
	configuredChatID, _ := strconv.ParseInt(n.chatID, 10, 64)
	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		updates, err := n.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			log.Printf("[telegram] getUpdates error: %v", err)
			continue
		}
		for _, u := range updates {
			offset = u.ID + 1
			if u.Message.Chat.ID != configuredChatID {
				log.Printf("[telegram] ignoring message from unexpected chat %d", u.Message.Chat.ID)
				continue
			}
			if u.Message.ReplyToMessage.MessageID == messageID {
				log.Printf("[telegram] received reply to message %d: %s", messageID, u.Message.Text)
				return strings.TrimSpace(u.Message.Text), nil
			}
			if u.Message.MessageID > messageID && u.Message.Text != "" {
				log.Printf("[telegram] received message after %d (no reply): %s", messageID, u.Message.Text)
				return strings.TrimSpace(u.Message.Text), nil
			}
		}
	}
}
