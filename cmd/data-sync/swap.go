package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// stagedFiles is the explicit list of files the sync produces. The swap step
// renames exactly these — anything else in data/.staging/ is left for the
// next sync run to clean up.
var stagedFiles = []string{
	"pokedex.json",
	"moves.json",
	"typechart.json",
	"_provenance.json",
}

// swap promotes the staged files into dataDir via os.Rename, which is atomic
// at the filesystem level on the same device. We do the renames sequentially
// rather than in parallel — Postgres-style atomicity is impossible across
// multiple files, but each individual file flip is atomic, and the only
// failure mode is a partial promotion which is detectable on next sync.
func swap(stagingDir, dataDir string) error {
	for _, name := range stagedFiles {
		src := filepath.Join(stagingDir, name)
		dst := filepath.Join(dataDir, name)
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("staged file %s missing: %w", src, err)
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("rename %s -> %s: %w", src, dst, err)
		}
	}
	return nil
}
