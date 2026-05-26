package main

import "embed"

// dataFS bundles the curated Pokémon dataset into the binary so a user
// can run pokearena-agent from anywhere without cloning the repo.
//
// The JSON files here are copies of the canonical data/ at the repo
// root. Drift is low-risk (the dataset changes a few times per decade,
// per README §Data ingestion) but possible — sync this directory after
// any change to the top-level data/ files. A Makefile target
// (`make agent-data`) automates the copy.
//
//go:embed data/pokedex.json data/moves.json data/typechart.json
var dataFS embed.FS
