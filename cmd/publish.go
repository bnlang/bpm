package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"bpm/internal/archive"
	"bpm/internal/manifest"
	"bpm/internal/paths"
	"bpm/internal/platform"
	"bpm/internal/registry"
)

var (
	publishPlatform string
	publishBinary   string
)

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish the package in the current directory",
	Long: `Publish the current package to the active registry.

Kind is detected from bnl.json:
  - "main" field set    → kind=lib    (tar the project root)
  - "targets" set       → kind=native (one tarball per platform)

Both kinds respect a project-root .bpmignore (gitignore syntax), composed on
top of the built-in skip rules (build/, deps/, .git/, *.tar.gz, ...).

Native packages declare per-platform binary paths via "targets":

  {
    "name": "sqlite",
    "version": "0.2.0",
    "targets": {
      "windows-x64":  "build/windows/bin/sqlite.dll",
      "linux-x64":    "build/linux/bin/sqlite.so",
      "darwin-arm64": "build/macos/bin/sqlite.dylib"
    }
  }

By default, ` + "`bpm publish`" + ` on a native package uploads every platform in
` + "`targets`" + ` in one go — pre-flighting that each binary exists on disk, then
posting one tarball per platform. Repeating the command is idempotent: any
platform already on the registry at this version is skipped (409 → skip).

Pass --platform <name> to narrow to a single platform (handy for CI matrix
runs, or to retry one missing platform). Pass --binary to override the path
read from targets[<platform>].`,
	RunE: runPublish,
}

func init() {
	publishCmd.Flags().StringVar(&publishPlatform, "platform", "",
		"asset platform (lib | windows-x64 | linux-x64 | darwin-arm64 | ...). "+
			"If empty for a native package, every platform in `targets` is published.")
	publishCmd.Flags().StringVar(&publishBinary, "binary", "",
		"path to the built plugin binary (overrides targets.<platform>). "+
			"Requires --platform.")
	rootCmd.AddCommand(publishCmd)
}

func runPublish(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	projectDir, err := paths.FindProjectRoot(cwd)
	if err != nil {
		return err
	}
	m, err := manifest.Load(projectDir)
	if err != nil {
		return err
	}
	if m.Name == "" || m.Version == "" {
		return fmt.Errorf("bnl.json must declare 'name' and 'version' to publish")
	}

	c, err := loadClient(true)
	if err != nil {
		return err
	}

	matcher, err := archive.LoadBpmIgnore(projectDir)
	if err != nil {
		return fmt.Errorf("loading .bpmignore: %w", err)
	}

	meta := registry.PublishMetadata{
		Kind:         m.Kind(),
		Dependencies: m.Dependencies,
		Description:  m.Description,
		License:      m.License,
		Homepage:     m.Homepage,
		Repository:   m.Repository,
	}

	if m.Kind() == "lib" {
		return publishLibPackage(c, m, projectDir, matcher, meta)
	}
	return publishNativePackage(c, m, projectDir, matcher, meta)
}

func publishLibPackage(c *registry.Client, m *manifest.Manifest, projectDir string,
	matcher archive.IgnoreMatcher, meta registry.PublishMetadata) error {

	tarball, err := makeTempTarball()
	if err != nil {
		return err
	}
	defer os.Remove(tarball)

	if _, _, err := archive.PackDirWithIgnore(projectDir, tarball, matcher); err != nil {
		return err
	}
	res, err := c.Publish(m.Name, m.Version, "lib", tarball, meta)
	if err != nil {
		if registry.IsConflict(err) {
			info("nothing to publish: %s@%s is already on the registry.", m.Name, m.Version)
			info("bump \"version\" in bnl.json to publish a new release.")
			return nil
		}
		return err
	}
	info("publish  %s@%s  (%s)  — %d bytes — %s",
		res.Name, res.Version, res.Platform, res.SizeBytes, res.Integrity)
	return nil
}

