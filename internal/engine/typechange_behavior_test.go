package engine

import "testing"

// typechange_behavior_test.go covers Soak, Reflect Type, Conversion and
// Conversion 2 — the moves that rewrite a Pokémon's typing while it is out.
//
// The mechanic is two lines of assignment; what needs pinning is everything
// around it. That the change is *live*, so the very next attack is scored
// against the new typing and every other rule that reads a type agrees. That it
// is *field-scoped*, so leaving puts the real typing back and the memo is
// first-writer-wins. And that each move's refusal is canon's own refusal rather
// than a tidier one someone preferred — Conversion in particular reads its
// user's first move slot whatever is in it, which is why it fails so often.

// TestSoakMakesTheTargetPureWater, and the target's second type goes with it.
func TestSoakMakesTheTargetPureWater(t *testing.T) {
	d := loadDex(t)
	// 3 is Venusaur: Grass/Poison, so both slots have something to lose.
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{3, 65})
	teachMoves(t, d, s.Active(0), "soak")
	teachMoves(t, d, s.Active(1), "splash")

	foe := s.Active(1)
	playTurn(d, s, 0, 0)
	if foe.Type1 != "water" || foe.Type2 != "" {
		t.Errorf("Soak should leave the target pure Water, got %s/%s", foe.Type1, foe.Type2)
	}
}

// TestSoakFailsOnSomethingAlreadyPureWater, which is canon's own refusal: it
// compares the target's whole current typing against Water, so a Water/Flying
// target is still soakable and a pure Water one is not.
func TestSoakFailsOnSomethingAlreadyPureWater(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		name     string
		foeDex   int
		wantFail bool
	}{
		// 55 is Golduck: pure Water.
		{"pure Water", 55, true},
		// 130 is Gyarados: Water/Flying, so the typing is not just Water.
		{"Water and something", 130, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := neutralBattle(t, d, 11, []int{143, 65}, []int{c.foeDex, 65})
			teachMoves(t, d, s.Active(0), "soak")
			teachMoves(t, d, s.Active(1), "splash")

			log := playTurn(d, s, 0, 0)
			failed := logHas(log, "But it failed!")
			if failed != c.wantFail {
				t.Errorf("Soak on a %s target: failed=%v, want %v (log %v)", c.name, failed, c.wantFail, logTexts(log))
			}
		})
	}
}

// TestSoakIsRefusedByASubstitute. Soak carries no bypass-sub flag upstream, and
// the doll check that serves the declarative moves lives somewhere a shell move
// never reaches — so the handler has to make it itself.
func TestSoakIsRefusedByASubstitute(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{3, 65})
	teachMoves(t, d, s.Active(0), "soak")
	teachMoves(t, d, s.Active(1), "splash")

	foe := s.Active(1)
	foe.Volatiles.Substitute = &SubstituteState{HP: 40, MaxHP: 40}
	log := playTurn(d, s, 0, 0)

	if foe.Type1 != "grass" {
		t.Errorf("a doll should have refused the Soak, but the target is %s/%s", foe.Type1, foe.Type2)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("the refusal should be visible, got %v", logTexts(log))
	}
}

// TestReflectTypeReachesThroughASubstitute is the other half of the pair: this
// one does carry bypass-sub, so the doll is transparent to it.
func TestReflectTypeReachesThroughASubstitute(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{3, 65})
	teachMoves(t, d, s.Active(0), "reflect-type")
	teachMoves(t, d, s.Active(1), "splash")

	user, foe := s.Active(0), s.Active(1)
	foe.Volatiles.Substitute = &SubstituteState{HP: 40, MaxHP: 40}
	playTurn(d, s, 0, 0)

	if user.Type1 != "grass" || user.Type2 != "poison" {
		t.Errorf("Reflect Type should ignore the doll and copy Grass/Poison, got %s/%s", user.Type1, user.Type2)
	}
}

