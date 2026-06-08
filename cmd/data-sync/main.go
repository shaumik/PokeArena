// Command data-sync is the Go ETL orchestrator: it reads the upstream
// Showdown snapshot from tools/data-sync/upstream/, runs the species through
// the filter chain, transforms to our schema, stages the result under
// data/.staging/, validates it via domain.LoadDexFS, and atomically swaps the
// staged files over data/*.json.
//
// Flags:
//
//	-upstream  directory holding the upstream snapshot
//	           (default: tools/data-sync/upstream)
//	-data      destination directory holding the live data files
//	           (default: data)
//	-no-swap   stop after staging+validation; print a summary and leave
//	           data/.staging/ in place for inspection (sync-diff mode)
//
// On any failure between stages, data/ is untouched and the staging dir is
// left for inspection. The atomic swap step uses os.Rename, so a half-done
// run can never produce a partially-updated data/ tree.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	// Blank import: pull engine's package init so internal/specs sees
	// every supported volatile / side-condition / weather / terrain slug
	// before transform.go filters upstream against them.
	_ "pokearena/internal/engine"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[data-sync] ")

	upstreamDir := flag.String("upstream", "tools/data-sync/upstream", "path to the upstream snapshot directory")
	dataDir := flag.String("data", "data", "path to the live data directory (sync target)")
	noSwap := flag.Bool("no-swap", false, "stage and validate but do not swap into data/ (sync-diff mode)")
	flag.Parse()

	if err := run(*upstreamDir, *dataDir, *noSwap); err != nil {
		log.Fatalf("sync failed: %v", err)
	}
}

func run(upstreamDir, dataDir string, noSwap bool) error {
	up, err := loadUpstream(upstreamDir)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	log.Printf("extracted %d species, %d moves, %d types from snapshot (gen %d, @pkmn/sim %s)",
		len(up.Species), len(up.Moves), len(up.Typechart), up.Meta.Gen, up.Meta.SimVersion)

	species := applyFilters(up.Species, defaultFilters)
	log.Printf("filter chain kept %d / %d species", len(species), len(up.Species))

	transformed, err := transform(up, species)
	if err != nil {
		return fmt.Errorf("transform: %w", err)
	}

	stagingDir, err := stage(dataDir, transformed, up.Meta)
	if err != nil {
		return fmt.Errorf("stage: %w", err)
	}
	log.Printf("staged to %s", stagingDir)

	if err := validate(stagingDir); err != nil {
		return fmt.Errorf("validate (staging left at %s): %w", stagingDir, err)
	}
	log.Printf("validation passed")

	if noSwap {
		log.Printf("--no-swap set; leaving staging at %s", stagingDir)
		return nil
	}
	if err := swap(stagingDir, dataDir); err != nil {
		return fmt.Errorf("swap: %w", err)
	}
	log.Printf("swap complete; live data refreshed")

	// Best-effort cleanup of the now-empty staging dir.
	_ = os.Remove(stagingDir)
	return nil
}