func publishNativePackage(c *registry.Client, m *manifest.Manifest, projectDir string,
	matcher archive.IgnoreMatcher, meta registry.PublishMetadata) error {

	if publishBinary != "" && publishPlatform == "" {
		publishPlatform = platform.Current()
	}

	var platforms []string
	if publishPlatform != "" {
		platforms = []string{publishPlatform}
	} else {
		if len(m.Targets) == 0 {
			return fmt.Errorf("native publish: manifest has no `targets` map.\n" +
				"Add a `targets` entry to bnl.json, or pass --platform + --binary.")
		}
		platforms = make([]string, 0, len(m.Targets))
		for p := range m.Targets {
			platforms = append(platforms, p)
		}
		sort.Strings(platforms)
	}

	type job struct {
		plat string
		bin  string
	}
	jobs := make([]job, 0, len(platforms))
	var missing []string
	for _, plat := range platforms {
		bin, err := resolveNativeBinary(m, projectDir, plat)
		if err != nil {
			return err
		}
		if _, err := os.Stat(bin); err != nil {
			rel, relErr := filepath.Rel(projectDir, bin)
			if relErr != nil || strings.HasPrefix(rel, "..") {
				rel = bin
			}
			missing = append(missing, fmt.Sprintf("  %-14s %s", plat, rel))
			continue
		}
		jobs = append(jobs, job{plat: plat, bin: bin})
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"missing binaries for %s@%s:\n%s\n"+
				"build them first (e.g. .\\build.ps1 on Windows or ./build.sh on Linux/macOS), "+
				"then re-run bpm publish.",
			m.Name, m.Version, strings.Join(missing, "\n"))
	}

	published, skipped := 0, 0
	for _, j := range jobs {
		tarball, err := makeTempTarball()
		if err != nil {
			return err
		}
		if err := packNative(projectDir, m, j.bin, matcher, tarball); err != nil {
			os.Remove(tarball)
			return err
		}
		res, err := c.Publish(m.Name, m.Version, j.plat, tarball, meta)
		os.Remove(tarball)
		if err != nil {
			if registry.IsConflict(err) {
				info("skip     %s@%s  (%s)  — already published", m.Name, m.Version, j.plat)
				skipped++
				continue
			}
			return fmt.Errorf("publish %s/%s: %w", m.Name, j.plat, err)
		}
		info("publish  %s@%s  (%s)  — %d bytes — %s",
			res.Name, res.Version, res.Platform, res.SizeBytes, res.Integrity)
		published++
	}

	total := len(jobs)
	switch {
	case published == 0 && skipped == total:
		info("nothing to publish: %s@%s already has all %d platform(s) on the registry.",
			m.Name, m.Version, total)
		info("bump \"version\" in bnl.json to publish a new release.")
	case skipped == 0:
		info("done: %d platform(s) published", published)
	default:
		info("done: %d platform(s) published, %d already up", published, skipped)
	}
	return nil
}

func makeTempTarball() (string, error) {
	tmp, err := os.CreateTemp("", "bpm-publish-*.tar.gz")
	if err != nil {
		return "", err
	}
	tmp.Close()
	return tmp.Name(), nil
}

func resolveNativeBinary(m *manifest.Manifest, projectDir, plat string) (string, error) {
	if publishBinary != "" {
		return publishBinary, nil
	}
	if len(m.Targets) == 0 {
		return "", fmt.Errorf(
			"native publish: manifest has no `targets` map. "+
				"Add a `targets[%q]` entry to bnl.json, or pass --binary.", plat)
	}
	rel, ok := m.Targets[plat]
	if !ok {
		return "", fmt.Errorf("native publish: targets has no entry for platform %q", plat)
	}
	return filepath.Join(projectDir, filepath.FromSlash(rel)), nil
}

func packNative(projectDir string, m *manifest.Manifest, binary string, matcher archive.IgnoreMatcher, dst string) error {
	binSt, err := os.Stat(binary)
	if err != nil {
		return fmt.Errorf("binary: %w", err)
	}
	if binSt.IsDir() {
		return fmt.Errorf("binary %s is a directory", binary)
	}

	binDir := filepath.Dir(binary)
	binaryBase := filepath.Base(binary)
	canonical := platform.LibraryFilename(m.Name)

	tmpdir, err := os.MkdirTemp("", "bpm-pack-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	walkErr := filepath.Walk(binDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(binDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		var dstName string
		if rel == binaryBase {
			dstName = canonical
		} else {
			dstName = rel
		}

		if matcher != nil && matcher(dstName, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		target := filepath.Join(tmpdir, dstName)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
	if walkErr != nil {
		return fmt.Errorf("staging bin dir %s: %w", binDir, walkErr)
	}

	if readme := findReadme(projectDir); readme != "" {
		if err := copyFile(readme, filepath.Join(tmpdir, filepath.Base(readme))); err != nil {
			return fmt.Errorf("staging readme: %w", err)
		}
	}

	staged := &manifest.Manifest{
		Name:         m.Name,
		Version:      m.Version,
		Description:  m.Description,
		License:      m.License,
		Homepage:     m.Homepage,
		Repository:   m.Repository,
		Native:       canonical,
		Dependencies: m.Dependencies,
	}
	if err := manifest.Save(tmpdir, staged); err != nil {
		return err
	}

	_, _, err = archive.PackDir(tmpdir, dst)
	return err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func findReadme(projectDir string) string {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := e.Name()
		ext := strings.ToLower(filepath.Ext(base))
		stem := strings.ToLower(strings.TrimSuffix(base, ext))
		if stem == "readme" {
			return filepath.Join(projectDir, base)
		}
	}
	return ""
}
