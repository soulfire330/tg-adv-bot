package tg

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotd/td/tg"
)

const selfRecipient = "me"

// ResolveChannel находит публичную группу по @username или t.me-ссылке.
// Членство проверяем здесь же: без него история группы недоступна.
func ResolveChannel(ctx context.Context, api *tg.Client, input string) (*tg.InputPeerChannel, error) {
	username := normalizeUsername(input)
	resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if err != nil {
		return nil, fmt.Errorf("резолв @%s: %w", username, err)
	}
	for _, chat := range resolved.Chats {
		channel, ok := chat.(*tg.Channel)
		if !ok {
			continue
		}
		if channel.Left {
			return nil, fmt.Errorf("аккаунт не состоит в @%s: вступи в группу этим аккаунтом и перезапусти сервис", username)
		}
		return &tg.InputPeerChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}, nil
	}
	return nil, fmt.Errorf("@%s — не группа и не канал", username)
}

// ResolveRecipient принимает @username, t.me-ссылку или "me" (Избранное).
func ResolveRecipient(ctx context.Context, api *tg.Client, input string) (tg.InputPeerClass, error) {
	if strings.EqualFold(strings.TrimSpace(input), selfRecipient) {
		return &tg.InputPeerSelf{}, nil
	}
	username := normalizeUsername(input)
	resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
	if err != nil {
		return nil, fmt.Errorf("резолв получателя @%s: %w", username, err)
	}
	for _, raw := range resolved.Users {
		user, ok := raw.(*tg.User)
		if !ok || user.Min {
			continue
		}
		return &tg.InputPeerUser{UserID: user.ID, AccessHash: user.AccessHash}, nil
	}
	return nil, fmt.Errorf("получатель @%s не найден", username)
}

// normalizeUsername превращает @name, t.me/name и t.me/name?x в name.
func normalizeUsername(input string) string {
	username := strings.TrimSpace(input)
	for _, prefix := range []string{
		"https://t.me/",
		"http://t.me/",
		"t.me/",
		"tg://resolve?domain=",
	} {
		username = strings.TrimPrefix(username, prefix)
	}
	username = strings.TrimPrefix(username, "@")
	if cut := strings.IndexAny(username, "?/"); cut >= 0 {
		username = username[:cut]
	}
	return username
}
