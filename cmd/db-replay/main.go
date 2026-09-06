// Command db-replay reconstructs a watchable Replay from a live battle's
// persisted turns. Live model games are played over the gateway and can never be
// re-simulated from a seed, but every turn's engine state was stored, so a replay
// can be rebuilt from it exactly.
//
// It reads a JSON export of one battle — {seed, winner, turns:[{state, log}]} —
// on stdin or from -in (produced by a psql query against battle_turns), applies
// the trainer labels, and writes an eval.Replay JSON. This keeps the tool free of
// a database dependency: the dump step is a psql one-liner, the shape step is here.
//
// Usage:
//
//	docker exec pg psql ... -c "<json query>" | db-replay -side0 "Claude Sonnet 4.6" -side1 heuristic -team Genesis
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"

	"github.com/shaumik/PokeArena/internal/eval"
)

// export is the psql json_build_object shape for one battle.
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
	log.SetPrefix("[db-replay] ")

	in := flag.String("in", "-", "battle JSON export path (\"-\" for stdin)")
	out := flag.String("out", "-", "Replay JSON output path (\"-\" for stdout)")
	side0 := flag.String("side0", "agent", "name for side 0 (the model)")
	side1 := flag.String("side1", "heuristic", "name for side 1 (the reference bot)")
	team := flag.String("team", "", "team name, for the replay label")
	model := flag.String("model", "", "agentic config key (e.g. cc-haiku); when set, side 0's name is the model's display name, matching the report's board exactly")
	flag.Parse()

	// Deriving side 0's label from the shared ModelDisplay guarantees the replay's
	// trainer name equals the contestant name on the board, so it attaches to the
	// right matrix cell without hand-matching a string.
	if *model != "" {
		if name, _ := eval.ModelDisplay(*model); name != "" {
			*side0 = name
		}
	}

	raw, err := readAll(*in)
	if err != nil {
		log.Fatalf("read export: %v", err)
	}
	var ex export
	if err := json.Unmarshal(raw, &ex); err != nil {
		log.Fatalf("parse export: %v", err)
	}
	if len(ex.Turns) == 0 {
		log.Fatalf("battle has no stored turns")
	}

	turns := make([]eval.StoredTurn, len(ex.Turns))
	for i, t := range ex.Turns {
		turns[i] = eval.StoredTurn{State: t.State, Log: t.Log}
	}
	winner := *side0
	switch ex.Winner {
	case 1:
		winner = *side1
	case 2:
		winner = "draw"
	}

	rep, err := eval.ReplayFromStored(uint64(ex.Seed), *side0, *side1, winner, turns)
	if err != nil {
		log.Fatalf("reconstruct: %v", err)
	}
	rep.Title = *side0 + " vs " + *side1
	rep.Match = *side0 + "-vs-" + *side1
	rep.Team = *team

	data, err := json.Marshal(rep)
	if err != nil {
		log.Fatalf("marshal replay: %v", err)
	}
	if *out == "-" {
		os.Stdout.Write(data)
		os.Stdout.Write([]byte("\n"))
	} else if err := os.WriteFile(*out, data, 0o644); err != nil {
		log.Fatalf("write %s: %v", *out, err)
	}
	log.Printf("reconstructed %d-turn replay (%s vs %s, winner=%s)", rep.Turns, rep.Side0, rep.Side1, rep.Winner)
}

func readAll(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
