package engine

import "testing"

// gravity_ban_test.go covers the half of Gravity that was missing entirely:
// the ban on the airborne moves. The accuracy boost and the grounding were both
// implemented; the move ban had nothing to read, because data-sync dropped the
// `gravity` flag before it ever reached the engine.

// TestGravityRefusesAnAirborneMove: the resolve-time gate, which is the
// authoritative one. It has to be the authoritative one because the AI calls
// the dex-less LegalActions, which cannot run the selection filter — a fix that
// lived only in the menu would leave every AI-driven side flying under Gravity.
func TestGravityRefusesAnAirborneMove(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{142}, "B", []int{143}, 1)
	atk := s.Active(0)
	atk.Moves = []MoveSlot{{MoveID: "fly", PP: 15, MaxPP: 15}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.PseudoWeather.Gravity = &PWTimer{TurnsLeft: 5}
	ppBefore := atk.Moves[0].PP

	log := playTurn(d, s, 0, 0)
	if !logHas(log, "because of gravity") {
		t.Errorf("Fly under Gravity should be refused, got %v", logTexts(log))
	}
	if atk.Volatiles.Charging != nil {
		t.Error("a refused move must not start its charge turn")
	}
	// Canon runs onBeforeMove ahead of deductPP, so the refusal is free.
	if atk.Moves[0].PP != ppBefore {
		t.Errorf("a move refused before it is used should cost no PP: %d -> %d",
			ppBefore, atk.Moves[0].PP)
	}
}

// TestGravityLeavesGroundedMovesAlone: the ban keys off the move's own flag and
// nothing else. A predicate that guessed from category, power or type would
// take Tackle down with Fly.
func TestGravityLeavesGroundedMovesAlone(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{142}, "B", []int{143}, 1)
	s.Active(0).Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}}
	// Not Splash for the idler: Splash carries the gravity flag upstream and is
	// banned too, which is exactly the sort of thing a hand-written list would
	// have got wrong and reading the flag gets right for free.
	s.Active(1).Moves = []MoveSlot{{MoveID: "growl", PP: 40, MaxPP: 40}}
	s.PseudoWeather.Gravity = &PWTimer{TurnsLeft: 5}

	log := playTurn(d, s, 0, 0)
	if logHas(log, "because of gravity") {
		t.Errorf("Tackle carries no gravity flag and should be unaffected, got %v", logTexts(log))
	}
}

// TestGravityBansSplash: the ban list is not the obvious one, and this is the
// entry that proves the rule is read from the data rather than guessed. Splash
// is a 0-power status move that does nothing at all — and upstream flags it,
// because the Pokémon jumps.
func TestGravityBansSplash(t *testing.T) {
	d := loadDex(t)
	if !d.Moves["splash"].HasFlag("gravity") {
		t.Error("Splash should carry the gravity flag through data-sync")
	}
	for _, id := range []string{"fly", "bounce", "high-jump-kick", "jump-kick", "magnet-rise", "telekinesis"} {
		if !d.Moves[id].HasFlag("gravity") {
			t.Errorf("%s should carry the gravity flag", id)
		}
	}
	if d.Moves["dig"].HasFlag("gravity") {
		t.Error("Dig charges underground and must not carry the flag")
	}
}

// TestGravityDropsWhatIsAlreadyInTheAir: canon's onFieldStart strips the
// fly/bounce charge, Magnet Rise and Telekinesis from every active as the field
// goes up. Without it a Pokémon that committed to Fly the turn before would
// finish the move Gravity is meant to have banned.
func TestGravityDropsWhatIsAlreadyInTheAir(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{142}, "B", []int{143}, 1)
	atk := s.Active(0)
	atk.Moves = []MoveSlot{{MoveID: "fly", PP: 15, MaxPP: 15}}
	atk.Volatiles.Charging = &ChargingState{MoveIdx: 0}
	atk.Volatiles.MagnetRise = &MagnetRiseState{TurnsLeft: 5}
	foe := s.Active(1)
	foe.Volatiles.Telekinesis = &TelekinesisState{TurnsLeft: 3}

	var log []LogLine
	applyGravitySetter(s, 0, &log)

	if atk.Volatiles.Charging != nil {
		t.Error("the mid-air charge should have been canceled")
	}
	if atk.Volatiles.MagnetRise != nil {
		t.Error("Magnet Rise should have been canceled")
	}
	if foe.Volatiles.Telekinesis != nil {
		t.Error("Telekinesis should have been canceled")
	}
	if !logHas(log, "fell from the sky") {
		t.Errorf("the knock-down should be announced, got %v", logTexts(log))
	}
}

// TestGravityKeepsAGroundedChargeGoing: Dig and Solar Beam charge with their
// feet on the ground and upstream does not touch them, so the cancellation must
// go through airborneChargeMoves rather than clearing every Charging volatile
// it finds.
func TestGravityKeepsAGroundedChargeGoing(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{142}, "B", []int{143}, 1)
	atk := s.Active(0)
	atk.Moves = []MoveSlot{{MoveID: "dig", PP: 10, MaxPP: 10}}
	atk.Volatiles.Charging = &ChargingState{MoveIdx: 0}

	var log []LogLine
	applyGravitySetter(s, 0, &log)
	if atk.Volatiles.Charging == nil {
		t.Error("Dig charges underground and should survive Gravity")
	}
}

// TestGravityKeepsAirborneMovesOffTheMenu: the usability half. A Pokémon whose
// whole moveset is banned falls through to Struggle, which is what canon
// produces too.
func TestGravityKeepsAirborneMovesOffTheMenu(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{142}, "B", []int{143}, 1)
	act := s.Active(0)
	act.Moves = []MoveSlot{
		{MoveID: "fly", PP: 15, MaxPP: 15},
		{MoveID: "tackle", PP: 35, MaxPP: 35},
	}
	s.PseudoWeather.Gravity = &PWTimer{TurnsLeft: 5}

	for _, a := range LegalActionsDex(d, s, 0) {
		if a.Kind == ActionMove && a.Index == 0 {
			t.Error("Fly should not be offered while Gravity is up")
		}
	}
	act.Moves = act.Moves[:1] // nothing but Fly
	got := LegalActionsDex(d, s, 0)
	moves := 0
	for _, a := range got {
		if a.Kind == ActionMove {
			moves++
		}
	}
	if moves != 1 {
		t.Fatalf("an all-banned moveset should fall through to Struggle, got %d move actions", moves)
	}
}
