package engine

import "testing"

// skydrop_behavior_test.go covers the two-turn move that takes its target with
// it.
//
// The hold is the mechanic: for one turn the target cannot act and cannot
// leave, neither party can be dragged out, and the whole thing has to come
// apart cleanly however the turn ends — including when the carrier faints, and
// including when the Pokemon it took up is not the one below it any more.
//
// What is deliberately not modeled is the untargetability. In singles that
// costs almost nothing, because the two Pokemon out of reach are the only two
// on the field and both are already committed; what it does cost is the terrain
// residuals, and those ledger rows say so.

// TestSkyDropHoldsItsTargetForATurn: the lift deals nothing, and the target's
// turn is gone.
func TestSkyDropHoldsItsTargetForATurn(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{142, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "sky-drop")
	teachMoves(t, d, s.Active(1), "tackle")

	mine, foe := s.Active(0), s.Active(1)
	log := playTurn(d, s, 0, 0)
	if foe.HP != foe.MaxHP {
		t.Errorf("the lift should deal nothing, dealt %d", foe.MaxHP-foe.HP)
	}
	if mine.HP != mine.MaxHP {
		t.Errorf("a held target cannot attack, but the carrier took %d", mine.MaxHP-mine.HP)
	}
	if !logHas(log, "into the sky") {
		t.Errorf("the lift should announce itself, got %v", logTexts(log))
	}
	if !logHas(log, "can't move while it is in the air") {
		t.Errorf("the held target's lost turn should be visible, got %v", logTexts(log))
	}
	if mine.Volatiles.SkyDrop == nil {
		t.Error("the carrier should be holding something")
	}
}

// TestSkyDropReleasesAndHitsOnTheSecondTurn.
func TestSkyDropReleasesAndHitsOnTheSecondTurn(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{142, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "sky-drop")
	teachMoves(t, d, s.Active(1), "tackle")

	mine, foe := s.Active(0), s.Active(1)
	playTurn(d, s, 0, 0)
	log := playTurn(d, s, 0, 0)

	if foe.HP >= foe.MaxHP {
		t.Error("the drop should have dealt damage")
	}
	if !logHas(log, "released from the sky") {
		t.Errorf("the release should announce itself, got %v", logTexts(log))
	}
	if mine.Volatiles.SkyDrop != nil {
		t.Error("the hold should be gone once the drop resolves")
	}
	// And the target has its turns back.
	if acts := LegalActionsDex(d, s, 1); len(acts) < 2 {
		t.Errorf("the released target should be free again, got %v", acts)
	}
}

// TestSkyDropBarsTheTargetFromSwitching, and bars it harder than an ordinary
// trap does: canon's hold sits below Shed Shell in priority, so the boots do
// not answer it.
func TestSkyDropBarsTheTargetFromSwitching(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{142, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "sky-drop")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "tackle")
	}
	s.Active(1).Item = ItemKind("shed-shell")

	playTurn(d, s, 0, 0)
	for _, a := range LegalActionsDex(d, s, 1) {
		if a.Kind == ActionSwitch {
			t.Fatalf("a held target should have no switch to make, got %v", LegalActionsDex(d, s, 1))
		}
	}
}

// TestSkyDropRefusesTheDrag from either end. Canon protects both parties, so
// neither a phazing move nor anything else that moves a Pokemon can separate
// them mid-flight.
func TestSkyDropRefusesTheDrag(t *testing.T) {
	d := loadDex(t)
	for _, victim := range []int{0, 1} {
		s := neutralBattle(t, d, 11, []int{142, 65}, []int{143, 65})
		teachMoves(t, d, s.Active(0), "sky-drop")
		for i := range s.Sides[1].Team {
			teachMoves(t, d, &s.Sides[1].Team[i], "roar")
		}
		playTurn(d, s, 0, 0)

		var log []LogLine
		before := s.Sides[victim].Active
		applyForceSwitch(s, 1-victim, NewRNG(3), &log)
		if s.Sides[victim].Active != before {
			t.Errorf("dragging side %d out of a Sky Drop should be refused, but the active moved", victim)
		}
	}
}

// TestSkyDropPicksUpAFlyingTypeAndDropsItForNothing. Canon does not refuse the
// lift — the target goes up like anything else — and then the drop does zero,
// announced as an immunity rather than a failure.
func TestSkyDropPicksUpAFlyingTypeAndDropsItForNothing(t *testing.T) {
	d := loadDex(t)
	// 149 is Dragonite: Dragon/Flying.
	s := neutralBattle(t, d, 11, []int{142, 65}, []int{149, 65})
	teachMoves(t, d, s.Active(0), "sky-drop")
	teachMoves(t, d, s.Active(1), "tackle")

	mine, foe := s.Active(0), s.Active(1)
	playTurn(d, s, 0, 0)
	if mine.Volatiles.SkyDrop == nil {
		t.Fatal("a Flying-type target should still be picked up")
	}
	if mine.HP != mine.MaxHP {
		t.Errorf("it should have lost its turn on the way up, but the carrier took %d", mine.MaxHP-mine.HP)
	}

	log := playTurn(d, s, 0, 0)
	if foe.HP != foe.MaxHP {
		t.Errorf("a Flying-type takes nothing from the drop, took %d", foe.MaxHP-foe.HP)
	}
	if !logHas(log, "doesn't affect") {
		t.Errorf("the zero should read as an immunity, got %v", logTexts(log))
	}
}

