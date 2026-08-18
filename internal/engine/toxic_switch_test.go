package engine

import "testing"

// Toxic's escalating clock resets when the badly-poisoned Pokémon leaves the
// field (Gen 3+). doSwitchWithCarry used to reset Stages and Volatiles but
// leave ToxicCounter alone, so a benched Pokémon carried a clock it had no way
// to clear: the tournament Machamp that filed this came back on turn 37 still
// ticking 5/16 after 24 turns off the field, and got one action before dying.
//
// The status itself must survive — only the multiplier goes back to the bottom
// rung.
func TestSwitchResetsToxicCounter(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143, 59}, "B", []int{124, 3}, 5)
	if err != nil {
		t.Fatal(err)
	}
	a := s.Active(0)
	a.Ability, a.Item = AbilityNone, ItemNone
	a.Status, a.ToxicCounter = StatusToxic, 5
	a.HP = a.MaxHP
	// The foe idles all game: the only HP the subject loses is its own toxic
	// chip, so the tick can be measured off the HP delta directly.
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	// Out to the bench.
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
	benched := s.Sides[0].Team[0]
	if benched.Status != StatusToxic {
		t.Fatalf("switching out cured the poison: status=%v, want toxic", benched.Status)
	}
	if benched.ToxicCounter != 1 {
		t.Fatalf("ToxicCounter on the bench = %d, want 1 (the clock resets on switch-out)",
			benched.ToxicCounter)
	}

	// Back in, and the first tick since returning is the bottom rung, not the
	// count it left on. ToxicCounter is the *next* tick's numerator, so a reset
	// to 0 rather than 1 would show up here as a free damage-less turn.
	back := &s.Sides[0].Team[0]
	back.HP = back.MaxHP
	before := back.HP
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 0}, {Kind: ActionMove, Index: 0}})

	got := before - s.Sides[0].Team[0].HP
	if want := back.MaxHP / 16; got != want {
		t.Errorf("first toxic tick after returning = %d HP, want %d (1/16); counter=%d",
			got, want, s.Sides[0].Team[0].ToxicCounter)
	}
	if c := s.Sides[0].Team[0].ToxicCounter; c != 2 {
		t.Errorf("ToxicCounter after one tick back on the field = %d, want 2", c)
	}
}

// The reset is keyed on the status, so a Pokémon that is not badly poisoned
// keeps its zeroed counter rather than being handed a live one on the way out.
func TestSwitchLeavesUnpoisonedToxicCounterAlone(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{143, 59}, "B", []int{124, 3}, 5)
	if err != nil {
		t.Fatal(err)
	}
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
	if c := s.Sides[0].Team[0].ToxicCounter; c != 0 {
		t.Errorf("unpoisoned switch-out left ToxicCounter = %d, want 0", c)
	}
}
