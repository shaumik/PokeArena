package engine

import "testing"

// TestGluttonyDoesNotEatTheMomentTheGasClears: the three readings of the
// Showdown case, as a whole-battle sequence.
//
// Gluttony is a latch upstream, not a passive threshold: the berries test
// abilityState.gluttony, which the ability sets from onStart and onDamage.
// Neutralizing Gas's onEnd re-runs every surviving ability's Start event and
// then sets Gluttony's latch back to false — one line of special-casing whose
// entire purpose is this case. Modeled as a threshold check alone, a holder
// sitting under the half-HP line swallows its berry the instant the gas leaves.
//
// Snorlax at 235 max HP puts the Aguav Berry's own line at 58 and Gluttony's at
// 117, so 93 HP is a spot only Gluttony reaches. That is what makes the middle
// reading mean anything.
func TestGluttonyDoesNotEatTheMomentTheGasClears(t *testing.T) {
	d := loadDex(t)
	const (
		snorlax   = 143
		weezing   = 110
		vileplume = 45
	)
	s, err := NewBattle(d, "b", "Holder", []int{snorlax}, "Gasser", []int{weezing, vileplume}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability = AbilityGluttony
	holder.Item = "aguav-berry"
	holder.Moves = cbmMoves("splash")
	holder.HP = 118 // one point above Gluttony's line
	for i := range s.Sides[1].Team {
		p := &s.Sides[1].Team[i]
		p.Item = ItemNone
		p.Moves = cbmMoves("tackle", "splash")
	}
	s.Sides[1].Team[0].Ability = AbilityNeutralizingGas
	s.Sides[1].Team[1].Ability = AbilityNone
	syncAbilitySuppression(s, &[]LogLine{})

	// Turn 1: the tackle drops the holder into Gluttony range, but the gas is
	// up, so the ability does nothing and the berry stays.
	cbmTurn(d, s, 0, 0)
	if holder.HP > 117 {
		t.Fatalf("setup: wanted the holder inside Gluttony's range, got %d HP", holder.HP)
	}
	if holder.HP <= 58 {
		t.Fatalf("setup: the holder must stay above the berry's own line, got %d HP", holder.HP)
	}
	if holder.Item == ItemNone {
		t.Fatalf("Neutralizing Gas should have kept Gluttony from reaching the berry")
	}

	// Turn 2: the gas walks off the field. The berry must survive the moment.
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionSwitch, Index: 1}})
	if holder.Item == ItemNone {
		t.Errorf("Gluttony must not eat the berry the moment Neutralizing Gas leaves")
	}

	// Turn 3: the next HP drop re-arms it, and now the berry goes.
	cbmTurn(d, s, 0, 0)
	if holder.Item != ItemNone {
		t.Errorf("Gluttony should get its chance back on the next HP drop, still holding %q",
			holder.Item)
	}
}

// TestGluttonyIsUnlatchedWithoutAnyGas: the latch must not cost the ability
// anything in the ordinary case. A holder that has never met Neutralizing Gas
// eats at half HP on the first drop that takes it there.
func TestGluttonyIsUnlatchedWithoutAnyGas(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Holder", []int{143}, "Foe", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability = AbilityGluttony
	holder.Item = "aguav-berry"
	holder.Moves = cbmMoves("splash")
	holder.HP = 118
	foe := s.Active(1)
	foe.Item = ItemNone
	foe.Ability = AbilityNone
	foe.Moves = cbmMoves("tackle")

	cbmTurn(d, s, 0, 0)
	if holder.Item != ItemNone {
		t.Errorf("Gluttony with no gas in sight should eat at half HP, still holding %q at %d HP",
			holder.Item, holder.HP)
	}
}
