// Package state — что сервис помнит между запусками: последний обработанный
// пост и хеши пересланных текстов.
package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// State — то, что сервис знает о прошлом запуске.
type State struct {
	LastID int `json:"last_id"`
}

// seen — содержимое файла с хешами: повтор этого текста уже пересылали.
type seen struct {
	Hashes []uint64 `json:"hashes"`
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
	var st State
	err := readJSON(s.path, &st)
	return st, err
}

func (s *Store) Save(st State) error {
	return writeJSON(s.path, st)
}

// SeenStore хранит кольцо хешей пересланных текстов отдельным файлом, чтобы
// state.json оставался одной строкой.
type SeenStore struct {
	path string
}

func NewSeen(path string) *SeenStore {
	return &SeenStore{path: path}
}

// Load возвращает пустой список, если файла ещё нет.
func (s *SeenStore) Load() ([]uint64, error) {
	var f seen
	err := readJSON(s.path, &f)
	return f.Hashes, err
}

func (s *SeenStore) Save(hashes []uint64) error {
	return writeJSON(s.path, seen{Hashes: hashes})
}

func readJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

// writeJSON пишет через временный файл, чтобы обрыв не оставил половину JSON.
func writeJSON(path string, src any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
