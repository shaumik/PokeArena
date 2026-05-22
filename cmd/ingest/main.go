// Command ingest is the one-shot dataset loader: it applies the schema and
// mirrors the curated JSON dataset into PostgreSQL. It is idempotent and
// re-runnable — upserts converge, so running it twice changes nothing.
package main

import (
	"context"
	"log"
	"os"

	"pokearena/internal/config"
	"pokearena/internal/domain"
	"pokearena/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[ingest] ")

	cfg := config.Load()
	ctx := context.Background()

	dex, err := domain.LoadDex(envOr("DATA_DIR", "data"), cfg.DataVersion)
	if err != nil {
		log.Fatalf("load dataset: %v", err)
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("apply schema: %v", err)
	}

	// Moves first — species_moves rows reference them.
	for _, m := range dex.AllMoves() {
		if err := st.UpsertMove(ctx, m); err != nil {
			log.Fatalf("upsert move %s: %v", m.ID, err)
		}
	}
	for _, sp := range dex.AllSpecies() {
		if err := st.UpsertSpecies(ctx, sp, cfg.DataVersion); err != nil {
			log.Fatalf("upsert species %s: %v", sp.Name, err)
		}
	}

	log.Printf("ingested %d moves and %d species (data_version=%s)",
		len(dex.Moves), len(dex.Species), cfg.DataVersion)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
