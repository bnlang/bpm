package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagRegistry string
	flagQuiet    bool
)

var rootCmd = &cobra.Command{
	Use:   "bpm",
	Short: "Package manager for the bnl language",
	Long: `bpm — package manager for bnl.

Manages dependencies under deps/<name>/ in a project, or globally under
~/.bnl/deps/<name>/. Pure-bnl libraries and native plugins both supported.`,
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagRegistry, "registry",
		envOrDefault("BPM_REGISTRY", "https://bpm.bnlang.dev"),
		"registry URL (env: BPM_REGISTRY)")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false,
		"suppress non-error output")
}

// SetVersion is called from main() to wire the ldflags-injected version
// into cobra so `bpm --version` and `bpm -v` print it.
func SetVersion(version, buildDate string) {
	rootCmd.Version = fmt.Sprintf("%s (built %s)", version, buildDate)
	rootCmd.SetVersionTemplate("bpm {{.Version}}\n")
}

func Execute() error {
	return rootCmd.Execute()
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
