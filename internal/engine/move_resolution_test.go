package engine

import "testing"

// The executeMove wave: effects placed at the wrong point in the move's own
// resolution. Most of these are one call moved past one early return, and every
// one of them was invisible from inside the engine because the placement read as
// deliberate — three of them had a comment arguing for it.

// TestFlingSpendsItsItemThroughAProtect. Canon runs Try / PrepareHit above the
// moveSteps loop, and Protect lives inside that loop, so the item is thrown and
// lost even when the throw is fully absorbed. Having it the other way round made
// Fling a shield-check: press Protect and the Iron Ball stays on the belt.
func TestFlingSpendsItsItemThroughAProtect(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 3, []int{143}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "fling")
	teachMoves(t, d, &s.Sides[1].Team[0], "protect")
	s.Active(0).Item = ItemIronBall

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHas(log, "protected itself") {
		t.Fatalf("setup: Protect should have absorbed the throw; log %v", logTexts(log))
	}
	if s.Active(0).Item != ItemNone {
		t.Errorf("the flung item should be gone even though the throw was absorbed, got %q",
			s.Active(0).Item)
	}
}

// TestFocusPunchKeepsItsPPWhenItLosesFocus. Canon's beforeMoveCallback runs
// above deductPP, so a Focus Punch that never went off costs nothing. The check
// used to sit below choosePP, and its own comment conceded "PP is already
// spent".
func TestFocusPunchKeepsItsPPWhenItLosesFocus(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 5, []int{143}, []int{135}) // Jolteon outspeeds
	teachMoves(t, d, &s.Sides[0].Team[0], "focus-punch")
	teachMoves(t, d, &s.Sides[1].Team[0], "thunderbolt")
	before := s.Active(0).Moves[0].PP

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHas(log, "lost its focus") {
		t.Fatalf("setup: the Thunderbolt should have broken the focus; log %v", logTexts(log))
	}
	if got := s.Active(0).Moves[0].PP; got != before {
		t.Errorf("a Focus Punch that lost focus should keep its PP: %d -> %d", before, got)
	}
}

// TestFakeOutCannotFlinchABracedFocusPuncher: canon's focuspunch condition
// refuses the flinch volatile outright, which is what makes a Fake Out lead fail
// against it. The refusal lives on the volatile, so every source of a flinch is
// covered and not just the one move upstream's case names.
func TestFakeOutCannotFlinchABracedFocusPuncher(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 7, []int{143}, []int{135}) // Jolteon is faster
	teachMoves(t, d, &s.Sides[0].Team[0], "focus-punch")
	teachMoves(t, d, &s.Sides[1].Team[0], "fake-out")

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if logHas(log, "flinched") {
		t.Errorf("a Pokemon that announced Focus Punch cannot be flinched; log %v",
			logTexts(log))
	}
	// The volatile must not leak into the next turn either.
	if s.Active(0).Volatiles.FocusPunch {
		t.Errorf("the Focus Punch brace should be cleared at end of turn")
	}
}

// TestChargeIsSpentByAnyElectricMoveAndNothingElse. Both halves of this were
// inverted: the clear lived at the tail of the damaging-move path and fired
// regardless of type, so Air Slash ate the charge, and it sat below the
// status-move early return, so Thunder Wave could never spend it. The comment in
// damage.go stated the Gen 8 rule as if it were current.
func TestChargeIsSpentByAnyElectricMoveAndNothingElse(t *testing.T) {
	d := loadDex(t)
	spend := func(move string) bool {
		s := neutralBattle(t, d, 9, []int{135}, []int{143}) // Jolteon
		teachMoves(t, d, &s.Sides[0].Team[0], "charge", move)
		teachMoves(t, d, &s.Sides[1].Team[0], "splash")
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		if !s.Active(0).Volatiles.Charge {
			t.Fatalf("setup: Charge did not arm")
		}
		ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})
		return !s.Active(0).Volatiles.Charge
	}
	if !spend("thunderbolt") {
		t.Errorf("an Electric attack should spend the charge")
	}
	if !spend("thunder-wave") {
		t.Errorf("an Electric *status* move should spend the charge — Gen 9 keys on the " +
			"move's type, not its category")
	}
	if spend("body-slam") {
		t.Errorf("a non-Electric attack should not spend the charge")
	}
	if spend("agility") {
		t.Errorf("a non-Electric status move should not spend the charge")
	}
}

