package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// futuresight_behavior_test.go covers the one move in this dataset whose damage
// arrives two turns after the move does.
//
// The delay is the easy half. The hard half is that the attacker may not be
// there any more, and canon has three separate answers for what that means: the
// hit still happens, it is computed from the attacker's *natural* numbers with
// its item and ability suppressed, and the item hook it re-fires belongs to the
// attacker rather than to whoever is standing in its place. Each of those is a
// way to be plausibly wrong.

// futureSightBattle builds a mirror with the caster on side 0.
func futureSightBattle(t *testing.T, d *domain.Dex, seed uint64) *BattleState {
	t.Helper()
	s := neutralBattle(t, d, seed, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "future-sight", "splash")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "splash")
	}
	return s
}

// TestFutureSightLandsTwoTurnsLater, and not before. Canon's clock puts the hit
// at the end of the second turn after the cast, so three end-of-turn ticks pass
// between the announcement and the damage.
func TestFutureSightLandsTwoTurnsLater(t *testing.T) {
	d := loadDex(t)
	s := futureSightBattle(t, d, 11)
	foe := s.Active(1)

	log := playTurn(d, s, 0, 0)
	if !logHas(log, "foresaw an attack") {
		t.Errorf("the cast should announce itself, got %v", logTexts(log))
	}
	if foe.HP != foe.MaxHP {
		t.Fatalf("the cast turn must deal nothing, dealt %d", foe.MaxHP-foe.HP)
	}

	playTurn(d, s, 1, 0)
	if foe.HP != foe.MaxHP {
		t.Fatalf("the turn after the cast must deal nothing either, dealt %d", foe.MaxHP-foe.HP)
	}

	log = playTurn(d, s, 1, 0)
	if foe.HP >= foe.MaxHP {
		t.Error("the hit should have landed on the second turn after the cast")
	}
	if !logHas(log, "took the Future Sight attack") {
		t.Errorf("the landing should announce itself, got %v", logTexts(log))
	}
}

// TestFutureSightNeverMisses. Canon installs the pending hit from an onTry that
// short-circuits above every hit step, so the cast rolls no accuracy at all —
// which is why maximum evasion on the target changes nothing.
func TestFutureSightNeverMisses(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(1); seed <= 8; seed++ {
		s := futureSightBattle(t, d, seed)
		s.Active(1).Stages.Eva = 6

		log := playTurn(d, s, 0, 0)
		if logHas(log, "attack missed") {
			t.Fatalf("seed %d: the cast rolls no accuracy and cannot miss: %v", seed, logTexts(log))
		}
		if s.Sides[1].SlotConditions.FutureMove == nil {
			t.Fatalf("seed %d: the hit should be pending on the target's slot", seed)
		}
	}
}

// TestFutureSightFailsWhileOneIsPendingOnTheSlot, which is canon's own refusal:
// the condition has no restart handler, so a second install on an occupied slot
// simply does not take.
func TestFutureSightFailsWhileOneIsPendingOnTheSlot(t *testing.T) {
	d := loadDex(t)
	s := futureSightBattle(t, d, 11)

	playTurn(d, s, 0, 0)
	log := playTurn(d, s, 0, 0)
	if !logHas(log, "But it failed!") {
		t.Errorf("a second Future Sight at the same slot should fail, got %v", logTexts(log))
	}
}

// TestFutureSightDoesNotArmStompingTantrum. The cast reports canon's NOT_FAIL,
// which is a success and not a failure — so the move the user makes on the
// following turn must not find a failed move behind it.
func TestFutureSightDoesNotArmStompingTantrum(t *testing.T) {
	d := loadDex(t)
	s := futureSightBattle(t, d, 11)
	mine := s.Active(0)

	playTurn(d, s, 0, 0)
	if mine.Volatiles.MoveThisTurnFailed {
		t.Error("a Future Sight cast is a success; it must not record a failed move")
	}
	if mine.Volatiles.MoveLastTurnFailed {
		t.Error("nor should it shift one into last turn's record")
	}
}

// TestFutureSightHitsWhoeverIsStandingThere. The pending hit is keyed to the
// slot and not to the Pokemon, so a target that pivots away hands it to its
// replacement.
func TestFutureSightHitsWhoeverIsStandingThere(t *testing.T) {
	d := loadDex(t)
	s := futureSightBattle(t, d, 11)

	playTurn(d, s, 0, 0)
	ResolveTurn(d, s, [2]Action{moveAt(1), switchTo(1)})
	playTurn(d, s, 1, 0)

	original, replacement := &s.Sides[1].Team[0], &s.Sides[1].Team[1]
	if original.HP != original.MaxHP {
		t.Errorf("the Pokemon that left should be untouched, took %d", original.MaxHP-original.HP)
	}
	if replacement.HP >= replacement.MaxHP {
		t.Error("the replacement should have taken the hit aimed at the slot")
	}
}

