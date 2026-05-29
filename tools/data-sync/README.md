# data-sync

The ETL pipeline that produces `data/pokedex.json`, `data/moves.json`, and
`data/typechart.json` from Pokémon Showdown's canonical data.

This is the **deliberate, staged, validated refresh** the project README has
always promised. Go owns the ETL; Node is isolated to a rarely-run helper
that dumps Showdown's data to a committed snapshot.

## Layout

```
tools/data-sync/
  refresh-upstream/     # Node helper (rare): pull Showdown → snapshot
    refresh.js
    package.json
    package-lock.json   # committed for reproducibility
  upstream/             # committed JSON snapshot of Showdown's data
    species.json
    moves.json
    typechart.json
    learnsets.json
    _meta.json          # @pkmn/sim version + refresh time
```

The Go orchestrator lives in `cmd/data-sync/`. It reads `upstream/`, applies
filters, transforms to our schema, stages, validates via `domain.LoadDexFS`,
and atomically swaps `data/.staging/*` over `data/*.json`.

## Operations

| Command | What it does | When to run |
|---|---|---|
| `make sync-upstream` | Node: refresh `tools/data-sync/upstream/` from Showdown | Rarely — when Smogon ships a new generation, or you want newer canonical data |
| `make sync` | Go: extract → filter → transform → stage → validate → swap | After editing the filter chain, or after a sync-upstream |
| `make sync-diff` | Same as `sync` but prints diff vs current `data/` and does NOT swap | Before committing — eyeball the changes |

## Pipeline stages (in order)

1. **Extract** — `cmd/data-sync` reads `tools/data-sync/upstream/*.json`.
2. **Filter** — A composable chain of `SpeciesFilter` predicates in
   `cmd/data-sync/filter.go`. Adding a filter = new file + line in the chain.
   Removing = delete the line. One line per filter is logged with in/out counts.
3. **Transform** — Showdown shape → our schema (see `docs/battle-state.md`).
   Movesets come from `upstream/learnsets.json` (each species's full Gen-1
   learnset, ordered lowest-level-up-first). Type names lowercased.
   Showdown's `accuracy: true` → our `bypass-acc` flag. Top-level `boosts` /
   `status` on status moves → our `primary` block. `secondary`/`secondaries`
   on damage moves → our `secondaries` array. Etc.
4. **Stage** — Writes `data/.staging/{pokedex,moves,typechart}.json` plus a
   sidecar `data/.staging/_provenance.json`.
5. **Validate** — Calls `domain.LoadDexFS` over the staging dir. Schema
   violation, unknown flag, broken move reference → fail.
6. **Swap** — Atomic rename: `data/.staging/*.json` → `data/*.json`.

If any stage fails, `data/` is untouched and `data/.staging/` is left in
place for inspection.

## Filter chain (today)

In `cmd/data-sync/filter.go`:

```go
var defaultFilters = []SpeciesFilter{
    GenAtMost(1),       // Pokédex #1–151 only
    NotPreEvolution(),  // drop species that have a further evolution
                        // (yes, this excludes Pikachu — it evolves to Raichu)
}
```

To narrow scope further (say, exclude legendaries), add a filter file and
one line above.

## Reproducibility

- `package-lock.json` is committed — `npm install` produces the same tree
  on every machine forever.
- `upstream/` is committed — re-running `make sync` without `make
  sync-upstream` is fully deterministic.
- `data/_provenance.json` records the `@pkmn/sim` version, the sync
  timestamp, and the curation git SHA at sync time.

## Why this shape

See `docs/battle-state.md` for the schema, and the design diary entry from
the round this was built for the *why*. Short version: ETL pipelines need
stages with clear contracts. Splitting Node (upstream snapshot) from Go
(transform pipeline) keeps the toolchains decoupled. Splitting the snapshot
into a committed file from the live ETL keeps reproducibility honest.
