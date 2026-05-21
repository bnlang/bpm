package main

import (
	"fmt"
	"os"

	"bpm/cmd"
)

var (
	Version   = "1.3.0"
	BuildDate = "2026-05-21"
)

func main() {
	cmd.SetVersion(Version, BuildDate)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
