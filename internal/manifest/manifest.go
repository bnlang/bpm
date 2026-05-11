package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Manifest struct {
	Name         string            `json:"name"`
	Version      string            `json:"version,omitempty"`
	Description  string            `json:"description,omitempty"`
	License      string            `json:"license,omitempty"`
	Homepage     string            `json:"homepage,omitempty"`
	Repository   string            `json:"repository,omitempty"` // e.g. https://github.com/user/repo
	Main         string            `json:"main,omitempty"`       // pure-bnl entry
	Native       string            `json:"native,omitempty"`     // installed: canonical plugin filename
	Dependencies map[string]string `json:"dependencies,omitempty"`

	Targets map[string]string `json:"targets,omitempty"`
}

const Filename = "bnl.json"

func Load(dir string) (*Manifest, error) {
	path := filepath.Join(dir, Filename)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &m, nil
}

func Save(dir string, m *Manifest) error {
	if m.Dependencies == nil {
		m.Dependencies = map[string]string{}
	}
	b, err := json.MarshalIndent(m, "", "    ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(dir, Filename), b, 0o644)
}

func (m *Manifest) Kind() string {
	if len(m.Targets) > 0 || m.Native != "" {
		return "native"
	}
	return "lib"
}

func (m *Manifest) AddDep(name, spec string) {
	if m.Dependencies == nil {
		m.Dependencies = map[string]string{}
	}
	m.Dependencies[name] = spec
}

func (m *Manifest) RemoveDep(name string) bool {
	if _, ok := m.Dependencies[name]; ok {
		delete(m.Dependencies, name)
		return true
	}
	return false
}
