package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"bpm/internal/lockfile"
	"bpm/internal/manifest"
	"bpm/internal/paths"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Remove a dependency",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		global, _ := cmd.Flags().GetBool("global")

		var depsDir string
		var projectDir string
		var projManif *manifest.Manifest

		if global {
			depsDir = paths.GlobalDepsDir()
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
		}

		target := filepath.Join(depsDir, name)
		if err := os.RemoveAll(target); err != nil {
			return err
		}

		if !global && projManif != nil {
			if projManif.RemoveDep(name) {
				if err := manifest.Save(projectDir, projManif); err != nil {
					return err
				}
			}
			if l, err := lockfile.Load(projectDir); err == nil && l != nil {
				delete(l.Packages, name)
				_ = l.Save(projectDir)
			}
		}

		info("removed %s from %s", name, depsDir)
		_ = fmt.Sprintf("")
		return nil
	},
}

func init() {
	uninstallCmd.Flags().BoolP("global", "g", false, "uninstall from ~/.bnl/deps")
	rootCmd.AddCommand(uninstallCmd)
}
