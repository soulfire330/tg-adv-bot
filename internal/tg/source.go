package tg

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"

	"github.com/gotd/td/tg"

	"tg-adv-bot/internal/service"
)

const (
	historyPageSize = 100
	// 50 страниц ≈ 5000 сообщений: столько копится за 5+ суток простоя группы
	// с потоком 500–1000 сообщений в сутки.
	historyMaxPages = 50
)

// Source отдаёт посты из истории группы.
type Source struct {
	api      *tg.Client
	peer     tg.InputPeerClass
	log      *slog.Logger
	pageSize int
	maxPages int
}

func NewSource(api *tg.Client, peer tg.InputPeerClass, log *slog.Logger) *Source {
	return &Source{api: api, peer: peer, log: log, pageSize: historyPageSize, maxPages: historyMaxPages}
}

// Fetch возвращает посты новее afterID по возрастанию id. limit > 0 —
// потолок выдачи: нужен только при первом запуске, для бэкфилла.
func (s *Source) Fetch(ctx context.Context, afterID, limit int) ([]service.Post, error) {
	var posts []service.Post
	offset := 0
	capped := true

	for page := 0; page < s.maxPages; page++ {
		res, err := s.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
			Peer:     s.peer,
			OffsetID: offset,
			Limit:    s.pageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("история группы: %w", err)
		}
		messages := historyMessages(res)
		if len(messages) == 0 {
			capped = false
			break
		}

		reachedKnown := false
		for _, raw := range messages {
			offset = raw.GetID()
			if offset <= afterID {
				reachedKnown = true
				continue
			}
			if message, ok := raw.(*tg.Message); ok {
				posts = append(posts, toPost(message))
			}
		}

		if reachedKnown || len(messages) < s.pageSize {
			capped = false
			break
		}
		if limit > 0 && len(posts) >= limit {
			capped = false
			break
		}
	}

	if capped && afterID > 0 {
		s.log.Warn("упёрлись в потолок истории: часть пропущенных постов останется необработанной",
			"pages", s.maxPages, "fetched", len(posts), "after_id", afterID)
	}

	if limit > 0 && len(posts) > limit {
		posts = posts[:limit]
	}
	slices.Reverse(posts)
	return posts, nil
}

// Forwarder пересылает сообщения группы получателю.
type Forwarder struct {
	api  *tg.Client
	from tg.InputPeerClass
	to   tg.InputPeerClass
}

func NewForwarder(api *tg.Client, from, to tg.InputPeerClass) *Forwarder {
	return &Forwarder{api: api, from: from, to: to}
}

func (f *Forwarder) Forward(ctx context.Context, ids []int) error {
	random := make([]int64, len(ids))
	for i := range random {
		random[i] = rand.Int64()
	}
	if _, err := f.api.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
		FromPeer: f.from,
		ToPeer:   f.to,
		ID:       ids,
		RandomID: random,
	}); err != nil {
		return fmt.Errorf("форвард %v: %w", ids, err)
	}
	return nil
}

func toPost(message *tg.Message) service.Post {
	post := service.Post{ID: message.ID, Text: message.Message}
	if grouped, ok := message.GetGroupedID(); ok {
		post.Grouped = grouped
	}
	return post
}

func historyMessages(res tg.MessagesMessagesClass) []tg.MessageClass {
	switch r := res.(type) {
	case *tg.MessagesMessages:
		return r.Messages
	case *tg.MessagesMessagesSlice:
		return r.Messages
	case *tg.MessagesChannelMessages:
		return r.Messages
	}
	return nil
}
