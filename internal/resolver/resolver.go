package resolver

import (
	"fmt"

	"bpm/internal/manifest"
	"bpm/internal/platform"
	"bpm/internal/registry"
	"bpm/internal/semvr"
)

type Resolved struct {
	Name         string
	Version      string
	Kind         string
	Integrity    string
	URL          string
	Dependencies map[string]string
	Optional     bool
}

type Skipped struct {
	Name   string
	Reason string
}

type effective struct {
	specs     []string
	platforms map[string]bool
	optional  bool
}

func (e *effective) merge(d manifest.DepSpec, firstSeen bool) {
	e.specs = appendUnique(e.specs, d.Version)
	if firstSeen {
		e.optional = d.Optional
	} else if !d.Optional {
		// A non-optional parent demand wins: dep becomes required.
		e.optional = false
	}
	if len(d.Platforms) == 0 {
		// "All platforms" widens to all.
		e.platforms = nil
		return
	}
	if !firstSeen && e.platforms == nil {
		// Already widened to all — stay there.
		return
	}
	if firstSeen {
		e.platforms = map[string]bool{}
	}
	for _, p := range d.Platforms {
		e.platforms[p] = true
	}
}

func (e *effective) appliesTo(plat string) bool {
	if e.platforms == nil {
		return true
	}
	return e.platforms[plat]
}

func Resolve(c *registry.Client, root map[string]manifest.DepSpec) ([]Resolved, []Skipped, error) {
	plat := platform.Current()

	state := map[string]*effective{}
	queue := []string{}
	enqueue := func(name string, d manifest.DepSpec) {
		s, ok := state[name]
		if !ok {
			s = &effective{}
			state[name] = s
			s.merge(d, true)
			if s.appliesTo(plat) {
				queue = append(queue, name)
			}
			return
		}
		s.merge(d, false)
		if s.appliesTo(plat) {
			queue = append(queue, name)
		}
	}

	for n, d := range root {
		enqueue(n, d)
	}

	versionsCache := map[string][]string{}
	availableVersions := func(name string) ([]string, error) {
		if v, ok := versionsCache[name]; ok {
			return v, nil
		}
		info, err := c.GetPackage(name)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(info.Versions))
		for _, v := range info.Versions {
			out = append(out, v.Version)
		}
		versionsCache[name] = out
		return out, nil
	}

	resolved := map[string]Resolved{}
	var skipped []Skipped

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		eff := state[name]
		if !eff.appliesTo(plat) {
			continue
		}

		avail, err := availableVersions(name)
		if err != nil {
			if eff.optional {
				skipped = appendSkipped(skipped, name, err.Error())
				continue
			}
			return nil, nil, fmt.Errorf("fetching %s: %w", name, err)
		}

		picked := ""
		for _, candidate := range descendingVersions(avail) {
			ok := true
			for _, spec := range eff.specs {
				rng, err := semvr.Range(spec)
				if err != nil {
					return nil, nil, fmt.Errorf("bad spec for %s: %w", name, err)
				}
				v, err := semverParse(candidate)
				if err != nil {
					ok = false
					break
				}
				if !rng.Check(v) {
					ok = false
					break
				}
			}
			if ok {
				picked = candidate
				break
			}
		}
		if picked == "" {
			msg := fmt.Sprintf("no version of %s satisfies %v", name, eff.specs)
			if eff.optional {
				skipped = appendSkipped(skipped, name, msg)
				continue
			}
			return nil, nil, fmt.Errorf("%s", msg)
		}

		if old, ok := resolved[name]; ok && old.Version == picked {
			continue
		}

		ver, err := c.GetVersion(name, picked)
		if err != nil {
			if eff.optional {
				skipped = appendSkipped(skipped, name, err.Error())
				continue
			}
			return nil, nil, fmt.Errorf("fetching %s@%s: %w", name, picked, err)
		}

		integrity, url := pickAsset(c, ver)
		if integrity == "" {
			msg := fmt.Sprintf("%s@%s has no asset for platform %s",
				name, picked, currentAssetPlatformFor(ver.Kind))
			if eff.optional {
				skipped = appendSkipped(skipped, name, msg)
				continue
			}
			return nil, nil, fmt.Errorf("%s", msg)
		}

		resolved[name] = Resolved{
			Name:         name,
			Version:      picked,
			Kind:         ver.Kind,
			Integrity:    integrity,
			URL:          url,
			Dependencies: flattenSpecs(ver.Dependencies),
			Optional:     eff.optional,
		}

		for childName, childSpec := range ver.Dependencies {
			enqueue(childName, childSpec)
		}
	}

	out := make([]Resolved, 0, len(resolved))
	for _, r := range resolved {
		out = append(out, r)
	}
	return out, skipped, nil
}

func pickAsset(c *registry.Client, v *registry.Version) (integrity, url string) {
	plat := currentAssetPlatformFor(v.Kind)
	for _, a := range v.Assets {
		if a.Platform == plat {
			return a.Integrity, fmt.Sprintf("%s/v1/p/%s/%s/asset/%s",
				c.BaseURL, v.Name, v.Version, a.Platform)
		}
	}
	return "", ""
}

func appendUnique(xs []string, x string) []string {
	for _, s := range xs {
		if s == x {
			return xs
		}
	}
	return append(xs, x)
}

func appendSkipped(xs []Skipped, name, reason string) []Skipped {
	for _, s := range xs {
		if s.Name == name {
			return xs
		}
	}
	return append(xs, Skipped{Name: name, Reason: reason})
}

func flattenSpecs(m map[string]manifest.DepSpec) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v.Version
	}
	return out
}
