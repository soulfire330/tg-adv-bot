package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"tg-adv-bot/internal/judge"
	"tg-adv-bot/internal/state"
)

type fakeSource struct {
	posts   []Post
	err     error
	afterID int
	limits  []int
}

func (f *fakeSource) Fetch(_ context.Context, afterID, limit int) ([]Post, error) {
	f.afterID = afterID
	f.limits = append(f.limits, limit)
	if f.err != nil {
		return nil, f.err
	}
	return f.posts, nil
}

type fakeJudge struct {
	verdict judge.Verdict
	err     error
	texts   []string
}

func (f *fakeJudge) Evaluate(_ context.Context, text string) (judge.Verdict, error) {
	f.texts = append(f.texts, text)
	if f.err != nil {
		return judge.Verdict{}, f.err
	}
	return f.verdict, nil
}

type fakeForwarder struct {
	batches [][]int
	err     error
}

func (f *fakeForwarder) Forward(_ context.Context, ids []int) error {
	f.batches = append(f.batches, ids)
	return f.err
}

type fakeStore struct {
	loaded state.State
	saved  []state.State
}

func (f *fakeStore) Load() (state.State, error) { return f.loaded, nil }

func (f *fakeStore) Save(st state.State) error {
	f.saved = append(f.saved, st)
	return nil
}

type fakeSeen struct {
	loaded []uint64
	saved  [][]uint64
}

func (f *fakeSeen) Load() ([]uint64, error) { return f.loaded, nil }

func (f *fakeSeen) Save(hashes []uint64) error {
	f.loaded = slices.Clone(hashes)
	f.saved = append(f.saved, slices.Clone(hashes))
	return nil
}

func newService(deps Deps) *Service {
	if deps.Log == nil {
		deps.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if deps.Backfill == 0 {
		deps.Backfill = 3
	}
	if deps.Interval == 0 {
		deps.Interval = time.Second
	}
	if deps.Keywords == nil {
		deps.Keywords = []string{"сдам"}
	}
	return New(deps)
}

func TestPollForwardsMatch(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: true, Reason: "подходит"}}
	forwarder := &fakeForwarder{}
	store := &fakeStore{}
	source := &fakeSource{posts: []Post{{ID: 10, Text: "сдам квартиру"}}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: store})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(llm.texts) != 1 || !strings.Contains(llm.texts[0], "сдам") {
		t.Errorf("LLM получил %q", llm.texts)
	}
	if len(forwarder.batches) != 1 || !slices.Equal(forwarder.batches[0], []int{10}) {
		t.Errorf("форварды = %v", forwarder.batches)
	}
	if lastID := lastSaved(t, store); lastID != 10 {
		t.Errorf("state.LastID = %d, want 10", lastID)
	}
}

func TestPollSkipsPostWithoutKeyword(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: true}}
	forwarder := &fakeForwarder{}
	store := &fakeStore{}
	source := &fakeSource{posts: []Post{{ID: 11, Text: "продам диван"}}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: store})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(llm.texts) != 0 {
		t.Errorf("LLM не должен вызываться: %q", llm.texts)
	}
	if len(forwarder.batches) != 0 {
		t.Errorf("форвардов = %d, want 0", len(forwarder.batches))
	}
	if lastID := lastSaved(t, store); lastID != 11 {
		t.Errorf("state.LastID = %d, want 11", lastID)
	}
}

func TestPollSkipsRejectedPost(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: false, Reason: "не то"}}
	forwarder := &fakeForwarder{}
	store := &fakeStore{}
	source := &fakeSource{posts: []Post{{ID: 12, Text: "сдам гараж"}}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: store})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(llm.texts) != 1 {
		t.Errorf("LLM вызван %d раз, want 1", len(llm.texts))
	}
	if len(forwarder.batches) != 0 {
		t.Errorf("форвардов = %d, want 0", len(forwarder.batches))
	}
	if lastID := lastSaved(t, store); lastID != 12 {
		t.Errorf("state.LastID = %d, want 12", lastID)
	}
}

func TestPollSkipsEmptyText(t *testing.T) {
	llm := &fakeJudge{}
	forwarder := &fakeForwarder{}
	store := &fakeStore{}
	source := &fakeSource{posts: []Post{{ID: 13, Text: "   "}}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: store})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(llm.texts) != 0 {
		t.Errorf("LLM не должен вызываться: %q", llm.texts)
	}
	if lastID := lastSaved(t, store); lastID != 13 {
		t.Errorf("state.LastID = %d, want 13", lastID)
	}
}

func TestPollForwardsAlbumAsOneMessage(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: true}}
	forwarder := &fakeForwarder{}
	store := &fakeStore{}
	source := &fakeSource{posts: []Post{
		{ID: 20, Grouped: 777, Text: "сдам велосипед"},
		{ID: 21, Grouped: 777},
	}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: store})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(llm.texts) != 1 || !strings.Contains(llm.texts[0], "велосипед") {
		t.Errorf("LLM получил %q", llm.texts)
	}
	if len(forwarder.batches) != 1 || !slices.Equal(forwarder.batches[0], []int{20, 21}) {
		t.Errorf("форварды = %v", forwarder.batches)
	}
	if lastID := lastSaved(t, store); lastID != 21 {
		t.Errorf("state.LastID = %d, want 21", lastID)
	}
}

func TestPollKeepsAlbumsSeparate(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: true}}
	forwarder := &fakeForwarder{}
	source := &fakeSource{posts: []Post{
		{ID: 30, Grouped: 1, Text: "сдам стол"},
		{ID: 31, Grouped: 2, Text: "сдам стул"},
	}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: &fakeStore{}})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(forwarder.batches) != 2 {
		t.Fatalf("форвардов = %d, want 2", len(forwarder.batches))
	}
}

