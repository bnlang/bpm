package main

import (
	"fmt"
	"os"

	"bpm/cmd"
)

var (
	Version   = "1.2.0"
	BuildDate = "2026-05-20"
)

func main() {
	cmd.SetVersion(Version, BuildDate)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
