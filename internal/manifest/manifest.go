package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type DepSpec struct {
	Version   string   `json:"version"`
	Platforms []string `json:"platforms,omitempty"`
	Optional  bool     `json:"optional,omitempty"`
}

func (d DepSpec) AppliesTo(plat string) bool {
	if len(d.Platforms) == 0 {
		return true
	}
	for _, p := range d.Platforms {
		if p == plat {
			return true
		}
	}
	return false
}

func (d DepSpec) Scoped() bool {
	return len(d.Platforms) > 0 || d.Optional
}

func (d *DepSpec) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		d.Version = s
		d.Platforms = nil
		d.Optional = false
		return nil
	}
	type raw DepSpec
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*d = DepSpec(r)
	return nil
}

func (d DepSpec) MarshalJSON() ([]byte, error) {
	if !d.Scoped() {
		return json.Marshal(d.Version)
	}
	type raw DepSpec
	return json.Marshal(raw(d))
}

type Manifest struct {
	Name         string             `json:"name"`
	Version      string             `json:"version,omitempty"`
	Description  string             `json:"description,omitempty"`
	License      string             `json:"license,omitempty"`
	Homepage     string             `json:"homepage,omitempty"`
	Repository   string             `json:"repository,omitempty"` // e.g. https://github.com/user/repo
	Main         string             `json:"main,omitempty"`       // pure-bnl entry
	Native       string             `json:"native,omitempty"`     // installed: canonical plugin filename
	Dependencies map[string]DepSpec `json:"dependencies,omitempty"`

	Targets map[string]string `json:"targets,omitempty"`
	Files   []string          `json:"files,omitempty"`
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
		m.Dependencies = map[string]DepSpec{}
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
		m.Dependencies = map[string]DepSpec{}
	}
	m.Dependencies[name] = DepSpec{Version: spec}
}

func (m *Manifest) AddScopedDep(name, spec string, platforms []string, optional bool) {
	if m.Dependencies == nil {
		m.Dependencies = map[string]DepSpec{}
	}
	m.Dependencies[name] = DepSpec{
		Version:   spec,
		Platforms: platforms,
		Optional:  optional,
	}
}

func (m *Manifest) RemoveDep(name string) bool {
	if _, ok := m.Dependencies[name]; ok {
		delete(m.Dependencies, name)
		return true
	}
	return false
}