// TestFutureSightExpiresOnAnEmptySlot rather than waiting for one. Canon's
// residual is exempt from the "skip a fainted holder" rule precisely so the
// clock keeps running; a hit that never expired would also block every later
// Future Sight at that slot.
func TestFutureSightExpiresOnAnEmptySlot(t *testing.T) {
	d := loadDex(t)
	s := futureSightBattle(t, d, 11)

	playTurn(d, s, 0, 0)
	playTurn(d, s, 1, 0)
	// Kill the occupant just before the hit is due; the replace phase does not
	// run until after the residuals, so the corpse is still the active.
	s.Active(1).HP = 0
	s.Active(1).Fainted = true
	playTurn(d, s, 1, 0)

	if s.Sides[1].SlotConditions.FutureMove != nil {
		t.Error("the pending hit should have expired rather than waiting for an occupant")
	}
}

// TestFutureSightUsesTheCastersNaturalNumbersWhenItIsGone. Canon suppresses a
// benched Pokemon's item and ability outright, and its stat stages left with it
// — so a Nasty Plot banked before switching out buys nothing.
func TestFutureSightUsesTheCastersNaturalNumbersWhenItIsGone(t *testing.T) {
	d := loadDex(t)

	hit := func(switchOut bool) int {
		s := futureSightBattle(t, d, 11)
		for i := range s.Sides[0].Team {
			teachMoves(t, d, &s.Sides[0].Team[i], "future-sight", "splash")
		}
		s.Active(0).Stages.SpA = 6
		playTurn(d, s, 0, 0)
		if switchOut {
			ResolveTurn(d, s, [2]Action{switchTo(1), moveAt(0)})
		} else {
			playTurn(d, s, 1, 0)
		}
		foe := s.Active(1)
		before := foe.HP
		playTurn(d, s, 1, 0)
		return before - foe.HP
	}

	onField, benched := hit(false), hit(true)
	if onField <= 0 || benched <= 0 {
		t.Fatalf("fixture: both hits should have landed, got %d and %d", onField, benched)
	}
	if benched >= onField {
		t.Errorf("a caster that left takes its +6 Sp. Atk with it: on-field %d, benched %d",
			onField, benched)
	}
}

// TestFutureSightRespectsTheTargetsScreens even when the caster is gone: the
// hit is computed against the board as it stands at landing time, not against a
// snapshot taken at cast time.
func TestFutureSightRespectsTheTargetsScreens(t *testing.T) {
	d := loadDex(t)

	hit := func(screen bool) int {
		s := futureSightBattle(t, d, 11)
		if screen {
			s.Sides[1].Conditions.LightScreen = &ScreenState{TurnsLeft: 8}
		}
		playTurn(d, s, 0, 0)
		playTurn(d, s, 1, 0)
		foe := s.Active(1)
		before := foe.HP
		playTurn(d, s, 1, 0)
		return before - foe.HP
	}

	plain, screened := hit(false), hit(true)
	if screened >= plain {
		t.Errorf("Light Screen should damp the special hit: %d without, %d with", plain, screened)
	}
}

// TestFutureSightIsTypedAtLandingTime. The shipped move carries
// `ignore-immunity`, derived from a flag whose only upstream job is to stop the
// *cast* being refused by the type chart; the hit canon synthesizes carries the
// opposite value. Leaving the flag on the landing hit would make the attack
// untyped and unstoppable, so the test asks for the type chart's opinion.
//
// It asks for a resistance rather than an immunity because this dex has no
// Dark-type species, and Dark is the only thing Psychic cannot touch. The
// upstream case that wants the immunity is skipped for that reason.
func TestFutureSightIsTypedAtLandingTime(t *testing.T) {
	d := loadDex(t)
	// 65 is Alakazam: Psychic, which resists Psychic.
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{65, 143})
	teachMoves(t, d, s.Active(0), "future-sight", "splash")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "splash")
	}

	playTurn(d, s, 0, 0)
	playTurn(d, s, 1, 0)
	log := playTurn(d, s, 1, 0)

	if !logHas(log, "not very effective") {
		t.Errorf("the landing hit is typed and should be resisted by a Psychic target, got %v", logTexts(log))
	}
}

// TestFutureSightIsAbsorbedByASubstitute. It carries no bypass-sub flag, so the
// doll stands between it and the holder like any other attack.
func TestFutureSightIsAbsorbedByASubstitute(t *testing.T) {
	d := loadDex(t)
	s := futureSightBattle(t, d, 11)

	playTurn(d, s, 0, 0)
	playTurn(d, s, 1, 0)
	foe := s.Active(1)
	foe.Volatiles.Substitute = &SubstituteState{HP: 60, MaxHP: 60}
	before := foe.HP
	playTurn(d, s, 1, 0)

	if foe.HP != before {
		t.Errorf("the doll should have taken it, but the holder lost %d", before-foe.HP)
	}
}

// TestFutureSightIgnoresAProtectPutUpToMeetIt. Canon strips Protect and Endure
// from the target before the hit resolves rather than consulting them, which is
// why a shield raised on the landing turn is no help.
func TestFutureSightIgnoresAProtectPutUpToMeetIt(t *testing.T) {
	d := loadDex(t)
	s := futureSightBattle(t, d, 11)
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "protect")
	}

	playTurn(d, s, 0, 0)
	playTurn(d, s, 1, 0)
	foe := s.Active(1)
	before := foe.HP
	playTurn(d, s, 1, 0)

	if foe.HP >= before {
		t.Error("a Protect on the landing turn should not stop a Future Sight")
	}
}
