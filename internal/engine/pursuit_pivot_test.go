package engine

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// pursuit_pivot_test.go covers Pursuit's second interception site: a pivot move
// (U-turn, Volt Switch, Flip Turn, Teleport) whose user is about to leave under
// its own power.
//
// The engine already handled a *chosen* switch, which is canon's first site
// inside switchIn. The second is the tail of runAction, where a move that set
// switchFlag gets a BeforeSwitchOut before the replacement is even requested —
// which is what the ported case is named after.

// pursuitBattle: a slow pursuer against a fast pivot. Speed matters and is not
// incidental — canon only intercepts while the pursuer's own action is unspent,
// so a *faster* Pursuit user gets no chase at all.
func pursuitBattle(t *testing.T, d *domain.Dex, pivotMove string) (*BattleState, []LogLine) {
	t.Helper()
	s, err := NewBattle(d, "b", "Chaser", []int{47}, "Pivot", []int{135, 143}, 1) // Parasect vs Jolteon
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "pursuit", PP: 20, MaxPP: 20}}
	s.Active(1).Moves = []MoveSlot{{MoveID: pivotMove, PP: 20, MaxPP: 20}}
	log := playTurn(d, s, 0, 0)
	return s, log
}

// TestPursuitCatchesAPivotBeforeItLeaves: the case itself. Without this the
// Pursuit lands on the *incoming* Pokémon at normal power, which is both the
// wrong target and the wrong number.
func TestPursuitCatchesAPivotBeforeItLeaves(t *testing.T) {
	d := loadDex(t)
	s, _ := pursuitBattle(t, d, "volt-switch")
	leaver := &s.Sides[1].Team[0]
	if leaver.HP >= leaver.MaxHP {
		t.Errorf("the Volt Switch user should have been caught on its way out, still at %d/%d",
			leaver.HP, leaver.MaxHP)
	}
	if arrival := &s.Sides[1].Team[1]; arrival.HP < arrival.MaxHP {
		t.Errorf("the replacement should arrive untouched, at %d/%d", arrival.HP, arrival.MaxHP)
	}
	if got := s.Active(1).Name; got == leaver.Name {
		t.Errorf("the pivot should still have completed its switch, active is %q", got)
	}
}

// TestPursuitDoesNotChaseABatonPass: the carve-out, and it is per-move rather
// than per-category. Baton Pass and Shed Tail set skipBeforeSwitchOutEventFlag
// from their own onHit; U-turn, Volt Switch, Flip Turn and Teleport do not.
func TestPursuitDoesNotChaseABatonPass(t *testing.T) {
	d := loadDex(t)
	s, _ := pursuitBattle(t, d, "baton-pass")
	leaver := &s.Sides[1].Team[0]
	if leaver.HP != leaver.MaxHP {
		t.Errorf("Baton Pass leaves without giving Pursuit its chance, got %d/%d",
			leaver.HP, leaver.MaxHP)
	}
}

// TestPursuitOnlyActsOnce: the chase spends the pursuer's action. Letting it
// also take its turn in the mover loop would be two Pursuits in one turn — the
// failure mode a "fire it from inside the pivot" fix invites.
func TestPursuitOnlyActsOnce(t *testing.T) {
	d := loadDex(t)
	s, log := pursuitBattle(t, d, "volt-switch")
	uses := 0
	for _, l := range log {
		if l.Text == "Parasect used Pursuit!" {
			uses++
		}
	}
	if uses != 1 {
		t.Errorf("Pursuit should resolve exactly once, saw %d uses in %v", uses, logTexts(log))
	}
	if pp := s.Sides[0].Team[0].Moves[0].PP; pp != 19 {
		t.Errorf("one use should cost one PP, got %d left of 20", pp)
	}
}

// TestPursuitDoublesAgainstAPivot: same ×2 the chosen-switch path gets. Canon's
// basePowerCallback reads target.switchFlag, which a pivot user carries just as
// a chosen switch does, so the two sites must agree.
func TestPursuitDoublesAgainstAPivot(t *testing.T) {
	d := loadDex(t)
	s, _ := pursuitBattle(t, d, "volt-switch")
	chased := s.Sides[1].Team[0].MaxHP - s.Sides[1].Team[0].HP

	// Control: the same pursuer hitting the same body with nobody leaving.
	c, err := NewBattle(d, "b", "Chaser", []int{47}, "Pivot", []int{135, 143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	c.Active(0).Moves = []MoveSlot{{MoveID: "pursuit", PP: 20, MaxPP: 20}}
	c.Active(1).Moves = []MoveSlot{{MoveID: "growl", PP: 40, MaxPP: 40}}
	playTurn(d, c, 0, 0)
	plain := c.Sides[1].Team[0].MaxHP - c.Sides[1].Team[0].HP

	if plain == 0 {
		t.Fatalf("control: Pursuit should have dealt damage")
	}
	if chased < plain*3/2 {
		t.Errorf("an intercepting Pursuit should hit at roughly double power: %d chased vs %d plain",
			chased, plain)
	}
}

// TestPursuitKOingAPivotStopsTheSwitch: canon's 'pursuitfaint'. A user killed
// on its way out never leaves — the replacement comes through the replace phase
// instead, not through the move.
func TestPursuitKOingAPivotStopsTheSwitch(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Chaser", []int{47}, "Pivot", []int{135, 143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "pursuit", PP: 20, MaxPP: 20}}
	pivot := s.Active(1)
	pivot.Moves = []MoveSlot{{MoveID: "volt-switch", PP: 20, MaxPP: 20}}
	pivot.HP = 1

	playTurn(d, s, 0, 0)
	if !s.Sides[1].Team[0].Fainted {
		t.Fatalf("setup: the chase should have KO'd the 1 HP pivot user")
	}
	if s.Sides[1].Active != 0 {
		t.Errorf("a pivot user KO'd on the way out does not complete its switch; active is %d",
			s.Sides[1].Active)
	}
	if !s.Replace[1] {
		t.Errorf("the KO should route into the replace phase instead")
	}
}
