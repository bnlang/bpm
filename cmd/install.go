package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"bpm/internal/archive"
	"bpm/internal/lockfile"
	"bpm/internal/manifest"
	"bpm/internal/paths"
	"bpm/internal/platform"
	"bpm/internal/resolver"
)

var flagGlobal bool

var installCmd = &cobra.Command{
	Use:   "install [name[@version]]",
	Short: "Install dependencies",
	Long: `Install dependencies into ./deps (default) or ~/.bnl/deps (with -g).

With no args, installs everything declared in bnl.json's "dependencies".
With one arg, installs that single package and adds it to bnl.json.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInstall,
}

func init() {
	installCmd.Flags().BoolVarP(&flagGlobal, "global", "g", false, "install into ~/.bnl/deps")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	c, err := loadClient(false)
	if err != nil {
		return err
	}

	var (
		depsDir       string
		rootSpecs     map[string]string
		projectDir    string
		projManif     *manifest.Manifest
		addedName     string
		addedExplicit bool
	)

	if flagGlobal {
		depsDir = paths.GlobalDepsDir()
		if len(args) == 0 {
			return fmt.Errorf("global install requires a package name")
		}
		name, spec, _ := splitNameVersion(args[0])
		rootSpecs = map[string]string{name: spec}
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
		rootSpecs = map[string]string{}
		for n, s := range projManif.Dependencies {
			rootSpecs[n] = s
		}
		if len(args) == 1 {
			name, spec, explicit := splitNameVersion(args[0])
			rootSpecs[name] = spec
			projManif.AddDep(name, spec) // replaced below if !explicit
			addedName = name
			addedExplicit = explicit
		}
		if len(rootSpecs) == 0 {
			info("no dependencies in %s", manifest.Filename)
			return nil
		}
	}

	plan, err := resolver.Resolve(c, rootSpecs)
	if err != nil {
		return err
	}

	if err := paths.EnsureDir(depsDir); err != nil {
		return err
	}

	for _, r := range plan {
		info("→ %s@%s", r.Name, r.Version)
		tmp, err := os.CreateTemp("", "bpm-*.tar.gz")
		if err != nil {
			return err
		}
		tmp.Close()
		assetPlat := "lib"
		if r.Kind == "native" {
			assetPlat = platform.Current()
		}
		if _, err := c.DownloadAsset(r.Name, r.Version, assetPlat, tmp.Name()); err != nil {
			os.Remove(tmp.Name())
			return fmt.Errorf("downloading %s@%s: %w", r.Name, r.Version, err)
		}
		if err := verifyIntegrity(tmp.Name(), r.Integrity); err != nil {
			os.Remove(tmp.Name())
			return fmt.Errorf("integrity mismatch for %s@%s: %w", r.Name, r.Version, err)
		}
		dst := filepath.Join(depsDir, r.Name)
		_ = os.RemoveAll(dst)
		if err := archive.UnpackTo(tmp.Name(), dst); err != nil {
			os.Remove(tmp.Name())
			return err
		}
		os.Remove(tmp.Name())

		if r.Kind == "native" {
			if err := normalizeNativeManifest(dst, r.Name); err != nil {
				return err
			}
		}
	}

	if !flagGlobal {
		if addedName != "" && !addedExplicit {
			for _, r := range plan {
				if r.Name == addedName {
					projManif.AddDep(addedName, "^"+r.Version)
					break
				}
			}
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
		if err := l.Save(projectDir); err != nil {
			return err
		}
	}

	info("installed %d package(s) into %s", len(plan), depsDir)
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
		fname := platform.LibraryFilename(depName)
		return manifest.Save(depDir, &manifest.Manifest{
			Name:   depName,
			Native: fname,
		})
	}
	m.Native = platform.LibraryFilename(depName)
	return manifest.Save(depDir, m)
}
