package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"bpm/internal/manifest"
	"bpm/internal/paths"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed dependencies",
	RunE: func(cmd *cobra.Command, args []string) error {
		global, _ := cmd.Flags().GetBool("global")

		var depsDir string
		if global {
			depsDir = paths.GlobalDepsDir()
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			projectDir, err := paths.FindProjectRoot(cwd)
			if err != nil {
				return err
			}
			depsDir = filepath.Join(projectDir, "deps")
		}

		entries, err := os.ReadDir(depsDir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		names := []string{}
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			ver := ""
			kind := ""
			if m, err := manifest.Load(filepath.Join(depsDir, n)); err == nil {
				ver = m.Version
				kind = m.Kind()
			}
			fmt.Printf("%-32s  %-12s  %s\n", n, ver, kind)
		}
		return nil
	},
}

func init() {
	listCmd.Flags().BoolP("global", "g", false, "list ~/.bnl/deps")
	rootCmd.AddCommand(listCmd)
}
