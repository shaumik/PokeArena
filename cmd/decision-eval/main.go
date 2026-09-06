// Command decision-eval scores how well a side chose in a stored live battle,
// not just whether it won. It reads a JSON export of one battle —
// {seed, winner, turns:[{state, log}]} — the same shape db-replay consumes,
// re-simulates each turn to recover the action played, and compares it to a
// depth-limited expectimax oracle deciding from the identical fog-of-war view.
//
// This is a spike: it prints per-battle coverage and oracle-agreement so the
// approach can be validated on real data before it grows a report section.
//
// Usage:
//
//	docker exec pg psql ... -c "<json query>" | decision-eval -side 0 -depth 3
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/eval"
)

type export struct {
	Seed   int64 `json:"seed"`
	Winner int   `json:"winner"`
	Turns  []struct {
		State json.RawMessage `json:"state"`
		Log   json.RawMessage `json:"log"`
	} `json:"turns"`
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("[decision-eval] ")

	in := flag.String("in", "-", "battle JSON export path (\"-\" for stdin)")
	dataDir := flag.String("data", "data", "dex data directory")
	side := flag.Int("side", 0, "which side to score (0 = the model seat)")
	depth := flag.Int("depth", 3, "oracle expectimax search depth")
	quiet := flag.Bool("quiet", false, "print only the summary line")
	manifest := flag.String("manifest", "", "aggregate mode: a file of \"model<TAB>export.json\" lines → per-model table")
	regretCap := flag.Float64("regret-cap", 3000, "winsorize regret at this cap for the mean (median is unaffected)")
	asJSON := flag.Bool("json", false, "aggregate mode: emit the per-model table as JSON")
	oracleName := flag.String("oracle", "expectimax", "reference policy to score against: expectimax|heuristic")
	flag.Parse()

	if *manifest != "" {
		runAggregate(*manifest, *dataDir, *oracleName, *side, *depth, *regretCap, *asJSON)
		return
	}

	raw, err := readAll(*in)
	if err != nil {
		log.Fatalf("read export: %v", err)
	}
	var ex export
	if err := json.Unmarshal(raw, &ex); err != nil {
		log.Fatalf("parse export: %v", err)
	}

	dex, err := domain.LoadDex(*dataDir, "bench")
	if err != nil {
		log.Fatalf("load dex: %v", err)
	}
	oracle, err := buildOracle(dex, *oracleName, *depth)
	if err != nil {
		log.Fatalf("oracle: %v", err)
	}

	turns := make([]eval.StoredTurn, len(ex.Turns))
	for i, t := range ex.Turns {
		turns[i] = eval.StoredTurn{State: t.State, Log: t.Log}
	}

	scores, skipped, err := eval.ScoreDecisions(dex, oracle, *side, turns)
	if err != nil {
		log.Fatalf("score: %v", err)
	}

	agree, blunders := 0, 0
	var sumRegret, maxRegret float64
	for _, s := range scores {
		if s.Agree {
			agree++
		}
		if s.Blunder {
			blunders++
		}
		sumRegret += s.Regret
		if s.Regret > maxRegret {
			maxRegret = s.Regret
		}
	}
	agreePct, blunderPct, meanRegret := 0.0, 0.0, 0.0
	if n := float64(len(scores)); n > 0 {
		agreePct = 100 * float64(agree) / n
		blunderPct = 100 * float64(blunders) / n
		meanRegret = sumRegret / n
	}
	fmt.Printf("seed=%d winner=%d turns=%d recovered=%d skipped=%d agree=%.0f%% blunder=%.0f%% meanRegret=%.0f maxRegret=%.0f depth=%d\n",
		ex.Seed, ex.Winner, len(ex.Turns), len(scores), skipped, agreePct, blunderPct, meanRegret, maxRegret, *depth)
	if *quiet {
		return
	}
	for _, s := range scores {
		mark := "  ok "
		if s.Blunder {
			mark = "BLUND"
		} else if !s.Agree {
			mark = "diff "
		}
		fmt.Printf("turn %2d  %s  chose %s  oracle %s  regret %.0f\n", s.Turn, mark, actStr(s.Chosen), actStr(s.Best), s.Regret)
	}
}

