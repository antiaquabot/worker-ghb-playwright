package notifier

import (
	"strconv"
	"testing"
)

func TestWaitForCode_FiltersByChatID(t *testing.T) {
	n := &Notifier{chatID: "-987654321"}
	configuredChatID, _ := strconv.ParseInt(n.chatID, 10, 64)

	updates := []Update{
		{
			ID: 1,
			Message: struct {
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
			}{
				Text:      "123456",
				MessageID: 101,
				Chat:      struct{ ID int64 `json:"id"` }{ID: -999999}, // wrong chat
			},
		},
		{
			ID: 2,
			Message: struct {
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
			}{
				Text:      "654321",
				MessageID: 102,
				Chat:      struct{ ID int64 `json:"id"` }{ID: configuredChatID}, // correct chat
			},
		},
	}

	found := ""
	for _, u := range updates {
		if u.Message.Chat.ID != configuredChatID {
			continue
		}
		if u.Message.Text != "" {
			found = u.Message.Text
			break
		}
	}

	if found != "654321" {
		t.Errorf("expected '654321' from correct chat, got %q", found)
	}
}