// TestReflectTypeCopiesTheCurrentTyping, not the one the target was built with.
// A foe that has itself been Soaked hands over Water — canon reads getTypes(),
// which is the live answer.
func TestReflectTypeCopiesTheCurrentTyping(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{3, 65})
	teachMoves(t, d, s.Active(0), "soak", "reflect-type")
	teachMoves(t, d, s.Active(1), "splash")

	user, foe := s.Active(0), s.Active(1)
	playTurn(d, s, 0, 0) // Soak: the Venusaur becomes pure Water.
	if foe.Type1 != "water" {
		t.Fatalf("fixture: the Soak should have landed, target is %s/%s", foe.Type1, foe.Type2)
	}
	playTurn(d, s, 1, 0) // Reflect Type: copy what it is now.

	if user.Type1 != "water" || user.Type2 != "" {
		t.Errorf("Reflect Type should have copied the Soaked Water typing, got %s/%s", user.Type1, user.Type2)
	}
}

// TestConversionTakesItsFirstSlotsType, whatever is in that slot. The rule is
// literally moveSlots[0], and a "smarter" version that hunted for a damaging
// move would stop failing in the places canon fails.
func TestConversionTakesItsFirstSlotsType(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{3, 65})
	// Slot 0 is Ember, a Fire move, so the Normal-type user becomes Fire.
	teachMoves(t, d, s.Active(0), "ember", "conversion")
	teachMoves(t, d, s.Active(1), "splash")

	user := s.Active(0)
	playTurn(d, s, 1, 0)
	if user.Type1 != "fire" || user.Type2 != "" {
		t.Errorf("Conversion off an Ember in slot 0 should give pure Fire, got %s/%s", user.Type1, user.Type2)
	}
}

// TestConversionFailsWhenTheUserIsAlreadyThatType — including the very common
// case of Conversion itself sitting in slot 0, since Conversion is Normal-typed
// and so are plenty of its users.
func TestConversionFailsWhenTheUserIsAlreadyThatType(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{3, 65})
	// Snorlax is Normal and Conversion is a Normal move, in slot 0.
	teachMoves(t, d, s.Active(0), "conversion")
	teachMoves(t, d, s.Active(1), "splash")

	user := s.Active(0)
	log := playTurn(d, s, 0, 0)
	if user.Type1 != "normal" {
		t.Errorf("the user's typing should be untouched, got %s/%s", user.Type1, user.Type2)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("Conversion into a type the user already has should fail, got %v", logTexts(log))
	}
}

// TestConversion2PicksSomethingThatResistsTheLastAttack, and picks it from the
// types the user does not already have.
func TestConversion2PicksSomethingThatResistsTheLastAttack(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(1); seed <= 8; seed++ {
		s := neutralBattle(t, d, seed, []int{143, 65}, []int{143, 65})
		teachMoves(t, d, s.Active(0), "splash", "conversion-2")
		teachMoves(t, d, s.Active(1), "ember")

		user := s.Active(0)
		// The Ember has to have happened first. Two mirrored Snorlax tie on
		// Speed, so rather than rig the order the fixture spends a turn: the
		// register outlives the turn it was written in, exactly as canon's
		// lastMoveUsed does.
		playTurn(d, s, 0, 0)
		playTurn(d, s, 1, 0)

		if user.Type1 == "normal" {
			t.Fatalf("seed %d: Conversion 2 should have changed the user's typing", seed)
		}
		if got := d.Multiplier("fire", user.Type1); got > 0.5 {
			t.Errorf("seed %d: picked %s, which takes Fire at %v — it must resist or be immune",
				seed, user.Type1, got)
		}
	}
}

// TestConversion2FailsAfterAStruggle. Canon types Struggle as `???` rather than
// as Normal, so there is no attack to answer. The engine records the last
// move's type separately from its slug for exactly this: Struggle has no dex
// entry and so no slug, and reading the dex would find a static "Normal" and
// give the opposite answer.
func TestConversion2FailsAfterAStruggle(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "conversion-2")
	teachMoves(t, d, s.Active(1), "tackle")

	user, foe := s.Active(0), s.Active(1)
	// Empty the foe's only slot: choosePP falls back to Struggle.
	foe.Moves[0].PP = 0

	log := playTurn(d, s, 0, 0)
	if foe.Volatiles.LastMoveType != "" {
		t.Fatalf("fixture: a Struggle should record a typeless last move, got %q", foe.Volatiles.LastMoveType)
	}
	if user.Type1 != "normal" {
		t.Errorf("Conversion 2 after a typeless attack should change nothing, got %s", user.Type1)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("it should fail visibly, got %v", logTexts(log))
	}
}