// TestSkyDropRefusesASubstitute outright rather than eating it: canon returns a
// hard failure from onTryHit, so nothing is lifted.
func TestSkyDropRefusesASubstitute(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{142, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "sky-drop")
	teachMoves(t, d, s.Active(1), "tackle")

	mine, foe := s.Active(0), s.Active(1)
	foe.Volatiles.Substitute = &SubstituteState{HP: 40, MaxHP: 40}

	log := playTurn(d, s, 0, 0)
	if mine.Volatiles.SkyDrop != nil {
		t.Error("a doll should have refused the lift")
	}
	if mine.Volatiles.Charging != nil {
		t.Error("and nothing should be charging either")
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("the refusal should be visible, got %v", logTexts(log))
	}
	if mine.HP == mine.MaxHP {
		t.Error("with no lift, the target keeps its turn and should have attacked")
	}
}

// TestSkyDropIsRefusedByProtect on the way up, which is what makes the shield
// worth pressing against it: the target keeps its next turn too.
func TestSkyDropIsRefusedByProtect(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{142, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "sky-drop")
	teachMoves(t, d, s.Active(1), "protect", "tackle")

	mine := s.Active(0)
	log := playTurn(d, s, 0, 0)
	if mine.Volatiles.SkyDrop != nil {
		t.Error("a Protect should have refused the lift")
	}
	if !logHas(log, "protected itself") {
		t.Errorf("the shield should be what is announced, got %v", logTexts(log))
	}
}

// TestSkyDropDoesNotClaimToHaveDroppedACorpse. Canon re-checks at the top of
// the drop that what it took up is still the thing below it, and fails plainly
// when it is not.
func TestSkyDropDoesNotClaimToHaveDroppedACorpse(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{142, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "sky-drop")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "tackle")
	}

	playTurn(d, s, 0, 0)
	// The Pokemon in the air dies to something else and is replaced.
	s.Active(1).HP = 0
	faint(s.Active(1), 1, &[]LogLine{})
	s.Sides[1].Active = 1

	mine := s.Active(0)
	log := playTurn(d, s, 0, 0)
	if !logHas(log, "But it failed!") {
		t.Errorf("with nobody up there the drop should fail plainly, got %v", logTexts(log))
	}
	if logHas(log, "released from the sky") {
		t.Errorf("it should not claim to have dropped anything, got %v", logTexts(log))
	}
	if mine.Volatiles.SkyDrop != nil {
		t.Error("a failed drop still has to end the hold")
	}
	if s.Active(1).HP != s.Active(1).MaxHP {
		t.Error("the replacement was never picked up and should be untouched")
	}
}

// TestSkyDropFreesItsTargetWhenTheCarrierLeaves. The hold lives on the carrier,
// so the ordinary volatile wipe is the whole of the cleanup — which is the
// argument for putting it there rather than on the Pokemon in the air.
func TestSkyDropFreesItsTargetWhenTheCarrierLeaves(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{142, 65}, []int{143, 65})
	for i := range s.Sides[0].Team {
		teachMoves(t, d, &s.Sides[0].Team[i], "sky-drop")
	}
	teachMoves(t, d, s.Active(1), "tackle")

	playTurn(d, s, 0, 0)
	if !heldBySkyDrop(s, 1) {
		t.Fatal("fixture: the target should be held")
	}
	// The carrier faints outright; nothing else touches the victim.
	faint(s.Active(0), 0, &[]LogLine{})
	if heldBySkyDrop(s, 1) {
		t.Error("a carrier that is gone is holding nothing")
	}
	if acts := LegalActionsDex(d, s, 1); len(acts) < 2 {
		t.Errorf("the freed target should have its options back, got %v", acts)
	}
}

// TestSkyDropIsRefusedByGravity, which it gets from the flag alone — the same
// flag that grounds Fly and Bounce, and one Sky Drop was already named in the
// transform's comment for carrying.
func TestSkyDropIsRefusedByGravity(t *testing.T) {
	d := loadDex(t)
	if !d.Moves["sky-drop"].HasFlag("gravity") {
		t.Fatal("Sky Drop should ship carrying the flag Gravity reads")
	}
	s := neutralBattle(t, d, 11, []int{142, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "sky-drop")
	teachMoves(t, d, s.Active(1), "tackle")
	s.PseudoWeather.Gravity = &PWTimer{TurnsLeft: 5}

	mine := s.Active(0)
	log := playTurn(d, s, 0, 0)
	if mine.Volatiles.SkyDrop != nil {
		t.Error("Gravity should have refused the lift")
	}
	if !logHas(log, "because of gravity") {
		t.Errorf("the refusal should name gravity, got %v", logTexts(log))
	}
}
