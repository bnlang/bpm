package paths

import (
	"errors"
	"os"
	"path/filepath"
)

func FindProjectRoot(start string) (string, error) {
	d, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		manifest := filepath.Join(d, "bnl.json")
		if info, err := os.Stat(manifest); err == nil && !info.IsDir() {
			parent := filepath.Base(filepath.Dir(d))
			if parent != "deps" {
				return d, nil
			}
		}
		next := filepath.Dir(d)
		if next == d {
			return "", errors.New("no bnl.json found in any parent directory (run `bpm init` first)")
		}
		d = next
	}
}

func BnlHome() string {
	if h := os.Getenv("BNL_HOME"); h != "" {
		return h
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".bnl")
	}
	return ".bnl"
}

func GlobalDepsDir() string {
	return filepath.Join(BnlHome(), "deps")
}

func AuthFile() string {
	return filepath.Join(BnlHome(), "auth.json")
}

func EnsureDir(p string) error {
	return os.MkdirAll(p, 0o755)
}
