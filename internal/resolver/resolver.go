package resolver

import (
	"fmt"

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
}

func Resolve(c *registry.Client, root map[string]string) ([]Resolved, error) {
	specs := map[string][]string{}
	for n, s := range root {
		specs[n] = append(specs[n], s)
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
	queue := []string{}
	for n := range specs {
		queue = append(queue, n)
	}

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		avail, err := availableVersions(name)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", name, err)
		}

		picked := ""
		for _, candidate := range descendingVersions(avail) {
			ok := true
			for _, spec := range specs[name] {
				rng, err := semvr.Range(spec)
				if err != nil {
					return nil, fmt.Errorf("bad spec for %s: %w", name, err)
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
			return nil, fmt.Errorf("no version of %s satisfies %v", name, specs[name])
		}

		if old, ok := resolved[name]; ok && old.Version == picked {
			continue
		}

		ver, err := c.GetVersion(name, picked)
		if err != nil {
			return nil, fmt.Errorf("fetching %s@%s: %w", name, picked, err)
		}

		integrity, url := pickAsset(c, ver)
		if integrity == "" {
			return nil, fmt.Errorf("%s@%s has no asset for platform %s",
				name, picked, currentAssetPlatformFor(ver.Kind))
		}

		resolved[name] = Resolved{
			Name:         name,
			Version:      picked,
			Kind:         ver.Kind,
			Integrity:    integrity,
			URL:          url,
			Dependencies: copyMap(ver.Dependencies),
		}

		for childName, childSpec := range ver.Dependencies {
			if !contains(specs[childName], childSpec) {
				specs[childName] = append(specs[childName], childSpec)
				queue = append(queue, childName)
			}
		}
	}

	out := make([]Resolved, 0, len(resolved))
	for _, r := range resolved {
		out = append(out, r)
	}
	return out, nil
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

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func copyMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
