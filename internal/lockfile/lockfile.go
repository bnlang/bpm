package lockfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const Filename = "bnl.lock"

type Entry struct {
	Version      string            `json:"version"`
	Kind         string            `json:"kind"`         // "lib" | "native"
	Integrity    string            `json:"integrity"`    // "sha256-<hex>"
	Resolved     string            `json:"resolved"`     // download URL
	Dependencies map[string]string `json:"dependencies"` // exact pinned versions of children
}

type Lockfile struct {
	Version  int              `json:"version"`
	Packages map[string]Entry `json:"packages"`
}

func New() *Lockfile {
	return &Lockfile{Version: 1, Packages: map[string]Entry{}}
}

func Load(dir string) (*Lockfile, error) {
	b, err := os.ReadFile(filepath.Join(dir, Filename))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l Lockfile
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	if l.Packages == nil {
		l.Packages = map[string]Entry{}
	}
	return &l, nil
}

func (l *Lockfile) Save(dir string) error {
	b, err := json.MarshalIndent(l, "", "    ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(dir, Filename), b, 0o644)
}
