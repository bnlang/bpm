package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"bpm/internal/archive"
	"bpm/internal/manifest"
	"bpm/internal/paths"
	"bpm/internal/platform"
)

var (
	packPlatform string
	packBinary   string
	packOutDir   string
	packAll      bool
)

var packCmd = &cobra.Command{
	Use:   "pack",
	Short: "Create a local tarball of the current package (does not upload)",
	Long: `Build the same tarball that 'bpm publish' would upload, but write it to
a local file instead of sending it to the registry.

Output filename:
  lib    → <name>-<version>.tar.gz
  native → <name>-<version>-<platform>.tar.gz

For native packages, 'bpm pack' defaults to the current host platform. Use
--platform <name> to pick a different one, or --all to write one tarball per
platform listed in the manifest's "targets" map. --binary overrides the path
read from targets[<platform>].

The resulting tarball can be installed with:

  bpm install ./<name>-<version>.tar.gz`,
	RunE: runPack,
}

func init() {
	packCmd.Flags().StringVar(&packPlatform, "platform", "",
		"native: asset platform to pack (default: current host)")
	packCmd.Flags().StringVar(&packBinary, "binary", "",
		"native: override the binary path from targets.<platform>")
	packCmd.Flags().StringVarP(&packOutDir, "out", "o", "",
		"directory to write the tarball into (default: current directory)")
	packCmd.Flags().BoolVar(&packAll, "all", false,
		"native: pack every platform in `targets`")
	rootCmd.AddCommand(packCmd)
}

func runPack(cmd *cobra.Command, args []string) error {
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
		return fmt.Errorf("bnl.json must declare 'name' and 'version' to pack")
	}

	outDir := packOutDir
	if outDir == "" {
		outDir = cwd
	} else if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(cwd, outDir)
	}
	if err := paths.EnsureDir(outDir); err != nil {
		return err
	}

	matcher, err := archive.LoadBpmIgnore(projectDir)
	if err != nil {
		return fmt.Errorf("loading .bpmignore: %w", err)
	}

	if m.Kind() == "lib" {
		if packAll || packPlatform != "" || packBinary != "" {
			return fmt.Errorf("--platform/--binary/--all only apply to native packages")
		}
		dst := filepath.Join(outDir, fmt.Sprintf("%s-%s.tar.gz", m.Name, m.Version))
		integ, size, err := archive.PackDirWithIgnore(projectDir, dst, matcher)
		if err != nil {
			return err
		}
		info("pack  %s  (%d bytes)  %s", dst, size, integ)
		return nil
	}

	var platforms []string
	switch {
	case packAll:
		if len(m.Targets) == 0 {
			return fmt.Errorf("native pack --all: manifest has no `targets` map")
		}
		platforms = make([]string, 0, len(m.Targets))
		for p := range m.Targets {
			platforms = append(platforms, p)
		}
		sort.Strings(platforms)
	case packPlatform != "":
		platforms = []string{packPlatform}
	default:
		platforms = []string{platform.Current()}
	}

	for _, plat := range platforms {
		bin, err := resolvePackBinary(m, projectDir, plat)
		if err != nil {
			return err
		}
		if _, err := os.Stat(bin); err != nil {
			return fmt.Errorf("missing binary for %s: %s", plat, bin)
		}
		dst := filepath.Join(outDir, fmt.Sprintf("%s-%s-%s.tar.gz", m.Name, m.Version, plat))
		if err := packNative(projectDir, m, plat, bin, matcher, dst); err != nil {
			return err
		}
		st, _ := os.Stat(dst)
		size := int64(0)
		if st != nil {
			size = st.Size()
		}
		info("pack  %s  (%d bytes)", dst, size)
	}
	return nil
}

func resolvePackBinary(m *manifest.Manifest, projectDir, plat string) (string, error) {
	if packBinary != "" {
		return packBinary, nil
	}
	if len(m.Targets) == 0 {
		return "", fmt.Errorf(
			"native pack: manifest has no `targets` map. "+
				"Add a `targets[%q]` entry to bnl.json, or pass --binary.", plat)
	}
	rel, ok := m.Targets[plat]
	if !ok {
		return "", fmt.Errorf("native pack: targets has no entry for platform %q", plat)
	}
	return filepath.Join(projectDir, filepath.FromSlash(rel)), nil
}
