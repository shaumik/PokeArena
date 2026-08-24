package engine

import "testing"

// status_type_immunity_test.go pins the rule that decides whether the type
// chart can refuse a status move — and, just as importantly, the much larger
// set of status moves it must not refuse.

// TestThunderWaveIsRefusedByGround: the one move in the game that opts back
// into type immunity. Upstream spells it `ignoreImmunity: false`, and it is the
// only status move in this dataset without the ignore-immunity flag.
func TestThunderWaveIsRefusedByGround(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{28}, 1) // Sandslash, Ground
	s.Active(0).Moves = []MoveSlot{{MoveID: "thunder-wave", PP: 20, MaxPP: 20}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "growl", PP: 40, MaxPP: 40}}

	log := playTurn(d, s, 0, 0)
	if got := s.Active(1).Status; got != StatusNone {
		t.Errorf("a Ground type should be immune to Thunder Wave, got %q", got)
	}
	if !logHas(log, "doesn't affect") {
		t.Errorf("the immunity should be announced, got %v", logTexts(log))
	}
}

// TestMostStatusMovesIgnoreTypeImmunity is the control, and it is the reason
// the gate reads a flag instead of the type chart. The intuitive fix — "status
// moves should respect type immunity" — is backwards: canon resolves
// Move#ignoreImmunity to true for any status move that doesn't say otherwise,
// so Glare paralyzes a Ghost and Sand Attack blinds a Levitate holder.
func TestMostStatusMovesIgnoreTypeImmunity(t *testing.T) {
	d := loadDex(t)
	// Sand Attack is Ground; Weezing floats, and a groundedness-based refusal
	// would wrongly catch it.
	run := func(moveID string, defender int) []LogLine {
		s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{defender}, 1)
		s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 20, MaxPP: 20}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "growl", PP: 40, MaxPP: 40}}
		return playTurn(d, s, 0, 0)
	}
	cases := []struct {
		move     string
		defender int
		why      string
	}{
		{"sand-attack", 110, "Ground-type move into a Levitate holder (Weezing)"},
		{"sand-attack", 6, "Ground-type move into a Flying-type (Charizard)"},
		{"confuse-ray", 143, "Ghost-type move into a Normal-type (Snorlax)"},
		{"gastro-acid", 82, "Poison-type move into a Steel-type (Magneton)"},
	}
	for _, c := range cases {
		if log := run(c.move, c.defender); logHas(log, "doesn't affect") {
			t.Errorf("%s should land — status moves ignore type immunity by default; got %v",
				c.why, logTexts(log))
		}
	}
}

// TestThunderWaveStillRefusedByTheAbilitiesThatEatElectric: the gate goes
// through typeEffectiveness rather than a raw chart lookup precisely so the
// ability overrides come along. Volt Absorb keys on the move's type, not on its
// category, so it answers Thunder Wave in canon.
func TestThunderWaveStillRefusedByTheAbilitiesThatEatElectric(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{143}, 1)
	s.Active(0).Moves = []MoveSlot{{MoveID: "thunder-wave", PP: 20, MaxPP: 20}}
	def := s.Active(1)
	def.Moves = []MoveSlot{{MoveID: "growl", PP: 40, MaxPP: 40}}
	def.Ability = "volt-absorb"

	playTurn(d, s, 0, 0)
	if got := def.Status; got != StatusNone {
		t.Errorf("Volt Absorb should refuse Thunder Wave, got %q", got)
	}
}

// TestStatusConditionImmunitiesAreADifferentAxis: an Electric-type is not
// immune to a *Poison*-type status move; it is immune to the paralysis a move
// might try to inflict. Keeping the two straight is why inflictStatus still
// owns the second one.
func TestStatusConditionImmunitiesAreADifferentAxis(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{143}, "B", []int{26}, 1) // Raichu, Electric
	s.Active(0).Moves = []MoveSlot{{MoveID: "toxic", PP: 10, MaxPP: 10}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "growl", PP: 40, MaxPP: 40}}

	log := playTurn(d, s, 0, 0)
	if logHas(log, "doesn't affect") {
		t.Errorf("Toxic is a Poison move and Electric has no type immunity to it; got %v",
			logTexts(log))
	}
	if got := s.Active(1).Status; got != StatusToxic {
		t.Errorf("an Electric-type is immune to paralysis, not to poison, got %q", got)
	}
}
