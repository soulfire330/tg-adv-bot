// Package state — последний обработанный пост, переживает рестарт.
package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// State — то, что знает сервис о прошлом запуске.
type State struct {
	LastID int `json:"last_id"`
}

// Store читает и пишет состояние атомарно.
type Store struct {
	path string
}

func New(path string) *Store {
	return &Store{path: path}
}

// Load возвращает пустое состояние, если файла ещё нет.
func (s *Store) Load() (State, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, err
	}
	return st, nil
}

// Save пишет через временный файл, чтобы обрыв не оставил половину JSON.
func (s *Store) Save(st State) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
