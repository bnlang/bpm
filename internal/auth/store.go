package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"bpm/internal/paths"
)

type Store struct {
	Tokens map[string]string `json:"tokens"`
}

func file() string { return paths.AuthFile() }

func Load() (*Store, error) {
	b, err := os.ReadFile(file())
	if errors.Is(err, os.ErrNotExist) {
		return &Store{Tokens: map[string]string{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Tokens == nil {
		s.Tokens = map[string]string{}
	}
	return &s, nil
}

func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(file()), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file(), b, 0o600)
}

func (s *Store) Get(registry string) string { return s.Tokens[registry] }

func (s *Store) Set(registry, token string) {
	if s.Tokens == nil {
		s.Tokens = map[string]string{}
	}
	s.Tokens[registry] = token
}

func (s *Store) Clear(registry string) { delete(s.Tokens, registry) }
