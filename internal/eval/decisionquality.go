package eval

import (
	"bytes"
	"encoding/json"
	"math"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// BlunderThreshold is the regret, in the oracle's eval points, above which a
// choice counts as a blunder. The oracle scores one whole Pokémon at ~1000
// points, so 300 ≈ giving up a third of a Pokémon's worth of position in a
// single move — enough to separate a genuine mistake from picking the
// second-best of two near-equal options. Tunable as the metric is calibrated.
const BlunderThreshold = 300.0

// Decision-quality eval scores how well a policy chose, not just whether it won.
// A live battle stored every turn's engine state, and the engine is a pure
// function of (state, actions) — so we can re-simulate each turn to recover the
// exact action a side played, then ask a stronger reference agent (the oracle)
// what it would have played from the *identical* fog-of-war view. The gap
// between the two is a per-decision quality signal that a win/loss record hides.
//
// The oracle sees exactly what the policy saw: ai.MakeView projects the same
// fog every agent decides from, so a deeper-searching expectimax is a fair
// yardstick rather than an omniscient one.

// Oracle is the reference policy a decision is scored against. It must expose
// per-action values (not just its top pick) so we can measure regret — how much
// value a choice gave up, not merely whether it matched. *ai.ExpectimaxAgent
// satisfies it; the interface keeps this package testable with a stub.
type Oracle interface {
	ScoreActions(v ai.View) []ai.ActionValue
}

// DecisionScore is one recovered free choice: the action a side actually played
// on a turn, the oracle's best from the identical view, and the value gap
// between them.
type DecisionScore struct {
	Turn    int           `json:"turn"`
	Side    int           `json:"side"`
	Chosen  engine.Action `json:"chosen"`
	Best    engine.Action `json:"best"`
	Agree   bool          `json:"agree"`   // Chosen == the oracle's top choice
	Regret  float64       `json:"regret"`  // oracle value of Best minus value of Chosen (>= 0)
	Blunder bool          `json:"blunder"` // Regret > BlunderThreshold
}

// recoverActions finds the [2]Action that, applied to prev, reproduces want
// byte-for-byte (both marshaled through engine.BattleState, so jsonb key-order
// differences wash out). It returns ok=false when no single action-pair
// reproduces want — most often a turn where a Pokémon fainted and a replacement
// was chosen mid-turn, which v1 does not yet score. Because the engine is
// deterministic from the stored RNGState, the matching pair is exactly what was
// played, and the match doubles as a validity check on the recorded state.
func recoverActions(dex *domain.Dex, prev json.RawMessage, want *engine.BattleState) ([2]engine.Action, bool) {
	wantJSON, err := json.Marshal(want)
	if err != nil {
		return [2]engine.Action{}, false
	}
	var base engine.BattleState
	if err := json.Unmarshal(prev, &base); err != nil {
		return [2]engine.Action{}, false
	}
	a0s := engine.LegalActionsDex(dex, &base, 0)
	a1s := engine.LegalActionsDex(dex, &base, 1)
	for _, a0 := range a0s {
		for _, a1 := range a1s {
			var trial engine.BattleState
			if err := json.Unmarshal(prev, &trial); err != nil {
				continue
			}
			engine.ResolveTurn(dex, &trial, [2]engine.Action{a0, a1})
			if got, err := json.Marshal(&trial); err == nil && bytes.Equal(got, wantJSON) {
				return [2]engine.Action{a0, a1}, true
			}
		}
	}
	return [2]engine.Action{}, false
}

// ScoreDecisions replays a live battle's stored turns and, for every clean free
// choice on modelSide it can recover, compares the model's action to the
// oracle's from the identical fog-of-war view. It returns one score per
// recovered decision plus the count of choosing-turns it could not recover
// (the faint/replacement turns v1 skips), so callers can report coverage rather
// than silently dropping them.
func ScoreDecisions(dex *domain.Dex, orc Oracle, modelSide int, turns []StoredTurn) (scores []DecisionScore, skipped int, err error) {
	for i := 1; i < len(turns); i++ {
		var prevState engine.BattleState
		if err := json.Unmarshal(turns[i-1].State, &prevState); err != nil {
			return nil, skipped, err
		}
		// Only a turn chosen from a choosing state is a free decision; the last
		// stored state (phase "ended") is a terminal snapshot, not a choice.
		if prevState.Phase != engine.PhaseChoosing {
			continue
		}
		var next engine.BattleState
		if err := json.Unmarshal(turns[i].State, &next); err != nil {
			return nil, skipped, err
		}
		acts, ok := recoverActions(dex, turns[i-1].State, &next)
		if !ok {
			skipped++
			continue
		}
		v := ai.MakeView(&prevState, modelSide)
		vals := orc.ScoreActions(v)
		if len(vals) == 0 {
			// Forced or single-option turn — no free choice to score.
			skipped++
			continue
		}
		chosen := acts[modelSide]
		best := vals[0]
		chosenVal, found := math.Inf(-1), false
		for _, av := range vals {
			if av.Value > best.Value {
				best = av
			}
			if av.Action == chosen {
				chosenVal, found = av.Value, true
			}
		}
		if !found {
			// The recovered action isn't in the oracle's legal set — a fog or
			// version mismatch; skip rather than report a bogus regret.
			skipped++
			continue
		}
		regret := best.Value - chosenVal
		scores = append(scores, DecisionScore{
			Turn:    next.Turn,
			Side:    modelSide,
			Chosen:  chosen,
			Best:    best.Action,
			Agree:   chosen == best.Action,
			Regret:  regret,
			Blunder: regret > BlunderThreshold,
		})
	}
	return scores, skipped, nil
}