// TestConversion2FailsWithNothingToAnswer: the foe has not attacked at all.
func TestConversion2FailsWithNothingToAnswer(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "conversion-2")
	teachMoves(t, d, s.Active(1), "splash")

	user := s.Active(0)
	// Splash is Normal-typed and does get recorded, so the fixture has to be a
	// foe that has genuinely not moved: side 0 is faster here, and the register
	// is read before the foe acts on turn 1 only if it moves second. Clear it
	// outright to state the case plainly.
	s.Active(1).Volatiles.LastMoveType = ""
	log := ResolveTurn(d, s, [2]Action{moveAt(0), switchTo(1)})

	if user.Type1 != "normal" {
		t.Errorf("with no last move to answer, the typing should be untouched, got %s", user.Type1)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("it should fail visibly, got %v", logTexts(log))
	}
}

// TestTypeChangeIsLiveForEverythingThatReadsAType. The point of writing Type1
// and Type2 rather than keeping an override is that every reader picks it up
// with no further work: here, the type chart and STAB both.
func TestTypeChangeIsLiveForEverythingThatReadsAType(t *testing.T) {
	d := loadDex(t)
	// 6 is Charizard: Fire/Flying, and so 4x weak to Rock.
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{6, 65})
	teachMoves(t, d, s.Active(0), "soak", "rock-tomb")
	teachMoves(t, d, s.Active(1), "splash")

	foe := s.Active(1)
	// Baseline: Rock into Fire/Flying.
	before := foe.HP
	playTurn(d, s, 1, 0)
	quadruple := before - foe.HP
	foe.HP = foe.MaxHP

	// Soak it to pure Water, where Rock is neutral, and hit it again.
	playTurn(d, s, 0, 0)
	if foe.Type1 != "water" {
		t.Fatalf("fixture: the Soak should have landed, target is %s/%s", foe.Type1, foe.Type2)
	}
	before = foe.HP
	playTurn(d, s, 1, 0)
	neutral := before - foe.HP

	if neutral >= quadruple {
		t.Errorf("Soaking away Fire/Flying should have taken Rock from 4x to neutral: %d then %d",
			quadruple, neutral)
	}
}

// TestTypeChangeRevertsOnSwitchOut. Canon discards the change when the Pokémon
// leaves, and BaseTypes is the memo that reproduces it.
func TestTypeChangeRevertsOnSwitchOut(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{3, 65})
	teachMoves(t, d, s.Active(0), "soak")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "splash")
	}

	foe := s.Active(1)
	playTurn(d, s, 0, 0)
	if foe.Type1 != "water" {
		t.Fatalf("fixture: the Soak should have landed")
	}

	ResolveTurn(d, s, [2]Action{moveAt(0), switchTo(1)})
	if got := &s.Sides[1].Team[0]; got.Type1 != "grass" || got.Type2 != "poison" {
		t.Errorf("switching out should restore Grass/Poison, got %s/%s", got.Type1, got.Type2)
	}
	if s.Sides[1].Team[0].BaseTypes != nil {
		t.Error("the memo should be spent once it has been used")
	}
}

// TestTypeMemoIsFirstWriterWins: a Pokémon changed twice reverts to what it was
// built with, not to what it wore in between. Same rule as BaseAbility.
func TestTypeMemoIsFirstWriterWins(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{3, 65})
	teachMoves(t, d, s.Active(0), "soak")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "splash")
	}

	foe := s.Active(1)
	playTurn(d, s, 0, 0)
	// A second rewrite, by hand, standing in for a move the dex cannot reach
	// twice in one battle.
	setTypes(foe, 1, "fire", "", &[]LogLine{})
	if foe.BaseTypes == nil || foe.BaseTypes[0] != "grass" || foe.BaseTypes[1] != "poison" {
		t.Fatalf("the memo should still hold the original Grass/Poison, holds %v", foe.BaseTypes)
	}

	ResolveTurn(d, s, [2]Action{moveAt(0), switchTo(1)})
	if got := &s.Sides[1].Team[0]; got.Type1 != "grass" || got.Type2 != "poison" {
		t.Errorf("it should revert past both changes to Grass/Poison, got %s/%s", got.Type1, got.Type2)
	}
}
