// Package service — конвейер: пост → кейворды → вердикт LLM → форвард.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"tg-adv-bot/internal/filter"
	"tg-adv-bot/internal/judge"
	"tg-adv-bot/internal/state"
)

// Post — сообщение группы; одиночное или часть альбома (Grouped != 0).
type Post struct {
	ID      int
	Grouped int64
	Text    string
}

// Source отдаёт посты новее afterID по возрастанию id; limit > 0 — не больше limit штук.
type Source interface {
	Fetch(ctx context.Context, afterID, limit int) ([]Post, error)
}

type Judge interface {
	Evaluate(ctx context.Context, text string) (judge.Verdict, error)
}

type Forwarder interface {
	Forward(ctx context.Context, ids []int) error
}

type Store interface {
	Load() (state.State, error)
	Save(state.State) error
}

// Deps — всё, чем сервис пользуется снаружи.
type Deps struct {
	Source   Source
	Judge    Judge
	Forward  Forwarder
	Store    Store
	Keywords []string
	Backfill int
	Interval time.Duration
	Log      *slog.Logger
}

type Service struct {
	deps    Deps
	lastID  int
	started bool
}

func New(deps Deps) *Service {
	return &Service{deps: deps}
}

// Run опрашивает группу, пока не отменят контекст. Ошибки опроса не фатальны.
func (s *Service) Run(ctx context.Context) error {
	st, err := s.deps.Store.Load()
	if err != nil {
		return fmt.Errorf("состояние: %w", err)
	}
	s.lastID = st.LastID
	s.deps.Log.Info("старт", "last_id", s.lastID, "interval", s.deps.Interval.String())

	ticker := time.NewTicker(s.deps.Interval)
	defer ticker.Stop()

	for {
		if err := s.Poll(ctx); err != nil && ctx.Err() == nil {
			s.deps.Log.Error("опрос группы", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Poll делает один проход: забирает посты новее lastID и обрабатывает их по порядку.
func (s *Service) Poll(ctx context.Context) error {
	limit := 0
	if !s.started {
		limit = s.deps.Backfill
		s.started = true
	}
	posts, err := s.deps.Source.Fetch(ctx, s.lastID, limit)
	if err != nil {
		return err
	}
	if len(posts) == 0 {
		return nil
	}
	s.deps.Log.Info("новые посты", "count", len(posts))

	for _, batch := range batches(posts) {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.handle(ctx, batch)
	}
	return nil
}

// handle доводит один батч (пост или альбом) до форварда и сдвигает состояние.
func (s *Service) handle(ctx context.Context, batch []Post) {
	ids := make([]int, 0, len(batch))
	texts := make([]string, 0, len(batch))
	for _, post := range batch {
		ids = append(ids, post.ID)
		if text := strings.TrimSpace(post.Text); text != "" {
			texts = append(texts, text)
		}
	}
	last := ids[len(ids)-1]
	text := strings.Join(texts, "\n")

	// Post смещается вперёд даже при ошибке LLM или форварда: иначе один
	// проблемный пост останавливает весь поток. Теряем пост, а не поток.
	defer s.advance(last)

	if text == "" {
		s.deps.Log.Debug("пост без текста", "ids", ids)
		return
	}
	if !filter.Match(s.deps.Keywords, text) {
		s.deps.Log.Debug("нет кейвордов", "ids", ids)
		return
	}

	verdict, err := s.deps.Judge.Evaluate(ctx, text)
	if err != nil {
		s.deps.Log.Error("LLM", "ids", ids, "err", err)
		return
	}
	if !verdict.Matches {
		s.deps.Log.Info("не подходит", "ids", ids, "reason", verdict.Reason)
		return
	}
	if err := s.deps.Forward.Forward(ctx, ids); err != nil {
		s.deps.Log.Error("форвард", "ids", ids, "err", err)
		return
	}
	s.deps.Log.Info("переслано", "ids", ids, "reason", verdict.Reason)
}

func (s *Service) advance(id int) {
	if id <= s.lastID {
		return
	}
	s.lastID = id
	if err := s.deps.Store.Save(state.State{LastID: id}); err != nil {
		s.deps.Log.Error("сохранение состояния", "err", err)
	}
}

// batches склеивает подряд идущие посты одного альбома.
func batches(posts []Post) [][]Post {
	var out [][]Post
	for _, post := range posts {
		if post.Grouped != 0 {
			if n := len(out); n > 0 && out[n-1][0].Grouped == post.Grouped {
				out[n-1] = append(out[n-1], post)
				continue
			}
		}
		out = append(out, []Post{post})
	}
	return out
}
