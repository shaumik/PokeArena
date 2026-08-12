package eval

import (
	"encoding/json"
	"fmt"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// Decision-quality scoring reads battles in the shape the live path persists:
// one marshaled engine.BattleState per turn, replacements folded in. That shape
// used to be obtainable only from Postgres, after a live batch had been played
// against real model endpoints — which made the metric expensive to re-measure
// and impossible to regression-test.
//
// CaptureStored produces the same shape from an offline game. Any ai.Agent can
// take the scored seat, so the pipeline can be exercised end to end with
// deterministic policies: no gateway, no database, no API spend, and a fixed
// seed gives the same battle every run.

// CaptureStored plays one game and records the battle state after every settled
// turn, in the layout ScoreDecisions expects: index 0 is the opening position
// (the state turn 1 is chosen from), and index i is the state after turn i.
//
// "Settled" is the load-bearing word. A KO leaves the engine in PhaseReplace
// mid-turn, and the live coordinator resolves that before it persists — so a
// stored state is only ever choosing or ended, and the pre-decision state for
// turn N is the stored state at N-1. Capturing the intermediate replace states
// would break that indexing and silently shift every decision by one.
func CaptureStored(dex *domain.Dex, agents [2]ai.Agent, teams [2][]engine.TeamPick, seed uint64, budget Budget) (GameResult, []StoredTurn, error) {
	id := fmt.Sprintf("eval-%d", seed)
	s, err := engine.NewBattleFromPicks(dex, id, "P0", teams[0], "P1", teams[1], seed)
	if err != nil {
		return GameResult{}, nil, fmt.Errorf("new battle: %w", err)
	}

	res := GameResult{Seed: seed}
	opening, err := storedTurn(s, nil)
	if err != nil {
		return res, nil, err
	}
	turns := []StoredTurn{opening}

	for !s.Ended() {
		if len(res.Decisions) > maxDecisions {
			return res, turns, fmt.Errorf("battle %s did not terminate within %d decisions", id, maxDecisions)
		}
		if s.Phase != engine.PhaseChoosing {
			// Replacements are consumed inside the turn below, so the loop head
			// should only ever see a choosing state or a finished battle.
			return res, turns, fmt.Errorf("battle %s in unexpected phase %q", id, s.Phase)
		}

		var acts [2]engine.Action
		for side := 0; side < 2; side++ {
			d := decide(dex, agents[side], s, side, budget)
			acts[side] = d.Action
			res.Usage[side] = res.Usage[side].Add(d.Usage)
			res.Decisions = append(res.Decisions, d)
		}
		logs := engine.ResolveTurn(dex, s, acts)

		// Drive the replacement cascade to completion: a replacement can itself
		// faint the incoming Pokémon (entry hazards), so this is a loop, not an
		// if. Its log lines belong to this turn and are appended to it.
		for !s.Ended() && s.Phase == engine.PhaseReplace {
			if len(res.Decisions) > maxDecisions {
				return res, turns, fmt.Errorf("battle %s did not terminate within %d decisions", id, maxDecisions)
			}
			var sw [2]*engine.Action
			for side := 0; side < 2; side++ {
				if !s.Replace[side] {
					continue
				}
				d := decide(dex, agents[side], s, side, budget)
				a := d.Action
				sw[side] = &a
				res.Usage[side] = res.Usage[side].Add(d.Usage)
				res.Decisions = append(res.Decisions, d)
			}
			logs = append(logs, engine.ResolveReplace(s, sw)...)
		}

		st, err := storedTurn(s, logs)
		if err != nil {
			return res, turns, err
		}
		turns = append(turns, st)
	}

	res.Winner = s.Winner
	res.Turns = s.Turn
	return res, turns, nil
}

func storedTurn(s *engine.BattleState, logs []engine.LogLine) (StoredTurn, error) {
	state, err := json.Marshal(s)
	if err != nil {
		return StoredTurn{}, fmt.Errorf("marshal state: %w", err)
	}
	if logs == nil {
		logs = []engine.LogLine{}
	}
	log, err := json.Marshal(logs)
	if err != nil {
		return StoredTurn{}, fmt.Errorf("marshal log: %w", err)
	}
	return StoredTurn{State: state, Log: log}, nil
}
