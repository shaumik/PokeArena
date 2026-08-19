package engine

import "testing"

// Fake Out works only as the user's first action after entering the field.
//
// It had no restriction at all: `data/moves.json` carries it as priority +3
// with a 100%-chance flinch secondary, `domain.Move` has no field that could
// express "first turn only", and the string "fake-out" appeared in no Go file
// in the repo — so the gate could not even be written without new state. The
// result was a guaranteed flinch, at +3 priority, every turn, for as long as
// the user stayed in.
//
// It decided the final of the agent tournament that filed it. Priority is not
// reordered by Trick Room, so against a speed-inversion team an unrestricted
// Fake Out is not chip damage, it is a lock: the referee logged Persian using
// it on its 5th, 6th and 7th consecutive turns on the field, and the second
// Trick Room — with four live Pokémon behind it — produced no kills at all.
func TestFakeOutOnlyWorksOnTheFirstActionAfterEntering(t *testing.T) {
	d := loadDex(t)
	// Persian leads with Fake Out; the foe idles so nothing else moves the
	// board. Kangaskhan on the bench gives the tester somewhere to pivot.
	s, err := NewBattle(d, "b", "A", []int{53, 115}, "B", []int{143, 3}, 4)
	if err != nil {
		t.Fatal(err)
	}
	user := s.Active(0)
	user.Moves = []MoveSlot{
		{MoveID: "fake-out", PP: 10, MaxPP: 10},
		{MoveID: "u-turn", PP: 20, MaxPP: 20},
	}
	user.Item = ItemNone
	foe := s.Active(1)
	foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	foe.Ability, foe.Item = AbilityNone, ItemNone

	fakeOut := Action{Kind: ActionMove, Index: 0}
	idle := Action{Kind: ActionMove, Index: 0}

	// Turn 1: the user's first action. It lands.
	hpBefore := s.Active(1).HP
	ResolveTurn(d, s, [2]Action{fakeOut, idle})
	if s.Active(1).HP == hpBefore {
		t.Fatal("Fake Out did nothing on the user's first action out; it is supposed to work exactly then")
	}

	// Turn 2: the user's second action. It must fail.
	hpBefore = s.Active(1).HP
	log := ResolveTurn(d, s, [2]Action{fakeOut, idle})
	if s.Active(1).HP != hpBefore {
		t.Errorf("Fake Out dealt damage on the user's second action out (%d → %d); "+
			"it is a first-turn-only move", hpBefore, s.Active(1).HP)
	}
	if !hasLine(log, "But it failed!") {
		t.Errorf("second Fake Out did not log a failure; lines = %v", texts(log))
	}
	// The flinch must not land either — the whole point of the move.
	if s.Active(1).Volatiles.Flinch {
		t.Error("a refused Fake Out still flinched the target")
	}

	// Switching out and back in restores the privilege: the counter lives in
	// Volatiles precisely so leaving the field zeroes it.
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, idle})
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 0}, idle})
	hpBefore = s.Active(1).HP
	ResolveTurn(d, s, [2]Action{fakeOut, idle})
	if s.Active(1).HP == hpBefore {
		t.Error("Fake Out failed on the first action after re-entering; switching out must reset the counter")
	}
}

// The counter behind the gate counts *actions*, not successful moves: canon
// burns the first-turn privilege on a turn the user was fully paralysed or
// flinched, because the action ran and simply did nothing.
func TestMoveActionsCountsRefusedActions(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{53, 115}, "B", []int{143, 3}, 11)
	if err != nil {
		t.Fatal(err)
	}
	user := s.Active(0)
	user.Moves = []MoveSlot{{MoveID: "fake-out", PP: 10, MaxPP: 10}}
	user.Item = ItemNone
	// Asleep for several turns: the action still runs each turn and is refused
	// by canAct, which is exactly the case the counter has to count.
	user.Status, user.SleepTurns = StatusSleep, 3
	foe := s.Active(1)
	foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	foe.Ability, foe.Item = AbilityNone, ItemNone

	act := [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}}
	ResolveTurn(d, s, act)
	if got := s.Active(0).Volatiles.MoveActions; got != 1 {
		t.Fatalf("MoveActions after one refused action = %d, want 1", got)
	}
	ResolveTurn(d, s, act)
	if got := s.Active(0).Volatiles.MoveActions; got != 2 {
		t.Fatalf("MoveActions after two refused actions = %d, want 2", got)
	}
}

func texts(log []LogLine) []string {
	out := make([]string, 0, len(log))
	for _, l := range log {
		out = append(out, l.Text)
	}
	return out
}

func hasLine(log []LogLine, want string) bool {
	for _, l := range log {
		if l.Text == want {
			return true
		}
	}
	return false
}
