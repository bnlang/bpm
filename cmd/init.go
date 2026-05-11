package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"bpm/internal/manifest"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter bnl.json in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		path := filepath.Join(cwd, manifest.Filename)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists", manifest.Filename)
		}
		m := &manifest.Manifest{
			Name:         filepath.Base(cwd),
			Version:      "0.1.0",
			Description:  "",
			Main:         "index.bnl",
			Dependencies: map[string]string{},
		}
		if err := manifest.Save(cwd, m); err != nil {
			return err
		}
		info("wrote %s", manifest.Filename)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
