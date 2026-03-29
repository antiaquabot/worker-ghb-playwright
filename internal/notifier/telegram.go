package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
