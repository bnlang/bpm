package cmd

import (
	"fmt"
	"os"

	"bpm/internal/auth"
	"bpm/internal/registry"
)

func loadClient(requireAuth bool) (*registry.Client, error) {
	store, err := auth.Load()
	if err != nil {
		return nil, fmt.Errorf("loading auth: %w", err)
	}
	tok := store.Get(flagRegistry)
	if requireAuth && tok == "" {
		return nil, fmt.Errorf("not logged in to %s — run `bpm login` first", flagRegistry)
	}
	return registry.New(flagRegistry, tok), nil
}

func info(format string, args ...interface{}) {
	if flagQuiet {
		return
	}
	fmt.Fprintf(os.Stdout, format, args...)
	if len(format) == 0 || format[len(format)-1] != '\n' {
		fmt.Fprintln(os.Stdout)
	}
}
