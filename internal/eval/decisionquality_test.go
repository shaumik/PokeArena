package eval

import (
	"encoding/json"
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
)

// stubOracle scores the first legal action worst (0) and the last one best
// (1000), so a policy that always plays move#0 disagrees with a known,
// fixed regret of 1000 — letting the test assert regret/blunder arithmetic
// without depending on real expectimax values.
type stubOracle struct{}

func (stubOracle) ScoreActions(v ai.View) []ai.ActionValue {
	acts := ai.LegalActions(v)
	if len(acts) <= 1 {
		return nil // no free choice — mirror expectimax's contract
	}
	out := make([]ai.ActionValue, len(acts))
	for i, a := range acts {
		out[i] = ai.ActionValue{Action: a}
	}
	out[len(out)-1].Value = 1000
	return out
}

// TestScoreDecisions_RecoversActionsAndRegret plays a fixed line (both sides
// spam move#0), stores each turn's state the way the live coordinator does, and
// checks that ScoreDecisions re-simulates back to the exact action played and
// computes regret against the oracle. A Snorlax mirror keeps anyone from
// fainting in the first few turns, so every turn is a clean, recoverable choice.
func TestScoreDecisions_RecoversActionsAndRegret(t *testing.T) {
	d := loadDex(t)
	s, err := engine.NewBattle(d, "b", "P0", []int{143, 131}, "P1", []int{143, 131}, 42)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}

	move0 := [2]engine.Action{{Kind: engine.ActionMove, Index: 0}, {Kind: engine.ActionMove, Index: 0}}
	var turns []StoredTurn
	for i := 0; i < 4 && !s.Ended(); i++ {
		if s.Phase != engine.PhaseChoosing {
			break
		}
		engine.ResolveTurn(d, s, move0)
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal state: %v", err)
		}
		turns = append(turns, StoredTurn{State: append(json.RawMessage(nil), b...)})
	}
	if len(turns) < 2 {
		t.Fatalf("need >= 2 stored turns to score a transition, got %d", len(turns))
	}

	scores, skipped, err := ScoreDecisions(d, stubOracle{}, 0, turns)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if len(scores) == 0 {
		t.Fatalf("no decisions scored (skipped=%d)", skipped)
	}
	for _, sc := range scores {
		if sc.Chosen.Kind != engine.ActionMove || sc.Chosen.Index != 0 {
			t.Errorf("turn %d: recovered %+v, want move#0", sc.Turn, sc.Chosen)
		}
		if sc.Regret != 1000 {
			t.Errorf("turn %d: regret %.0f, want 1000", sc.Turn, sc.Regret)
		}
		if !sc.Blunder {
			t.Errorf("turn %d: expected a blunder at regret 1000 (threshold %.0f)", sc.Turn, BlunderThreshold)
		}
		if sc.Agree {
			t.Errorf("turn %d: move#0 is the oracle's worst here, should not agree", sc.Turn)
		}
	}
}
