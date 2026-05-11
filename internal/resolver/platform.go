package resolver

import (
	"sort"

	mmsv "github.com/Masterminds/semver/v3"

	"bpm/internal/platform"
)

func descendingVersions(versions []string) []string {
	parsed := make([]*mmsv.Version, 0, len(versions))
	for _, v := range versions {
		if pv, err := mmsv.NewVersion(v); err == nil {
			parsed = append(parsed, pv)
		}
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].GreaterThan(parsed[j]) })
	out := make([]string, 0, len(parsed))
	for _, v := range parsed {
		out = append(out, v.Original())
	}
	return out
}

func semverParse(s string) (*mmsv.Version, error) {
	return mmsv.NewVersion(s)
}

func currentAssetPlatformFor(kind string) string {
	if kind == "lib" {
		return "lib"
	}
	return platform.Current()
}
