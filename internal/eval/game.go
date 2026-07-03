// Package eval drives headless agent-vs-agent battles and records a
// per-decision trace. It is the substrate the benchmark stands on: given a
// dex, two agents, teams, and a seed, RunGame plays a full battle with no UI
// and returns a deterministic, replayable result.
//
// Determinism is the whole point. The engine RNG is seeded from the battle
// seed, so for any two deterministic agents the same (agents, teams, seed)
// produces a byte-identical GameResult — same winner, same turn count, same
// decision trace, same state fingerprints. That reproducibility is what lets
// a third party clone the repo and confirm every published number.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// maxDecisions caps a single game so a non-terminating battle (an engine bug,
// or two agents that stall forever) fails loudly instead of hanging the runner.
const maxDecisions = 20000

// Decision is one recorded choice at one decision point. It carries enough to
// (a) verify reproducibility via StateHash and (b) later score the move against
// expectimax-optimal for per-move regret — the flagship metric. The View that
// produced it is fingerprinted rather than embedded to keep traces compact;
// Step 3 can re-derive the full state by replaying the seed to this point.
type Decision struct {
	Turn  int             `json:"turn"`
	Side  int             `json:"side"`
	Phase engine.Phase    `json:"phase"`
	// Action is what actually resolved. If the agent proposed an illegal action
	// it is replaced by the first legal one and Fallback is set — mirroring the
	// gateway's real behavior and making that failure mode measurable.
	Action   engine.Action   `json:"action"`
	Legal    []engine.Action `json:"legal"`
	Fallback bool            `json:"fallback,omitempty"`
	// StateHash fingerprints the decision-point View. Equal hashes across two
	// runs mean byte-identical decision states — the core reproducibility check.
	StateHash string `json:"state_hash"`
}

// GameResult is the full record of one battle.
type GameResult struct {
	Seed      uint64     `json:"seed"`
	Winner    int        `json:"winner"` // 0 or 1 = side, 2 = draw
	Turns     int        `json:"turns"`
	Decisions []Decision `json:"decisions"`
}

// Budget is the per-decision time limit handed to each agent's context.
// Zero means no deadline (correct for deterministic agents; LLM agents want a
// real budget). The engine also enforces legality regardless.
type Budget time.Duration

// RunGame plays one full battle between agents[0] and agents[1] with the given
// per-side teams and seed, returning the recorded result.
//
// Teams are TeamPicks (chosen 1–4 moves per mon), built via NewBattleFromPicks
// — NOT bare dex numbers, which would hand every Pokémon its full learnset and
// balloon the decision space into something that isn't competitive Pokémon.
//
// teams[0] == teams[1] gives a mirror match: identical rosters, identical RNG,
// so the only free variable is the policy. That is how a win becomes evidence
// about the agent rather than the draw.
func RunGame(dex *domain.Dex, agents [2]ai.Agent, teams [2][]engine.TeamPick, seed uint64, budget Budget) (GameResult, error) {
	id := fmt.Sprintf("eval-%d", seed)
	s, err := engine.NewBattleFromPicks(dex, id, "P0", teams[0], "P1", teams[1], seed)
	if err != nil {
		return GameResult{}, fmt.Errorf("new battle: %w", err)
	}

	res := GameResult{Seed: seed}

	for !s.Ended() {
		if len(res.Decisions) > maxDecisions {
			return res, fmt.Errorf("battle %s did not terminate within %d decisions", id, maxDecisions)
		}

		switch s.Phase {
		case engine.PhaseChoosing:
			var acts [2]engine.Action
			for side := 0; side < 2; side++ {
				d := decide(dex, agents[side], s, side, budget)
				acts[side] = d.Action
				res.Decisions = append(res.Decisions, d)
			}
			engine.ResolveTurn(dex, s, acts)

		case engine.PhaseReplace:
			var sw [2]*engine.Action
			for side := 0; side < 2; side++ {
				if !s.Replace[side] {
					continue
				}
				d := decide(dex, agents[side], s, side, budget)
				a := d.Action
				sw[side] = &a
				res.Decisions = append(res.Decisions, d)
			}
			engine.ResolveReplace(s, sw)

		default:
			return res, fmt.Errorf("battle %s in unexpected phase %q", id, s.Phase)
		}
	}

	res.Winner = s.Winner
	res.Turns = s.Turn
	return res, nil
}

// decide projects the fog-of-war View for one side, asks the agent, and records
// the choice. An illegal or errored proposal is deterministically replaced by
// the first legal action (and flagged), so the game always makes legal progress
// and the substitution is itself part of the reproducible trace.
func decide(dex *domain.Dex, agent ai.Agent, s *engine.BattleState, side int, budget Budget) Decision {
	v := ai.MakeView(s, side)
	legal := ai.LegalActions(v)

	ctx := context.Background()
	if budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(budget))
		defer cancel()
	}

	d := Decision{
		Turn:      s.Turn,
		Side:      side,
		Phase:     s.Phase,
		Legal:     legal,
		StateHash: hashView(v),
	}

	act, err := agent.Decide(ctx, v)
	if err != nil || !isLegal(legal, act) {
		if len(legal) > 0 {
			act = legal[0]
		}
		d.Fallback = true
	}
	d.Action = act
	return d
}

func isLegal(legal []engine.Action, a engine.Action) bool {
	for _, l := range legal {
		if l == a {
			return true
		}
	}
	return false
}

// hashView fingerprints a View by its canonical JSON. Go marshals map keys in
// sorted order, so the encoding is stable and the hash is a reliable equality
// check for decision states across runs.
func hashView(v ai.View) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "err"
	}
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("%016x", h.Sum64())
}
