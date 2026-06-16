// Command data-validate loads a dataset directory through domain.LoadDexFS
// and exits 0 on success, non-zero on any schema or referential-integrity
// violation. It is the same validation gate the sync uses, exposed as its
// own binary so CI can guard data/*.json directly without invoking the full
// sync pipeline.
//
// Usage:
//
//	data-validate [dir]
//
// dir defaults to "data". Returns a one-line summary on success.
package main

import (
	"fmt"
	"log"
	"os"

	"pokearena/internal/domain"
	// Blank import: engine's init() populates internal/specs with the
	// vocabularies the domain validator checks against. Skipping it
	// would make every move's volatile / side-condition slug look
	// unknown.
	_ "pokearena/internal/engine"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[data-validate] ")

	dir := "data"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	dex, err := domain.LoadDex(dir, "validate")
	if err != nil {
		log.Fatalf("invalid dataset at %s: %v", dir, err)
	}
	fmt.Printf("ok: %d species, %d moves, %d items, type chart loaded from %s\n",
		len(dex.Species), len(dex.Moves), len(dex.Items), dir)
}
