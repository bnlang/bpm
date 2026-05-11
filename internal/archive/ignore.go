package archive

import (
	"errors"
	"os"
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"
)

type IgnoreMatcher func(rel string, isDir bool) bool

func LoadBpmIgnore(dir string) (IgnoreMatcher, error) {
	p := filepath.Join(dir, ".bpmignore")
	_, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return func(string, bool) bool { return false }, nil
	}
	if err != nil {
		return nil, err
	}
	gi, err := gitignore.CompileIgnoreFile(p)
	if err != nil {
		return nil, err
	}
	return func(rel string, _ bool) bool {
		return gi.MatchesPath(filepath.ToSlash(rel))
	}, nil
}
