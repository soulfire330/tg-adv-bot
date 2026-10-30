package state

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSaveLoadRoundtrip(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "state.json"))

	if err := store.Save(State{LastID: 42}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LastID != 42 {
		t.Errorf("LastID = %d, want 42", got.LastID)
	}

	if err := store.Save(State{LastID: 43}); err != nil {
		t.Fatalf("повторный Save: %v", err)
	}
	got, err = store.Load()
	if err != nil {
		t.Fatalf("повторный Load: %v", err)
	}
	if got.LastID != 43 {
		t.Errorf("LastID = %d, want 43", got.LastID)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "нет.json"))

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LastID != 0 {
		t.Errorf("LastID = %d, want 0", got.LastID)
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "state.json")

	if err := New(path).Save(State{LastID: 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("файл не создан: %v", err)
	}
}

func TestSeenRoundtrip(t *testing.T) {
	store := NewSeen(filepath.Join(t.TempDir(), "dedup.json"))
	hashes := []uint64{1, 1 << 63, ^uint64(0)}

	if err := store.Save(hashes); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(got, hashes) {
		t.Errorf("hashes = %v, want %v", got, hashes)
	}
}

func TestSeenLoadMissingFileIsEmpty(t *testing.T) {
	got, err := NewSeen(filepath.Join(t.TempDir(), "нет.json")).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("hashes = %v, want 0 штук", got)
	}
}