// TestDestinyBondFailsBackToBack: the guard existed and was unreachable, because
// the end-of-turn sweep cleared the volatile before the next turn could refuse
// it. Canon's onPrepareHit is one line that both removes the bond and fails the
// move, so a second use costs the user its threat.
func TestDestinyBondFailsBackToBack(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{94}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "destiny-bond")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")

	log1 := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHas(log1, "trying to take its foe down") {
		t.Fatalf("setup: the first Destiny Bond should arm; log %v", logTexts(log1))
	}
	log2 := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !logHas(log2, "But it failed") {
		t.Errorf("a second Destiny Bond in a row should fail; log %v", logTexts(log2))
	}
	if s.Active(0).Volatiles.DestinyBond {
		t.Errorf("the failed use should have taken the bond down with it")
	}
}

// TestSturdyLosesPrecedenceToEndure: both save the Pokemon, and canon announces
// Endure. Sturdy used to clamp at the end of computeDamage, upstream of
// dealDamage's whole survival chain, so Endure never saw a lethal figure.
func TestSturdyLosesPrecedenceToEndure(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 13, []int{112}, []int{95}) // Rhydon vs Onix
	teachMoves(t, d, &s.Sides[0].Team[0], "earthquake")
	teachMoves(t, d, &s.Sides[1].Team[0], "endure")
	s.Active(1).Ability = AbilitySturdy
	s.Active(0).Stats.Atk = 999

	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if s.Active(1).Fainted {
		t.Fatalf("setup: the target should have survived at 1 HP")
	}
	if !logHas(log, "endured the hit") {
		t.Errorf("Endure clamps first, so Endure is what announces; log %v", logTexts(log))
	}
	if logHas(log, "hung on with Sturdy") {
		t.Errorf("Sturdy should not announce a save Endure already made; log %v",
			logTexts(log))
	}
}

// TestStruggleCostsAQuarterOfMaxHPAndRockHeadCannotStopIt. Struggle's recoil rode
// the shared self-effect block, which takes its fraction of the damage *dealt*
// and is gated on Rock Head. Since Gen 4 it is a quarter of the user's maximum HP
// whatever it dealt, and canon exempts it from Rock Head specifically.
func TestStruggleCostsAQuarterOfMaxHPAndRockHeadCannotStopIt(t *testing.T) {
	d := loadDex(t)
	struggle := func(ability AbilityKind, defDex int) int {
		s := neutralBattle(t, d, 15, []int{143}, []int{defDex})
		s.Active(0).Ability = ability
		s.Active(0).Moves = []MoveSlot{{MoveID: "body-slam", PP: 0, MaxPP: 15}}
		teachMoves(t, d, &s.Sides[1].Team[0], "splash")
		s.Active(1).HP = s.Active(1).MaxHP
		before := s.Active(0).HP
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		return before - s.Active(0).HP
	}
	me := buildPokemon(d, d.Species[143])
	want := me.MaxHP / 4

	// Against a body that resists it hard and one that does not: the cost is the
	// same either way, because it is a fraction of the *user's* max HP.
	soft := struggle(AbilityNone, 143) // Snorlax, neutral
	rock := struggle(AbilityNone, 95)  // Onix, Rock/Ground — soaks it
	for label, got := range map[string]int{"neutral target": soft, "resisting target": rock} {
		if got < want-1 || got > want+1 {
			t.Errorf("Struggle against a %s cost %d, want ~%d (a quarter of the user's max HP)",
				label, got, want)
		}
	}
	if got := struggle("rock-head", 143); got < want-1 {
		t.Errorf("Rock Head cost %d — it does not block Struggle's recoil, which is not "+
			"recoil in the sense the ability cares about", got)
	}
}

// TestAMutualWipeIsDecidedByFaintOrder. Gen 5+ settles a simultaneous wipe by
// faint order rather than calling it a draw, and the order comes from the
// residual phase's Speed walk — so a Perish Song mirror is a win for the
// *slower* side. See docs/battle-state.md for what that changes downstream.
func TestAMutualWipeIsDecidedByFaintOrder(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 21, []int{135}, []int{143}) // Jolteon (fast) vs Snorlax
	teachMoves(t, d, &s.Sides[0].Team[0], "perish-song", "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")

	for i := 0; i < 5 && !s.Ended(); i++ {
		move := 1
		if i == 0 {
			move = 0
		}
		ResolveTurn(d, s, [2]Action{moveAt(move), moveAt(0)})
	}
	if !s.Ended() {
		t.Fatalf("the song should have ended the battle by now; phase %s", s.Phase)
	}
	if s.LiveCount(0) != 0 || s.LiveCount(1) != 0 {
		t.Fatalf("both sides should be wiped; got %d-%d", s.LiveCount(0), s.LiveCount(1))
	}
	if s.Winner != 1 {
		t.Errorf("the faster Pokemon falls first, so the slower side wins: winner = %d, want 1",
			s.Winner)
	}
}
