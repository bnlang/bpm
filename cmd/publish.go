package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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

The CLI picks targets[<platform>], takes that file's enclosing directory as
the package root (sibling DLLs come along), and renames the binary itself to
the canonical <name>.<ext>. Use --binary to override per invocation.`,
	RunE: runPublish,
}

func init() {
	publishCmd.Flags().StringVar(&publishPlatform, "platform", "",
		"asset platform (lib | windows-x64 | linux-x64 | darwin-arm64 | ...)")
	publishCmd.Flags().StringVar(&publishBinary, "binary", "",
		"path to the built plugin binary (overrides targets.<platform>)")
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

	kind := m.Kind()

	tmp, err := os.CreateTemp("", "bpm-publish-*.tar.gz")
	if err != nil {
		return err
	}
	tmp.Close()
	tarball := tmp.Name()
	defer os.Remove(tarball)

	matcher, err := archive.LoadBpmIgnore(projectDir)
	if err != nil {
		return fmt.Errorf("loading .bpmignore: %w", err)
	}

	var platform_ string
	if kind == "lib" {
		platform_ = "lib"
		if _, _, err := archive.PackDirWithIgnore(projectDir, tarball, matcher); err != nil {
			return err
		}
	} else {
		if publishPlatform == "" {
			publishPlatform = platform.Current()
		}
		platform_ = publishPlatform

		binary, err := resolveNativeBinary(m, projectDir, publishPlatform)
		if err != nil {
			return err
		}
		if err := packNative(projectDir, m, binary, matcher, tarball); err != nil {
			return err
		}
	}

	meta := registry.PublishMetadata{
		Kind:         kind,
		Dependencies: m.Dependencies,
		Description:  m.Description,
		License:      m.License,
		Homepage:     m.Homepage,
		Repository:   m.Repository,
	}
	res, err := c.Publish(m.Name, m.Version, platform_, tarball, meta)
	if err != nil {
		return err
	}
	info("published %s@%s (%s) — %d bytes — %s",
		res.Name, res.Version, res.Platform, res.SizeBytes, res.Integrity)
	return nil
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
