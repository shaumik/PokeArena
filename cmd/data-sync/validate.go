package main

import (
	"fmt"
	"os"

	"github.com/shaumik/PokeArena/internal/domain"
)

// validate loads the staged dataset through the live domain validator. Any
// schema violation, unknown flag, missing move reference, or type-chart
// shape problem fails the sync.
func validate(stagingDir string) error {
	if _, err := domain.LoadDexFS(os.DirFS(stagingDir), "sync-staging"); err != nil {
		return fmt.Errorf("domain.LoadDexFS: %w", err)
	}
	return nil
}
