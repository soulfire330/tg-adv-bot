package tg

import (
	"testing"

	"github.com/gotd/td/tg"

	"tg-adv-bot/internal/service"
)

func TestNormalizeUsername(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"@some_group", "some_group"},
		{"some_group", "some_group"},
		{"t.me/some_group", "some_group"},
		{"http://t.me/some_group", "some_group"},
		{"t.me/some_group?single", "some_group"},
		{"  @some_name  ", "some_name"},
		{"tg://resolve?domain=name", "name"},
	}
	for _, tt := range tests {
		if got := normalizeUsername(tt.input); got != tt.want {
			t.Errorf("normalizeUsername(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestToPost(t *testing.T) {
	message := &tg.Message{ID: 7, Message: "сдам квартиру"}
	if got := toPost(message); got != (service.Post{ID: 7, Text: "сдам квартиру"}) {
		t.Errorf("toPost() = %+v", got)
	}

	message.SetGroupedID(99)
	if got := toPost(message); got.Grouped != 99 {
		t.Errorf("Grouped = %d, want 99", got.Grouped)
	}
}

func TestHistoryMessages(t *testing.T) {
	want := []tg.MessageClass{&tg.Message{ID: 1}}

	for name, res := range map[string]tg.MessagesMessagesClass{
		"messages":        &tg.MessagesMessages{Messages: want},
		"messagesSlice":   &tg.MessagesMessagesSlice{Messages: want},
		"channelMessages": &tg.MessagesChannelMessages{Messages: want},
	} {
		t.Run(name, func(t *testing.T) {
			if got := historyMessages(res); len(got) != 1 || got[0].GetID() != 1 {
				t.Errorf("historyMessages() = %v", got)
			}
		})
	}
}
