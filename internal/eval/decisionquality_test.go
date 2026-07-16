package eval

import (
	"context"
	"encoding/json"
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
)

func faintedCount(s *engine.BattleState) int {
	n := 0
	for _, side := range s.Sides {
		for _, p := range side.Team {
			if p.Fainted {
				n++
			}
		}
	}
	return n
}

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

// TestRecoverActions_FaintTurns plays a full heuristic-vs-heuristic game (which
// KOs Pokémon and forces mid-turn replacements), stores each settled turn the
// way the live coordinator does, and checks that recoverActions re-derives the
// exact actions on every turn — including the faint turns, where resolving must
// also settle the replacement pick. This is the coverage the naive single-
// ResolveTurn recovery misses.
func TestRecoverActions_FaintTurns(t *testing.T) {
	d := loadDex(t)
	// Frail, hard-hitting mirror so KOs (and replacements) happen quickly.
	s, err := engine.NewBattle(d, "b", "P0", []int{65, 94, 101}, "P1", []int{65, 94, 101}, 7)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	h := ai.NewHeuristicAgent(d)
	ctx := context.Background()

	var turns []StoredTurn
	sawFaint := false
	for guard := 0; !s.Ended(); guard++ {
		if guard > 500 {
			t.Fatal("battle did not terminate")
		}
		a0, _ := h.Decide(ctx, ai.MakeView(s, 0))
		a1, _ := h.Decide(ctx, ai.MakeView(s, 1))
		engine.ResolveTurn(d, s, [2]engine.Action{a0, a1})
		for s.Phase == engine.PhaseReplace {
			sawFaint = true
			var sw [2]*engine.Action
			for side := 0; side < 2; side++ {
				if s.Replace[side] {
					ra, _ := h.Decide(ctx, ai.MakeView(s, side))
					sw[side] = &ra
				}
			}
			engine.ResolveReplace(s, sw)
		}
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal state: %v", err)
		}
		turns = append(turns, StoredTurn{State: append(json.RawMessage(nil), b...)})
	}
	if !sawFaint {
		t.Fatal("matchup produced no faint — test would not exercise replacement recovery")
	}

	faintTurns := 0
	for i := 1; i < len(turns); i++ {
		var prev, next engine.BattleState
		if err := json.Unmarshal(turns[i-1].State, &prev); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(turns[i].State, &next); err != nil {
			t.Fatal(err)
		}
		if prev.Phase != engine.PhaseChoosing {
			continue
		}
		if _, ok := recoverActions(d, turns[i-1].State, &next); !ok {
			t.Errorf("turn %d: failed to recover actions (fainted prev=%d next=%d)",
				next.Turn, faintedCount(&prev), faintedCount(&next))
			continue
		}
		if faintedCount(&next) > faintedCount(&prev) {
			faintTurns++
		}
	}
	if faintTurns == 0 {
		t.Fatal("no recovered turn involved a faint — coverage claim unverified")
	}
	t.Logf("recovered every choosing turn across %d stored turns, %d with a faint", len(turns), faintTurns)
}