func TestPollSurvivesLLMFailure(t *testing.T) {
	llm := &fakeJudge{err: errors.New("503")}
	forwarder := &fakeForwarder{}
	store := &fakeStore{}
	source := &fakeSource{posts: []Post{{ID: 40, Text: "сдам"}}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: store})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll не должен падать: %v", err)
	}

	if len(forwarder.batches) != 0 {
		t.Errorf("форвардов = %d, want 0", len(forwarder.batches))
	}
	if lastID := lastSaved(t, store); lastID != 40 {
		t.Errorf("state.LastID = %d, want 40", lastID)
	}
}

func TestPollSurvivesForwardFailure(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: true}}
	forwarder := &fakeForwarder{err: errors.New("FLOOD_WAIT")}
	store := &fakeStore{}
	source := &fakeSource{posts: []Post{{ID: 41, Text: "сдам"}}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: store})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll не должен падать: %v", err)
	}
	if lastID := lastSaved(t, store); lastID != 41 {
		t.Errorf("state.LastID = %d, want 41", lastID)
	}
}

func TestRunStartsFromStoredID(t *testing.T) {
	source := &fakeSource{}
	store := &fakeStore{loaded: state.State{LastID: 55}}
	svc := newService(Deps{Source: source, Judge: &fakeJudge{}, Forward: &fakeForwarder{}, Store: store})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := svc.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if source.afterID != 55 {
		t.Errorf("afterID = %d, want 55", source.afterID)
	}
}

func TestBackfillLimitOnlyOnFirstPoll(t *testing.T) {
	source := &fakeSource{}
	svc := newService(Deps{Source: source, Judge: &fakeJudge{}, Forward: &fakeForwarder{}, Store: &fakeStore{}})

	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	want := []int{3, 0}
	if !slices.Equal(source.limits, want) {
		t.Errorf("limits = %v, want %v", source.limits, want)
	}
}

func TestPollSkipsRepeatedText(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: true}}
	forwarder := &fakeForwarder{}
	seen := &fakeSeen{}
	source := &fakeSource{posts: []Post{
		{ID: 50, Text: "Сдам  1к квартиру"},
		{ID: 51, Text: "сдам 1к квартиру"},
	}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: &fakeStore{},
		Seen: seen, DedupSize: 100})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(llm.texts) != 1 {
		t.Errorf("LLM вызван %d раз, want 1", len(llm.texts))
	}
	if len(forwarder.batches) != 1 || !slices.Equal(forwarder.batches[0], []int{50}) {
		t.Errorf("форварды = %v, want только [50]", forwarder.batches)
	}
	if len(seen.saved) != 1 || len(seen.saved[0]) != 1 {
		t.Errorf("сохранённые хеши = %v, want один", seen.saved)
	}
}

func TestPollSkipsRepeatFromPreviousRun(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: true}}
	forwarder := &fakeForwarder{}
	store := &fakeStore{}
	seen := &fakeSeen{loaded: []uint64{textHash("сдам 1к квартиру")}}
	source := &fakeSource{posts: []Post{{ID: 60, Text: "Сдам 1к квартиру"}}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: store,
		Seen: seen, DedupSize: 100})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(llm.texts) != 0 {
		t.Errorf("LLM не должен вызываться: %q", llm.texts)
	}
	if len(forwarder.batches) != 0 {
		t.Errorf("форвардов = %d, want 0", len(forwarder.batches))
	}
	if lastID := lastSaved(t, store); lastID != 60 {
		t.Errorf("state.LastID = %d, want 60", lastID)
	}
}

func TestPollDoesNotRememberFailedForward(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: true}}
	forwarder := &fakeForwarder{err: errors.New("FLOOD_WAIT")}
	seen := &fakeSeen{}
	source := &fakeSource{posts: []Post{{ID: 70, Text: "сдам гараж"}}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: &fakeStore{},
		Seen: seen, DedupSize: 100})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(seen.saved) != 0 {
		t.Errorf("хеши сохранены после провала форварда: %v", seen.saved)
	}
}

func TestPollForgetsBeyondDedupSize(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: true}}
	forwarder := &fakeForwarder{}
	source := &fakeSource{posts: []Post{
		{ID: 80, Text: "сдам стол"},
		{ID: 81, Text: "сдам стул"},
		{ID: 82, Text: "сдам стол"},
	}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: &fakeStore{},
		Seen: &fakeSeen{}, DedupSize: 1})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	// Кольцо на один хеш: первый текст вытеснён вторым — третий пост проходит.
	if len(forwarder.batches) != 3 {
		t.Errorf("форвардов = %d, want 3: %v", len(forwarder.batches), forwarder.batches)
	}
}

func TestPollWithDedupDisabledForwardsRepeats(t *testing.T) {
	llm := &fakeJudge{verdict: judge.Verdict{Matches: true}}
	forwarder := &fakeForwarder{}
	seen := &fakeSeen{}
	source := &fakeSource{posts: []Post{
		{ID: 90, Text: "сдам велосипед"},
		{ID: 91, Text: "сдам велосипед"},
	}}

	svc := newService(Deps{Source: source, Judge: llm, Forward: forwarder, Store: &fakeStore{}, Seen: seen})
	if err := svc.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if len(forwarder.batches) != 2 {
		t.Errorf("форвардов = %d, want 2", len(forwarder.batches))
	}
	if len(seen.saved) != 0 {
		t.Errorf("хеши сохранены при DedupSize=0: %v", seen.saved)
	}
}

func lastSaved(t *testing.T, store *fakeStore) int {
	t.Helper()
	if len(store.saved) == 0 {
		t.Fatal("состояние не сохранялось")
	}
	return store.saved[len(store.saved)-1].LastID
}