func actStr(a engine.Action) string { return fmt.Sprintf("%s#%d", a.Kind, a.Index) }

// buildOracle picks the reference policy a decision is scored against.
//
// Two families are available on purpose. Expectimax is the stronger player and
// the default, but scoring only against it conflates playing well with
// searching the way it searches — the bias documented in
// docs/decision-quality.md, where expectimax d2 earns the best blunder rate
// while winning the fewest games. The heuristic is depth-0 with no opponent
// model, so running a batch through both and comparing the *orderings* is what
// turns that bias from an acknowledged caveat into a measured quantity.
//
// The two score in unrelated units. Never compare a regret or blunder rate
// across oracles as a magnitude; compare rank order and match rate.
func buildOracle(dex *domain.Dex, name string, depth int) (eval.Oracle, error) {
	switch name {
	case "expectimax":
		return ai.NewExpectimaxAgentFixed(dex, depth), nil
	case "heuristic":
		return ai.NewHeuristicAgent(dex), nil
	default:
		return nil, fmt.Errorf("unknown oracle %q (want expectimax or heuristic)", name)
	}
}

// runAggregate scores every battle listed in the manifest and prints the
// per-model decision-quality table. The manifest is one "model<TAB>export.json"
// line per battle (blank lines and "#" comments ignored) — the driver script
// builds it by mapping each attributed bid= to its model and exporting the
// battle from Postgres. Loading the dex and building the oracle once keeps a
// whole run's scoring on a single shared reference.
func runAggregate(manifestPath, dataDir, oracleName string, side, depth int, regretCap float64, asJSON bool) {
	dex, err := domain.LoadDex(dataDir, "bench")
	if err != nil {
		log.Fatalf("load dex: %v", err)
	}
	oracle, err := buildOracle(dex, oracleName, depth)
	if err != nil {
		log.Fatalf("oracle: %v", err)
	}

	mf, err := os.ReadFile(manifestPath)
	if err != nil {
		log.Fatalf("read manifest: %v", err)
	}

	var results []eval.BattleResult
	for lineNo, line := range strings.Split(string(mf), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			log.Fatalf("manifest line %d: want \"model<TAB>path\", got %q", lineNo+1, line)
		}
		model, path := parts[0], parts[1]
		raw, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("manifest line %d: read %s: %v", lineNo+1, path, err)
		}
		var ex export
		if err := json.Unmarshal(raw, &ex); err != nil {
			log.Fatalf("manifest line %d: parse %s: %v", lineNo+1, path, err)
		}
		turns := make([]eval.StoredTurn, len(ex.Turns))
		for i, t := range ex.Turns {
			turns[i] = eval.StoredTurn{State: t.State, Log: t.Log}
		}
		scores, _, err := eval.ScoreDecisions(dex, oracle, side, turns)
		if err != nil {
			log.Fatalf("manifest line %d: score %s: %v", lineNo+1, path, err)
		}
		results = append(results, eval.BattleResult{Model: model, Won: ex.Winner == side, Scores: scores})
	}

	stats := eval.AggregateByModel(results, regretCap)
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(stats); err != nil {
			log.Fatalf("encode: %v", err)
		}
		return
	}
	fmt.Printf("%-20s %6s %6s %8s %9s %9s %11s %10s\n",
		"model", "games", "win%", "decs", "blunder%", "match%", "medRegret", "meanReg")
	for _, s := range stats {
		fmt.Printf("%-20s %6d %5.0f%% %8d %8.0f%% %8.0f%% %11.0f %10.0f\n",
			s.Model, s.Games, 100*s.WinRate, s.Decisions,
			100*s.BlunderRate, 100*s.MatchRate, s.MedianRegret, s.MeanRegret)
	}
}

func readAll(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
