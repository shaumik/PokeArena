package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// trapping_release_test.go covers the two rules a move-based trap has beyond
// "the volatile exists": a Ghost is never held, and the hold ends when the
// trapper leaves. The engine had neither, and its comments asserted the
// opposite of both.

func trapBattle(t *testing.T, d *domain.Dex, victimDex int) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "tr", "P1", []int{143, 65}, "P2", []int{victimDex, 65}, 13)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			p.Item, p.Ability = ItemNone, AbilityNone
			p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		}
	}
	return s
}

func canSwitch(s *BattleState, side int) bool {
	for _, a := range LegalActions(s, side) {
		if a.Kind == ActionSwitch {
			return true
		}
	}
	return false
}

// TestAPartialTrapHoldsButNotAGhost: the hold is refused, the trap is not.
// Canon refuses the hold through tryTrap's type immunity and never refuses the
// volatile — `partiallytrapped` is not a name in the type chart — so a Ghost
// walks out *and* keeps taking the chip. Refusing the volatile would have been
// the wrong fix wearing the right shape.
func TestAPartialTrapHoldsButNotAGhost(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		dex  int
		held bool
		what string
	}{
		{143, true, "a Normal-type"},
		{94, false, "a Ghost"}, // Gengar
	} {
		s := trapBattle(t, d, c.dex)
		victim := s.Active(1)
		victim.Volatiles.PartialTrap = &PartialTrapState{Turns: 4, MoveName: "Wrap"}
		if got := !canSwitch(s, 1); got != c.held {
			t.Errorf("%s: held = %v, want %v", c.what, got, c.held)
		}
		// Either way the trap is still there and still chipping.
		before := victim.HP
		var log []LogLine
		applyResidual(s, 1, &log)
		if victim.HP >= before {
			t.Errorf("%s should still take the trap's chip, %d -> %d", c.what, before, victim.HP)
		}
	}
}

// TestATrapEndsWhenItsTrapperLeaves: the hold belongs to the trapper, not to
// the volatile. Every route off the field funnels through installSwitchIn, so
// one release covers a chosen switch, a replacement, a pivot and a drag.
func TestATrapEndsWhenItsTrapperLeaves(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		name  string
		apply func(v *Pokemon)
		check func(v *Pokemon) bool
	}{
		{
			name:  "partial trap",
			apply: func(v *Pokemon) { v.Volatiles.PartialTrap = &PartialTrapState{Turns: 4, MoveName: "Wrap"} },
			check: func(v *Pokemon) bool { return v.Volatiles.PartialTrap != nil },
		},
		{
			name:  "Mean Look",
			apply: func(v *Pokemon) { v.Volatiles.Trapped = true },
			check: func(v *Pokemon) bool { return v.Volatiles.Trapped },
		},
	} {
		s := trapBattle(t, d, 143)
		victim := s.Active(1)
		c.apply(victim)
		if canSwitch(s, 1) {
			t.Fatalf("%s: setup, the victim should be held", c.name)
		}

		// The trapper switches out.
		ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
		if c.check(victim) {
			t.Errorf("%s should have been released when the trapper left", c.name)
		}
		if !canSwitch(s, 1) {
			t.Errorf("%s: the victim should be free to switch once the trapper is gone", c.name)
		}
	}
}

// TestAFaintedTrapperDoesNotHold: the window between the trapper fainting and
// its replacement arriving. The volatile is still set, so the release has not
// run yet; the gate has to answer this on its own.
func TestAFaintedTrapperDoesNotHold(t *testing.T) {
	d := loadDex(t)
	s := trapBattle(t, d, 143)
	victim := s.Active(1)
	victim.Volatiles.PartialTrap = &PartialTrapState{Turns: 4, MoveName: "Wrap"}
	if canSwitch(s, 1) {
		t.Fatalf("setup: the victim should be held")
	}

	s.Active(0).HP = 0
	faint(s.Active(0), 0, &[]LogLine{})
	if !canSwitch(s, 1) {
		t.Error("a fainted trapper holds nobody")
	}
}

// TestRapidSpinFreesTheSpinner: canon clears the partial trap and Leech Seed
// from the same onAfterHit as the hazards. Both are hoisted above the hazard
// check, so a spinner with nothing to blow away still frees itself.
func TestRapidSpinFreesTheSpinner(t *testing.T) {
	d := loadDex(t)
	s := trapBattle(t, d, 143)
	spinner := s.Active(0)
	spinner.Moves = []MoveSlot{{MoveID: "rapid-spin", PP: 40, MaxPP: 40}}
	spinner.Volatiles.PartialTrap = &PartialTrapState{Turns: 4, MoveName: "Wrap"}
	spinner.Volatiles.LeechSeed = &LeechSeedState{SourceSide: 1}
	// No hazards on the spinner's side: the release must not be gated on them.

	playTurn(d, s, 0, 0)
	if spinner.Volatiles.PartialTrap != nil {
		t.Error("Rapid Spin should free the spinner from a partial trap")
	}
	if spinner.Volatiles.LeechSeed != nil {
		t.Error("Rapid Spin should clear the spinner's Leech Seed")
	}
}

// TestUnnerveStopsTheFoeEatingBerries, and stops nothing else: the item is
// still held, still knock-offable, still fills the slot.
func TestUnnerveStopsTheFoeEatingBerries(t *testing.T) {
	d := loadDex(t)
	s := trapBattle(t, d, 143)
	holder := s.Active(0)
	holder.Ability = "unnerve"
	holder.Volatiles.Unnerve = true
	foe := s.Active(1)
	foe.Item = "sitrus-berry"
	foe.HP = foe.MaxHP / 4

	var log []LogLine
	applyItemHPTrigger(s, 1, NewRNG(1), &log)
	if foe.Item != "sitrus-berry" {
		t.Error("Unnerve should have stopped the berry being eaten")
	}
	if foe.HP != foe.MaxHP/4 {
		t.Error("and therefore stopped the heal")
	}
}

// TestUnnerveLetsABerryThroughBetweenSwitches: the whole ported case. Canon
// resolves a switch as two queue actions with an Update between them, so there
// is a window where the old holder is gone and the new one's entry hook has not
// fired — and the berry that was being held back gets eaten in it. A live "is
// the foe holding Unnerve" read would close that window and lose the case,
// which is why the ability is a latch.
func TestUnnerveLetsABerryThroughBetweenSwitches(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "un", "P1", []int{143, 65}, "P2", []int{143}, 13)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			p.Item, p.Ability = ItemNone, AbilityNone
			p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		}
	}
	// Both of the switching side's Pokémon carry Unnerve, so the only moment
	// the foe is un-nerved is the gap between them.
	s.Sides[0].Team[0].Ability = "unnerve"
	s.Sides[0].Team[1].Ability = "unnerve"
	s.Active(0).Volatiles.Unnerve = true

	foe := s.Active(1)
	foe.Item = "lum-berry"
	foe.Status = StatusParalysis

	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
	if foe.Item != ItemNone {
		t.Error("the berry should have been eaten in the gap between the two Unnerve holders")
	}
	if foe.Status != StatusNone {
		t.Errorf("and it should have cured the paralysis, got %q", foe.Status)
	}
	if !s.Active(0).Volatiles.Unnerve {
		t.Error("the arrival should have armed its own latch on the way in")
	}
}
