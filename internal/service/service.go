// Package service — конвейер: пост → кейворды → вердикт LLM → форвард.
package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"slices"
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

// Seen хранит хеши уже пересланных текстов отдельным файлом.
type Seen interface {
	Load() ([]uint64, error)
	Save([]uint64) error
}

// Deps — всё, чем сервис пользуется снаружи.
type Deps struct {
	Source    Source
	Judge     Judge
	Forward   Forwarder
	Store     Store
	Seen      Seen
	Keywords  []string
	Backfill  int
	DedupSize int
	Interval  time.Duration
	Log       *slog.Logger
}

type Service struct {
	deps    Deps
	lastID  int
	seen    []uint64
	started bool
	ready   bool
}

func New(deps Deps) *Service {
	return &Service{deps: deps}
}

// Run опрашивает группу, пока не отменят контекст. Ошибки опроса не фатальны.
func (s *Service) Run(ctx context.Context) error {
	if err := s.init(); err != nil {
		return fmt.Errorf("состояние: %w", err)
	}
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
	if err := s.init(); err != nil {
		return err
	}
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

	// Повтор проверяем до LLM: тот же текст уже пересылали — не платим и не шлём.
	hash := textHash(text)
	if s.isRepeat(hash) {
		s.deps.Log.Info("повтор", "ids", ids)
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
	s.remember(hash)
	s.deps.Log.Info("переслано", "ids", ids, "reason", verdict.Reason)
}

// init один раз читает состояние прошлого запуска; Poll и Run зовут его лениво.
func (s *Service) init() error {
	if s.ready {
		return nil
	}
	st, err := s.deps.Store.Load()
	if err != nil {
		return err
	}
	s.lastID = st.LastID
	s.loadSeen()
	s.ready = true
	return nil
}

func (s *Service) loadSeen() {
	if s.deps.DedupSize <= 0 || s.deps.Seen == nil {
		return
	}
	hashes, err := s.deps.Seen.Load()
	if err != nil {
		// Битые хеши повторов не повод не работать: начинаем с пустого кольца.
		s.deps.Log.Warn("хеши повторов", "err", err)
		return
	}
	s.seen = hashes
	s.trimSeen()
}

// isRepeat сообщает, что этот текст уже пересылали в пределах кольца.
func (s *Service) isRepeat(hash uint64) bool {
	return s.deps.DedupSize > 0 && slices.Contains(s.seen, hash)
}

// remember кладёт хеш в кольцо и сохраняет его на диск.
func (s *Service) remember(hash uint64) {
	if s.deps.DedupSize <= 0 || s.deps.Seen == nil {
		return
	}
	s.seen = append(s.seen, hash)
	s.trimSeen()
	if err := s.deps.Seen.Save(s.seen); err != nil {
		s.deps.Log.Error("сохранение повторов", "err", err)
	}
}

// trimSeen вытесняет самые старые хеши, держа кольцо в пределах DedupSize.
func (s *Service) trimSeen() {
	if n := s.deps.DedupSize; n > 0 && len(s.seen) > n {
		s.seen = s.seen[:copy(s.seen, s.seen[len(s.seen)-n:])]
	}
}

// textHash — отпечаток текста без регистра и различий в пробелах: «Сдам  1к»
// и «сдам 1к» дают один хеш.
func textHash(text string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(strings.Join(strings.Fields(strings.ToLower(text)), " ")))
	return h.Sum64()
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
