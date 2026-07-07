package ai

import (
	"context"
	"testing"
	"time"

	"pokearena/internal/engine"
)

// TestExpectimaxFixed_Deterministic: the fixed-depth agent must return the
// identical action for the identical View every time — the property the
// benchmark's ground-truth pilot depends on. Depth is a free parameter here;
// reproducibility is depth-independent, so the tests use depth 1 to stay fast
// (depth-2+ search costs seconds per decision).
func TestExpectimaxFixed_Deterministic(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9, 26}, "B", []int{3, 65, 143}, 7)
	v := MakeView(s, 0)

	a := NewExpectimaxAgentFixed(d, 1)
	first, err := a.Decide(context.Background(), v)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	for i := 0; i < 5; i++ {
		got, err := a.Decide(context.Background(), v)
		if err != nil {
			t.Fatalf("decide %d: %v", i, err)
		}
		if got != first {
			t.Fatalf("non-deterministic choice: run %d gave %+v, want %+v", i, got, first)
		}
	}
}

// stateFoeActiveFainted builds a reconstruction-shaped state: my side has one
// healthy Pokémon, the foe side holds only its (now fainted) visible active —
// exactly what the search sees after KOing the foe's active, since the foe's
// bench is hidden and lives only in searchCtx.foeBench.
func stateFoeActiveFainted() *engine.BattleState {
	s := &engine.BattleState{Phase: engine.PhaseEnded, Winner: 0}
	s.Sides[0] = engine.Side{Team: []engine.Pokemon{{MaxHP: 100, HP: 100}}, Active: 0}
	s.Sides[1] = engine.Side{Team: []engine.Pokemon{{MaxHP: 100, HP: 0, Fainted: true}}, Active: 0}
	return s
}

// TestExpectimax_PhantomKOIsNotAWin is the regression test for the opponent-model
// fix. KOing the foe's visible active is a won game ONLY when the foe truly has
// no Pokémon left; while bench remains it must be a finite material lead, far
// below a real win. The old model returned +1e6 for both, which is what made
// deeper search chase phantom KOs and play worse.
func TestExpectimax_PhantomKOIsNotAWin(t *testing.T) {
	d := loadDex(t)
	a := NewExpectimaxAgentFixed(d, 1)
	s := stateFoeActiveFainted()

	realWin := a.value(searchCtx{me: 0, foeBench: 0}, s, 1)
	if realWin < winValue {
		t.Fatalf("foe truly out should be a win (>= %.0f), got %.1f", winValue, realWin)
	}

	phantom := a.value(searchCtx{me: 0, foeBench: 2}, s, 1)
	if phantom >= winValue {
		t.Fatalf("KO with 2 foe on the bench must NOT be a win, got %.1f", phantom)
	}
	if phantom > winValue/10 {
		t.Fatalf("phantom KO scored %.1f — should be a modest material value, nowhere near a win", phantom)
	}

	// More of the foe's team still standing is strictly worse for me: material
	// must be monotonic in the hidden bench count.
	less := a.value(searchCtx{me: 0, foeBench: 1}, s, 1)
	if !(less > phantom) {
		t.Fatalf("foeBench=1 (%.1f) should score higher than foeBench=2 (%.1f)", less, phantom)
	}
}

// TestExpectimax_DoubleKOIsADraw: when a searched line wipes BOTH sides in the
// same turn (mutual Explosion, recoil/Rough-Skin killing the attacker as its
// move KOs the last foe), the position is a draw — worth 0, strictly between a
// win and a loss. The old value() checked myAlive==0 first and scored it as a
// total loss (-winValue), which made the pilot flee even trades it should take.
func TestExpectimax_DoubleKOIsADraw(t *testing.T) {
	d := loadDex(t)
	a := NewExpectimaxAgentFixed(d, 1)

	// Both actives fainted and the foe has no hidden bench: a true mutual wipe.
	s := &engine.BattleState{Phase: engine.PhaseEnded, Winner: 2}
	s.Sides[0] = engine.Side{Team: []engine.Pokemon{{MaxHP: 100, HP: 0, Fainted: true}}, Active: 0}
	s.Sides[1] = engine.Side{Team: []engine.Pokemon{{MaxHP: 100, HP: 0, Fainted: true}}, Active: 0}

	draw := a.value(searchCtx{me: 0, foeBench: 0}, s, 1)
	if draw != 0 {
		t.Fatalf("double-KO draw must score 0, got %.1f", draw)
	}
	// A draw must sit strictly above a loss and below a win.
	loss := a.value(searchCtx{me: 0, foeBench: 1}, s, 1) // foe still has bench: I lost
	if loss != -winValue {
		t.Fatalf("my wipe while foe has bench must be a loss (%.0f), got %.1f", -winValue, loss)
	}
	if !(draw > loss) {
		t.Fatalf("draw (%.1f) must outrank a loss (%.1f)", draw, loss)
	}
}

// TestExpectimaxFixed_IgnoresDeadline: an already-expired context must not
// change the fixed-depth agent's answer. The time-budgeted agent would bail
// early and fall back to a shallower (or first-action) choice; the fixed-depth
// agent searches to full depth regardless, which is what makes it machine-
// independent.
func TestExpectimaxFixed_IgnoresDeadline(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9, 26}, "B", []int{3, 65, 143}, 7)
	v := MakeView(s, 0)

	a := NewExpectimaxAgentFixed(d, 1)
	want, _ := a.Decide(context.Background(), v)

	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()
	got, err := a.Decide(expired, v)
	if err != nil {
		t.Fatalf("decide with expired ctx: %v", err)
	}
	if got != want {
		t.Fatalf("expired deadline changed the choice: got %+v, want %+v", got, want)
	}
}
