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
	flag.Parse()

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
	oracle := ai.NewExpectimaxAgentFixed(dex, *depth)

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

func readAll(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
