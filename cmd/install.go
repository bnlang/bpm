package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"bpm/internal/archive"
	"bpm/internal/lockfile"
	"bpm/internal/manifest"
	"bpm/internal/paths"
	"bpm/internal/platform"
	"bpm/internal/registry"
	"bpm/internal/resolver"
)

var (
	flagGlobal       bool
	flagIgnoreFailed bool
)

var installCmd = &cobra.Command{
	Use:   "install [name[@version] | path]",
	Short: "Install dependencies",
	Long: `Install dependencies into ./deps (default) or ~/.bnl/deps (with -g).

With no args, installs everything declared in bnl.json's "dependencies".
With one arg, installs that single package and adds it to bnl.json.

The argument can be:
  - a registry name              e.g.  bpm install sqlite
  - a registry name@version      e.g.  bpm install sqlite@0.2.0
  - a local path to a .tar.gz    e.g.  bpm install ./sqlite-0.2.0.tar.gz
  - a local path to a directory  e.g.  bpm install ../sqlite

Local-path installs are recorded in bnl.json as "<name>": "file:<path>" so
re-running 'bpm install' (no args) reinstalls from the same source.

Pass --ignore-failed to keep going when an individual package fails. The
failing package is skipped (no manifest/lock entry) and a summary is printed
at the end. The default is strict — any failure aborts.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInstall,
}

func init() {
	installCmd.Flags().BoolVarP(&flagGlobal, "global", "g", false, "install into ~/.bnl/deps")
	installCmd.Flags().BoolVar(&flagIgnoreFailed, "ignore-failed", false,
		"skip packages that fail to install instead of aborting")
	rootCmd.AddCommand(installCmd)
}

type failedPkg struct {
	name string // empty if unknown (e.g. local source couldn't be opened)
	spec string
	err  error
}

func (f failedPkg) label() string {
	switch {
	case f.name != "" && f.spec != "" && f.spec != f.name:
		return fmt.Sprintf("%s (%s)", f.name, f.spec)
	case f.name != "":
		return f.name
	default:
		return f.spec
	}
}

func warnSkip(label string, err error) {
	fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", label, err)
}

func reportFailed(failed []failedPkg) {
	if len(failed) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "\nskipped %d package(s):\n", len(failed))
	for _, f := range failed {
		fmt.Fprintf(os.Stderr, "  %s: %v\n", f.label(), f.err)
	}
}

type localInstallResult struct {
	Name         string
	Version      string
	Kind         string
	Integrity    string
	Dependencies map[string]string
	SpecPath     string // path stored back into bnl.json (without "file:" prefix)
	SourceAbs    string // directory used as the base for resolving this package's own file: deps
}

type localWork struct {
	baseDir      string
	spec         string
	expectedName string // if non-empty, installed package's name must match
}

func runInstall(cmd *cobra.Command, args []string) error {
	var (
		depsDir    string
		rootSpecs  map[string]string
		projectDir string
		projManif  *manifest.Manifest
		failed     []failedPkg

		// Pending single-arg add: applied to the manifest only on success.
		pendingRegistryName     string
		pendingRegistrySpec     string
		pendingRegistryExplicit bool
		pendingLocalName        string
		pendingLocalSpec        string
	)

	if flagGlobal {
		depsDir = paths.GlobalDepsDir()
		if len(args) == 0 {
			return fmt.Errorf("global install requires a package name or path")
		}
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		projectDir, err = paths.FindProjectRoot(cwd)
		if err != nil {
			return err
		}
		depsDir = filepath.Join(projectDir, "deps")
		projManif, err = manifest.Load(projectDir)
		if err != nil {
			return err
		}
		if projManif.Dependencies == nil {
			projManif.Dependencies = map[string]string{}
		}
	}

	// Single-arg local install: install up front so we can learn the package
	// name from its bnl.json, then either return early (global) or splice it
	// into the regular dep graph.
	var preInstalledLocals []localInstallResult
	if len(args) == 1 && isLocalSpec(args[0]) {
		baseDir := projectDir
		if flagGlobal {
			cwd, _ := os.Getwd()
			baseDir = cwd
		}
		if err := paths.EnsureDir(depsDir); err != nil {
			return err
		}
		res, err := installFromLocalPath(baseDir, depsDir, args[0])
		if err != nil {
			if !flagIgnoreFailed {
				return err
			}
			warnSkip(args[0], err)
			failed = append(failed, failedPkg{spec: args[0], err: err})
		} else {
			info("→ %s@%s (file)", res.Name, res.Version)
			if flagGlobal {
				info("installed 1 package(s) into %s", depsDir)
				reportFailed(failed)
				return nil
			}
			pendingLocalName = res.Name
			pendingLocalSpec = "file:" + res.SpecPath
			preInstalledLocals = append(preInstalledLocals, *res)
		}
		if flagGlobal {
			// Up-front local failed in lenient mode; nothing else to do.
			info("installed 0 package(s) into %s", depsDir)
			reportFailed(failed)
			return nil
		}
	}

	if flagGlobal {
		name, spec, _ := splitNameVersion(args[0])
		rootSpecs = map[string]string{name: spec}
	} else {
		rootSpecs = map[string]string{}
		for n, s := range projManif.Dependencies {
			rootSpecs[n] = s
		}
		if len(args) == 1 && !isLocalSpec(args[0]) {
			name, spec, explicit := splitNameVersion(args[0])
			rootSpecs[name] = spec
			pendingRegistryName = name
			pendingRegistrySpec = spec
			pendingRegistryExplicit = explicit
		}
		if len(rootSpecs) == 0 && len(preInstalledLocals) == 0 {
			info("no dependencies in %s", manifest.Filename)
			reportFailed(failed)
			return nil
		}
	}

	done := map[string]bool{}
	for _, p := range preInstalledLocals {
		done[p.Name] = true
	}
	registrySpecs := map[string]string{}
	var queue []localWork

	// Seed from the consumer's root specs.
	for n, s := range rootSpecs {
		if done[n] {
			continue
		}
		if strings.HasPrefix(s, "file:") {
			queue = append(queue, localWork{baseDir: projectDir, spec: s, expectedName: n})
		} else {
			registrySpecs[n] = s
		}
	}

	for _, p := range preInstalledLocals {
		for cn, cs := range p.Dependencies {
			if done[cn] {
				continue
			}
			if _, inRoot := rootSpecs[cn]; inRoot {
				continue
			}
			if strings.HasPrefix(cs, "file:") {
				queue = append(queue, localWork{baseDir: p.SourceAbs, spec: cs, expectedName: cn})
			} else if _, exists := registrySpecs[cn]; !exists {
				registrySpecs[cn] = cs
			}
		}
	}

	localResults := append([]localInstallResult{}, preInstalledLocals...)
	for len(queue) > 0 {
		w := queue[0]
		queue = queue[1:]
		if err := paths.EnsureDir(depsDir); err != nil {
			return err
		}
		res, err := installFromLocalPath(w.baseDir, depsDir, w.spec)
		if err != nil {
			if !flagIgnoreFailed {
				if w.expectedName != "" {
					return fmt.Errorf("installing %s from %s: %w", w.expectedName, w.spec, err)
				}
				return fmt.Errorf("installing from %s: %w", w.spec, err)
			}
			label := w.expectedName
			if label == "" {
				label = w.spec
			}
			warnSkip(label, err)
			failed = append(failed, failedPkg{name: w.expectedName, spec: w.spec, err: err})
			continue
		}
		if w.expectedName != "" && res.Name != w.expectedName {
			mismatch := fmt.Errorf("source bnl.json declares name %q", res.Name)
			if !flagIgnoreFailed {
				return fmt.Errorf("dependency %q at %s declares name %q in its bnl.json",
					w.expectedName, w.spec, res.Name)
			}
			warnSkip(w.expectedName, mismatch)
			failed = append(failed, failedPkg{name: w.expectedName, spec: w.spec, err: mismatch})
			continue
		}
		if done[res.Name] {
			continue
		}
		done[res.Name] = true
		info("→ %s@%s (file)", res.Name, res.Version)
		localResults = append(localResults, *res)

		for cn, cs := range res.Dependencies {
			if done[cn] {
				continue
			}
			if strings.HasPrefix(cs, "file:") {
				queue = append(queue, localWork{baseDir: res.SourceAbs, spec: cs, expectedName: cn})
			} else if _, exists := registrySpecs[cn]; !exists {
				registrySpecs[cn] = cs
			}
		}
	}

	var plan []resolver.Resolved
	if len(registrySpecs) > 0 {
		c, err := loadClient(false)
		if err != nil {
			return err
		}
		if flagIgnoreFailed {
			plan, failed = resolvePerRoot(c, registrySpecs, failed)
		} else {
			plan, err = resolver.Resolve(c, registrySpecs)
			if err != nil {
				return err
			}
		}
		if err := paths.EnsureDir(depsDir); err != nil {
			return err
		}
		successPlan := plan[:0]
		for _, r := range plan {
			info("→ %s@%s", r.Name, r.Version)
			if err := downloadAndUnpack(c, r, depsDir); err != nil {
				if !flagIgnoreFailed {
					return err
				}
				warnSkip(fmt.Sprintf("%s@%s", r.Name, r.Version), err)
				failed = append(failed, failedPkg{name: r.Name, spec: r.Version, err: err})
				continue
			}
			successPlan = append(successPlan, r)
		}
		plan = successPlan
	}

	if !flagGlobal {
		if pendingRegistryName != "" {
			for _, r := range plan {
				if r.Name == pendingRegistryName {
					if pendingRegistryExplicit {
						projManif.AddDep(pendingRegistryName, pendingRegistrySpec)
					} else {
						projManif.AddDep(pendingRegistryName, "^"+r.Version)
					}
					break
				}
			}
		}
		if pendingLocalName != "" {
			projManif.AddDep(pendingLocalName, pendingLocalSpec)
		}
		if err := manifest.Save(projectDir, projManif); err != nil {
			return err
		}
		l := lockfile.New()
		for _, r := range plan {
			l.Packages[r.Name] = lockfile.Entry{
				Version:      r.Version,
				Kind:         r.Kind,
				Integrity:    r.Integrity,
				Resolved:     r.URL,
				Dependencies: r.Dependencies,
			}
		}
		for _, lr := range localResults {
			l.Packages[lr.Name] = lockfile.Entry{
				Version:      lr.Version,
				Kind:         lr.Kind,
				Integrity:    lr.Integrity,
				Resolved:     "file:" + lr.SpecPath,
				Dependencies: lr.Dependencies,
			}
		}
		if err := l.Save(projectDir); err != nil {
			return err
		}
	}

	total := len(plan) + len(localResults)
	info("installed %d package(s) into %s", total, depsDir)
	reportFailed(failed)
	return nil
}

func resolvePerRoot(c *registry.Client, registrySpecs map[string]string, failed []failedPkg) ([]resolver.Resolved, []failedPkg) {
	names := make([]string, 0, len(registrySpecs))
	for n := range registrySpecs {
		names = append(names, n)
	}
	sort.Strings(names)

	seen := map[string]bool{}
	var plan []resolver.Resolved
	for _, n := range names {
		spec := registrySpecs[n]
		p, err := resolver.Resolve(c, map[string]string{n: spec})
		if err != nil {
			warnSkip(n, err)
			failed = append(failed, failedPkg{name: n, spec: spec, err: err})
			continue
		}
		for _, r := range p {
			if seen[r.Name] {
				continue
			}
			seen[r.Name] = true
			plan = append(plan, r)
		}
	}
	return plan, failed
}

func downloadAndUnpack(c *registry.Client, r resolver.Resolved, depsDir string) error {
	tmp, err := os.CreateTemp("", "bpm-*.tar.gz")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	assetPlat := "lib"
	if r.Kind == "native" {
		assetPlat = platform.Current()
	}
	if _, err := c.DownloadAsset(r.Name, r.Version, assetPlat, tmp.Name()); err != nil {
		return fmt.Errorf("downloading %s@%s: %w", r.Name, r.Version, err)
	}
	if err := verifyIntegrity(tmp.Name(), r.Integrity); err != nil {
		return fmt.Errorf("integrity mismatch for %s@%s: %w", r.Name, r.Version, err)
	}
	dst := filepath.Join(depsDir, r.Name)
	_ = os.RemoveAll(dst)
	if err := archive.UnpackTo(tmp.Name(), dst); err != nil {
		return err
	}
	if r.Kind == "native" {
		if err := normalizeNativeManifest(dst, r.Name); err != nil {
			return err
		}
	}
	return nil
}

func splitNameVersion(s string) (name, spec string, explicit bool) {
	at := strings.LastIndex(s, "@")
	if at <= 0 {
		return s, "*", false
	}
	return s[:at], s[at+1:], true
}

func verifyIntegrity(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := "sha256-" + hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("got %s, want %s", got, want)
	}
	return nil
}

func normalizeNativeManifest(depDir, depName string) error {
	m, err := manifest.Load(depDir)
	if err != nil {
		return manifest.Save(depDir, &manifest.Manifest{
			Name:   depName,
			Native: platform.LibraryFilename(depName),
		})
	}
	if m.Native == "" {
		m.Native = platform.LibraryFilename(depName)
	}
	return manifest.Save(depDir, m)
}

func isLocalSpec(s string) bool {
	if strings.HasPrefix(s, "file:") {
		return true
	}
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, ".\\") || strings.HasPrefix(s, "..\\") {
		return true
	}
	if filepath.IsAbs(s) {
		return true
	}
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		if _, err := os.Stat(s); err == nil {
			return true
		}
	}
	if strings.ContainsAny(s, "/\\") {
		if _, err := os.Stat(s); err == nil {
			return true
		}
	}
	return false
}

func installFromLocalPath(baseDir, depsDir, spec string) (*localInstallResult, error) {
	rawPath := strings.TrimPrefix(spec, "file:")
	abs := rawPath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(baseDir, filepath.FromSlash(rawPath))
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("local install %q: %w", spec, err)
	}

	var (
		tarball         string
		integrityStable bool
	)
	if st.IsDir() {
		tmp, err := os.CreateTemp("", "bpm-local-*.tar.gz")
		if err != nil {
			return nil, err
		}
		tmp.Close()
		tarball = tmp.Name()
		defer os.Remove(tarball)
		matcher, err := archive.LoadBpmIgnore(abs)
		if err != nil {
			return nil, err
		}
		if _, _, err := archive.PackDirWithIgnore(abs, tarball, matcher); err != nil {
			return nil, err
		}
		integrityStable = false
	} else {
		tarball = abs
		integrityStable = true
	}

	integrity := ""
	if integrityStable {
		integrity, err = sha256OfFile(tarball)
		if err != nil {
			return nil, err
		}
	}

	if err := paths.EnsureDir(depsDir); err != nil {
		return nil, err
	}
	incoming, err := os.MkdirTemp(depsDir, ".bpm-incoming-")
	if err != nil {
		return nil, err
	}
	cleanupIncoming := func() { _ = os.RemoveAll(incoming) }
	if err := archive.UnpackTo(tarball, incoming); err != nil {
		cleanupIncoming()
		return nil, err
	}
	m, err := manifest.Load(incoming)
	if err != nil {
		cleanupIncoming()
		return nil, fmt.Errorf("reading manifest from local source: %w", err)
	}
	if m.Name == "" {
		cleanupIncoming()
		return nil, fmt.Errorf("local install: source bnl.json is missing 'name'")
	}

	dst := filepath.Join(depsDir, m.Name)
	if err := os.RemoveAll(dst); err != nil {
		cleanupIncoming()
		return nil, err
	}
	if err := os.Rename(incoming, dst); err != nil {
		cleanupIncoming()
		return nil, fmt.Errorf("moving local install into place: %w", err)
	}

	if m.Kind() == "native" {
		if err := normalizeNativeManifest(dst, m.Name); err != nil {
			return nil, err
		}
	}

	specPath := computeSpecPath(baseDir, rawPath, abs)
	sourceAbs := abs
	if !st.IsDir() {
		sourceAbs = filepath.Dir(abs)
	}

	return &localInstallResult{
		Name:         m.Name,
		Version:      m.Version,
		Kind:         m.Kind(),
		Integrity:    integrity,
		Dependencies: m.Dependencies,
		SpecPath:     specPath,
		SourceAbs:    sourceAbs,
	}, nil
}

func computeSpecPath(baseDir, rawPath, abs string) string {
	if !filepath.IsAbs(rawPath) {
		return filepath.ToSlash(filepath.Clean(rawPath))
	}
	if rel, err := filepath.Rel(baseDir, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(abs)
}

func sha256OfFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256-" + hex.EncodeToString(h.Sum(nil)), nil
}
