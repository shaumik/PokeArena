// Command decision-sim generates decision-quality inputs offline.
//
// The decision-quality metric reads battles in the shape the live path
// persists, which used to mean the only way to produce them was to play a batch
// against real model endpoints and export it from Postgres. That made the
// metric expensive to re-measure, impossible to re-run after the attribution
// files were lost, and untestable in CI.
//
// This plays the same battles with deterministic policies and writes the same
// export shape, plus the manifest `decision-eval -manifest` already consumes.
// No gateway, no database, no API spend, and a fixed seed reproduces a run
// exactly — so the numbers below a published table can always be re-derived.
//
// Usage:
//
//	decision-sim -out /tmp/dq -games 4
//	decision-eval -manifest /tmp/dq/manifest.tsv -depth 3
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/eval"
)

// export mirrors cmd/decision-eval's input shape, which is itself the shape
// db-replay consumes. Kept as a local struct rather than shared so the two
// commands stay independently readable; the field tags are the contract.
type export struct {
	Seed   int64  `json:"seed"`
	Winner int    `json:"winner"`
	Turns  []turn `json:"turns"`
}

type turn struct {
	State json.RawMessage `json:"state"`
	Log   json.RawMessage `json:"log"`
}

// randomPolicySeed fixes the random policy's RNG so a run is reproducible.
const randomPolicySeed = 20260811

// policy is one labelled contestant for the scored seat. The label becomes the
// "model" column in the aggregated table.
type policy struct {
	label string
	build func(*domain.Dex) ai.Agent
}

func policies(dex *domain.Dex, spec string) ([]policy, error) {
	all := map[string]policy{
		"heuristic":     {"heuristic", func(d *domain.Dex) ai.Agent { return ai.NewHeuristicAgent(d) }},
		"expectimax-d1": {"expectimax-d1", func(d *domain.Dex) ai.Agent { return ai.NewExpectimaxAgentFixed(d, 1) }},
		"expectimax-d2": {"expectimax-d2", func(d *domain.Dex) ai.Agent { return ai.NewExpectimaxAgentFixed(d, 2) }},
		"expectimax-d3": {"expectimax-d3", func(d *domain.Dex) ai.Agent { return ai.NewExpectimaxAgentFixed(d, 3) }},
		// A fixed RNG seed, rebuilt per game, keeps even the random floor
		// reproducible — the battle seed is what makes its games differ.
		"random": {"random", func(*domain.Dex) ai.Agent { return ai.NewRandomAgent(randomPolicySeed) }},
	}
	var out []policy
	for _, name := range strings.Split(spec, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		p, ok := all[name]
		if !ok {
			return nil, fmt.Errorf("unknown policy %q", name)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no policies selected")
	}
	return out, nil
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("[decision-sim] ")

	dataDir := flag.String("data", "data", "dex data directory")
	teamsPath := flag.String("teams", "data/benchmark-teams.json", "team library to play")
	outDir := flag.String("out", "", "directory to write exports and manifest.tsv (required)")
	games := flag.Int("games", 2, "games per policy per team")
	oppName := flag.String("opponent", "heuristic", "policy occupying the unscored seat (side 1)")
	policySpec := flag.String("policies", "heuristic,expectimax-d1,expectimax-d2", "comma-separated policies for the scored seat (side 0)")
	flag.Parse()

	if *outDir == "" {
		log.Fatal("-out is required")
	}

	dex, err := domain.LoadDex(*dataDir, "bench")
	if err != nil {
		log.Fatalf("load dex: %v", err)
	}
	lib, err := eval.LoadTeamLibrary(*teamsPath, dex)
	if err != nil {
		log.Fatalf("load team library: %v", err)
	}
	ps, err := policies(dex, *policySpec)
	if err != nil {
		log.Fatalf("policies: %v", err)
	}
	opps, err := policies(dex, *oppName)
	if err != nil {
		log.Fatalf("opponent: %v", err)
	}
	opp := opps[0]

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", *outDir, err)
	}

	// Every policy plays the same team on the same seed, so the table's rows
	// differ only by the policy in the scored seat — the mirror-match discipline
	// the rest of the benchmark uses, applied to decisions instead of wins.
	seeds := eval.SeedRange(*games)

	var manifest strings.Builder
	fmt.Fprintf(&manifest, "# decision-sim: library %s, opponent %s, %d games/team\n",
		lib.Version, opp.label, *games)

	total := 0
	for _, p := range ps {
		for _, team := range lib.Teams {
			for _, seed := range seeds {
				agents := [2]ai.Agent{p.build(dex), opp.build(dex)}
				picks := [2][]engine.TeamPick{team.Picks, team.Picks}
				res, turns, err := eval.CaptureStored(dex, agents, picks, seed, 0)
				if err != nil {
					log.Fatalf("%s / %s / seed %d: %v", p.label, team.Name, seed, err)
				}
				name := fmt.Sprintf("%s-%s-%d.json", p.label, sanitize(team.Name), seed)
				path := filepath.Join(*outDir, name)
				if err := writeExport(path, res.Winner, seed, turns); err != nil {
					log.Fatalf("write %s: %v", path, err)
				}
				fmt.Fprintf(&manifest, "%s\t%s\n", p.label, path)
				total++
			}
		}
		log.Printf("%s: %d games", p.label, len(lib.Teams)*len(seeds))
	}

	manifestPath := filepath.Join(*outDir, "manifest.tsv")
	if err := os.WriteFile(manifestPath, []byte(manifest.String()), 0o644); err != nil {
		log.Fatalf("write manifest: %v", err)
	}
	log.Printf("wrote %d games + %s (library %s)", total, manifestPath, lib.Version)
}

func writeExport(path string, winner int, seed uint64, turns []eval.StoredTurn) error {
	ex := export{Seed: int64(seed), Winner: winner, Turns: make([]turn, len(turns))}
	for i, t := range turns {
		ex.Turns[i] = turn{State: t.State, Log: t.Log}
	}
	b, err := json.Marshal(ex)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func sanitize(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
}
