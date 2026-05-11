package semvr

import (
	"fmt"
	"regexp"

	"github.com/Masterminds/semver/v3"
)

var (
	bareMajor      = regexp.MustCompile(`^\d+$`)
	bareMajorMinor = regexp.MustCompile(`^\d+\.\d+$`)
	exactSemver    = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[\w.-]+)?$`)
)

func Normalize(spec string) string {
	switch {
	case bareMajor.MatchString(spec):
		return "^" + spec + ".0.0"
	case bareMajorMinor.MatchString(spec):
		return "~" + spec + ".0"
	case exactSemver.MatchString(spec):
		return spec
	default:
		return spec
	}
}

func Range(spec string) (*semver.Constraints, error) {
	c, err := semver.NewConstraint(Normalize(spec))
	if err != nil {
		return nil, fmt.Errorf("invalid version spec %q: %w", spec, err)
	}
	return c, nil
}

func PickHighest(spec string, available []string) (string, error) {
	c, err := Range(spec)
	if err != nil {
		return "", err
	}
	var best *semver.Version
	for _, v := range available {
		ver, err := semver.NewVersion(v)
		if err != nil {
			continue
		}
		if !c.Check(ver) {
			continue
		}
		if best == nil || ver.GreaterThan(best) {
			best = ver
		}
	}
	if best == nil {
		return "", fmt.Errorf("no version of available list satisfies %q", spec)
	}
	return best.Original(), nil
}
