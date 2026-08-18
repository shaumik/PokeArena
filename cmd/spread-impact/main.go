// Command spread-impact measures how much the v2 training spreads change the
// games they are played in, by replaying every benchmark team in a heuristic
// mirror twice: once as shipped, once with EVs, IVs and Nature stripped back to
// the engine defaults.
//
// It exists because docs/benchmark.md publishes that table and the measurement
// behind it was previously ad hoc — which meant the numbers could not be
// re-derived after an engine change that moved them, and the OPEN-3 damage
// rounding rewrite is exactly such a change. Section 8 of that document stakes
// the benchmark's credibility on a third party re-deriving its numbers; a
// published table with no committed way to reproduce it does not meet that bar.
//
// Stripping rather than replaying the literal v1 library is deliberate and
// inherited from the original measurement: it holds movesets constant, so the
// comparison isolates the spread rather than folding in curation changes.
//
//	go run ./cmd/spread-impact
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/eval"
)

func main() {
	dataDir := flag.String("data", "data", "dataset directory")
	libPath := flag.String("teams", "data/benchmark-teams.json", "team library to measure")
	seeds := flag.Int("seeds", 60, "seeds per team per condition")
	flag.Parse()

	dex, err := domain.LoadDex(*dataDir, "spread-impact")
	if err != nil {
		log.Fatalf("load dex: %v", err)
	}
	lib, err := eval.LoadTeamLibrary(*libPath, dex)
	if err != nil {
		log.Fatalf("load team library: %v", err)
	}

	fmt.Printf("library %s — %d seeds per team per condition, heuristic mirror\n\n", lib.Version, *seeds)
	fmt.Println("| Team | spreads stripped | as shipped | games with a different outcome or length |")
	fmt.Println("|---|---:|---:|---:|")

	worst := 0
	for _, team := range lib.Teams {
		shipped := team.Picks
		stripped := stripSpreads(shipped)

		var sumShipped, sumStripped, differ int
		for seed := 1; seed <= *seeds; seed++ {
			a := mirror(dex, stripped, uint64(seed))
			b := mirror(dex, shipped, uint64(seed))
			sumStripped += a.Turns
			sumShipped += b.Turns
			if a.Turns != b.Turns || a.Winner != b.Winner {
				differ++
			}
			if b.Turns > worst {
				worst = b.Turns
			}
		}
		fmt.Printf("| %s | %.1f | %.1f | %d / %d |\n",
			team.Name,
			float64(sumStripped)/float64(*seeds),
			float64(sumShipped)/float64(*seeds),
			differ, *seeds)
	}
	fmt.Fprintf(os.Stderr, "\nlongest game as shipped: %d turns\n", worst)
}

// stripSpreads returns the picks with EVs, IVs and Nature cleared, so the
// engine falls back to its defaults (EV 0 / IV 31 / neutral) — the v1 baseline.
// Clone first: the pointers are shared with the loaded library, and mutating
// them in place would corrupt the "as shipped" arm of the very comparison.
func stripSpreads(picks []engine.TeamPick) []engine.TeamPick {
	out := make([]engine.TeamPick, len(picks))
	for i, p := range picks {
		c := p.Clone()
		c.EVs, c.IVs, c.Nature = nil, nil, ""
		out[i] = c
	}
	return out
}

// mirror plays one team against itself with a heuristic on both sides. Fresh
// agents per game: the heuristic carries no state between games today, but the
// benchmark's per-game independence is a property worth not relying on luck for.
func mirror(dex *domain.Dex, picks []engine.TeamPick, seed uint64) eval.GameResult {
	agents := [2]ai.Agent{ai.NewHeuristicAgent(dex), ai.NewHeuristicAgent(dex)}
	res, err := eval.RunGame(dex, agents, [2][]engine.TeamPick{picks, picks}, seed, 0)
	if err != nil {
		log.Fatalf("seed %d: %v", seed, err)
	}
	return res
}
