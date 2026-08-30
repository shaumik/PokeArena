// Package pokearena lives only to embed the curated dataset so a single
// physical copy of data/*.json can be reached by go:embed. go:embed cannot
// escape its own package directory, so before this file existed the agent
// binary kept a hand-synced duplicate under cmd/pokearena-agent/data/. By
// putting the embed at the module root — the one place that already
// contains data/ — every binary that wants the embedded dataset can import
// it from here.
//
// Services that read data/ from disk (battle-worker, ai-service, gateway,
// data-sync, data-validate) don't use this package; they keep their
// existing DATA_DIR-based loading. Only binaries that need a
// no-clone-required standalone build (pokearena-agent today) embed.
package pokearena

import (
	"embed"
	"io/fs"
)

//go:embed data/pokedex.json data/moves.json data/typechart.json data/items.json data/natures.json
//go:embed data/benchmark-teams.json data/_provenance.json data/model-pricing.json
//go:embed data/ai-teams.json
var dataFS embed.FS

// DataFS returns the embedded data directory rooted at "data/" — i.e. the
// caller sees pokedex.json / moves.json / typechart.json / items.json /
// natures.json at the top level, which is the shape domain.LoadDexFS expects.
//
// benchmark-teams.json and _provenance.json ride along so that cmd/bench is
// self-contained too: the benchmark is the project's zero-setup entry point,
// and "go run github.com/shaumik/PokeArena/cmd/bench@latest" runs from a
// module cache directory with no data/ anywhere near it.
//
// ai-teams.json rides along for pokearena-mcp's offline mode, where the
// built-in opponent needs a roster and there is no gateway to ask for one.
func DataFS() fs.FS {
	sub, err := fs.Sub(dataFS, "data")
	if err != nil {
		// fs.Sub only fails on a malformed name; "data" is a static
		// string that we control, so any error here is a compile-time
		// bug in this package, not a runtime condition.
		panic(err)
	}
	return sub
}
