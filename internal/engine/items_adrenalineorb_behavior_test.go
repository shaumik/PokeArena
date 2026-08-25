package engine

import "testing"

// items_adrenalineorb_behavior_test.go pins the one distinction this item is
// really about: a drop that a guard *refused* and a drop that landed as zero
// are different events, and only the first arms the orb.
//
// Upstream carries that rule in the shape of a JS object — a guard deletes
// boost.atk, the floor sets it to 0 — which is not a thing this engine has. So
// it is reconstructed from two explicit pieces, and these tests are what keep
// the reconstruction honest.

// orbBattle puts an Intimidate holder on side 1 and the orb on side 0.
func orbBattle(t *testing.T) (*BattleState, *Pokemon) {
	t.Helper()
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	mine := s.Active(0)
	mine.Item = ItemAdrenalineOrb
	for i := range s.Sides[0].Team {
		teachMoves(t, d, &s.Sides[0].Team[i], "splash")
	}
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "splash")
	}
	s.Active(1).Ability = AbilityIntimidate
	seedAbilitySuppression(s)
	return s, mine
}

// intimidated runs the turn on which the Intimidate holder's entry hook fires.
func intimidated(t *testing.T, s *BattleState, mine *Pokemon) {
	t.Helper()
	playTurn(loadDex(t), s, 0, 0)
}

// TestAdrenalineOrbFiresOnAPlainIntimidate.
func TestAdrenalineOrbFiresOnAPlainIntimidate(t *testing.T) {
	s, mine := orbBattle(t)
	intimidated(t, s, mine)

	if mine.Stages.Spe != 1 {
		t.Errorf("the orb should have raised Speed to +1, got %+d", mine.Stages.Spe)
	}
	if mine.Item != ItemNone {
		t.Errorf("and consumed itself, still holding %q", mine.Item)
	}
	if mine.Stages.Atk != -1 {
		t.Errorf("fixture: the Attack drop should also have landed, got %+d", mine.Stages.Atk)
	}
}

// TestAdrenalineOrbFiresWhenAGuardRefusedTheDrop. This is the half the
// upstream comment exists to explain: Mist and the stat-guard abilities delete
// the boost entry rather than zeroing it, and the orb reads that as "somebody
// tried to intimidate me".
func TestAdrenalineOrbFiresWhenAGuardRefusedTheDrop(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(s *BattleState, mine *Pokemon)
	}{
		{"Mist", func(s *BattleState, _ *Pokemon) {
			s.Sides[0].Conditions.Mist = &MistState{TurnsLeft: 5}
		}},
		{"Clear Body", func(_ *BattleState, mine *Pokemon) {
			mine.Ability = "clear-body"
		}},
		{"Own Tempo", func(_ *BattleState, mine *Pokemon) {
			mine.Ability = "own-tempo"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, mine := orbBattle(t)
			tc.setup(s, mine)
			intimidated(t, s, mine)

			if mine.Stages.Atk != 0 {
				t.Errorf("fixture: %s should have refused the drop, Atk is %+d", tc.name, mine.Stages.Atk)
			}
			if mine.Stages.Spe != 1 {
				t.Errorf("a refused drop still arms the orb, got Spe %+d", mine.Stages.Spe)
			}
		})
	}
}

// TestAdrenalineOrbDoesNotFireAtTheAttackFloor. Nothing was refused — there was
// simply nothing left to take — and canon reads the difference off boost.atk
// being present and zero.
func TestAdrenalineOrbDoesNotFireAtTheAttackFloor(t *testing.T) {
	s, mine := orbBattle(t)
	mine.Stages.Atk = -6
	intimidated(t, s, mine)

	if mine.Stages.Spe != 0 {
		t.Errorf("a drop with nothing left to take must not arm the orb, got Spe %+d", mine.Stages.Spe)
	}
	if mine.Item != ItemAdrenalineOrb {
		t.Error("and the orb should still be held")
	}
}

// TestAdrenalineOrbFiresAtMinusFive, the row either side of the floor: the drop
// still has one stage to take, so it lands and the orb fires.
func TestAdrenalineOrbFiresAtMinusFive(t *testing.T) {
	s, mine := orbBattle(t)
	mine.Stages.Atk = -5
	intimidated(t, s, mine)

	if mine.Stages.Spe != 1 {
		t.Errorf("at -5 the drop still lands and the orb fires, got Spe %+d", mine.Stages.Spe)
	}
}

// TestAdrenalineOrbDoesNotFireAtMaxSpeed — canon's first refusal, checked ahead
// of the Attack question.
func TestAdrenalineOrbDoesNotFireAtMaxSpeed(t *testing.T) {
	s, mine := orbBattle(t)
	mine.Stages.Spe = 6
	intimidated(t, s, mine)

	if mine.Item != ItemAdrenalineOrb {
		t.Error("there is no Speed left to gain; the orb should be unspent")
	}
}

// TestAdrenalineOrbDoesNotFireThroughASubstitute. A doll is not a guard: canon's
// Intimidate never reaches the boost at all, so onAfterBoost never runs.
func TestAdrenalineOrbDoesNotFireThroughASubstitute(t *testing.T) {
	s, mine := orbBattle(t)
	mine.Volatiles.Substitute = &SubstituteState{HP: 60, MaxHP: 60}
	intimidated(t, s, mine)

	if mine.Stages.Spe != 0 || mine.Item != ItemAdrenalineOrb {
		t.Errorf("a Substitute stops Intimidate before the boost: Spe %+d, item %q",
			mine.Stages.Spe, mine.Item)
	}
}

// TestOwnTempoRefusesOnlyIntimidate, and not an ordinary Attack drop — which is
// why it is its own flag rather than a BlocksStatLowerByFoe entry.
func TestOwnTempoRefusesOnlyIntimidate(t *testing.T) {
	d := loadDex(t)
	s, mine := orbBattle(t)
	mine.Ability = "own-tempo"
	mine.Item = ItemNone
	s.Active(1).Ability = AbilityNone
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "growl")
	}
	seedAbilitySuppression(s)

	playTurn(d, s, 0, 0)
	if mine.Stages.Atk != -1 {
		t.Errorf("Own Tempo refuses Intimidate, not Growl: Atk is %+d", mine.Stages.Atk)
	}
}
