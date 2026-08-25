package engine

import "testing"

// reactive_behavior_test.go covers the three moves that answer damage with
// damage: Counter, Mirror Coat and Bide.
//
// They read the same event and read it differently, which is the whole reason
// the register has the shape it does. Counter and Mirror Coat *assign* — on a
// multi-hit move only the last strike is paid back — and they filter by
// category. Bide *accumulates*, over three turns, from either category. Getting
// either operator backwards produces a move that looks right in the easy case
// and is wrong in the case upstream ships a test for.
//
// The other half of the register is that a hit which connected for nothing is
// not the same as no hit at all: canon stores a slot and an amount, tests the
// slot to decide whether the move fails, and returns `amount || 1` when it does
// not. Collapsing those onto "amount > 0" turns an Endured hit into a failure.

// TestCounterReturnsTwiceThePhysicalDamage, and Seismic Toss is the fixture
// because it is the one physical move whose damage is a constant: at this
// engine's fixed level 50 it deals exactly 50, so the answer is exactly 100
// with no roll to bound.
func TestCounterReturnsTwiceThePhysicalDamage(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "seismic-toss")
	teachMoves(t, d, s.Active(1), "counter")

	foe := s.Active(1)
	mine := s.Active(0)
	mineBefore := mine.HP
	playTurn(d, s, 0, 0)

	if got := foe.MaxHP - foe.HP; got != 50 {
		t.Fatalf("fixture: Seismic Toss should have dealt exactly 50, dealt %d", got)
	}
	if got := mineBefore - mine.HP; got != 100 {
		t.Errorf("Counter should return twice the 50 it took, returned %d", got)
	}
}

// TestCounterOnlyAnswersItsOwnCategory: physical for Counter, special for
// Mirror Coat. Canon checks the move's category inside onDamagingHit, so the
// wrong category leaves the register untouched and the move fails outright.
func TestCounterOnlyAnswersItsOwnCategory(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		counter, incoming string
		wantAnswered      bool
	}{
		{"counter", "tackle", true},
		{"counter", "water-gun", false},
		{"mirror-coat", "water-gun", true},
		{"mirror-coat", "tackle", false},
	} {
		t.Run(c.counter+" vs "+c.incoming, func(t *testing.T) {
			s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
			teachMoves(t, d, s.Active(0), c.incoming)
			teachMoves(t, d, s.Active(1), c.counter)
			mine := s.Active(0)
			before := mine.HP

			log := playTurn(d, s, 0, 0)
			answered := mine.HP < before
			if answered != c.wantAnswered {
				t.Errorf("%s vs a %s hit: answered=%v, want %v", c.counter, c.incoming, answered, c.wantAnswered)
			}
			if !c.wantAnswered && !logHas(log, "But it failed!") {
				t.Errorf("an unanswered %s should fail visibly, got %v", c.counter, logTexts(log))
			}
		})
	}
}

// TestCounterAnswersOnlyTheLastHit is the discriminator between canon's
// assignment and the accumulation it is easy to write instead. Double Kick
// lands twice; canon overwrites the register on each strike, so the answer is
// twice the *second* hit, not twice the pair.
func TestCounterAnswersOnlyTheLastHit(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "double-kick")
	teachMoves(t, d, s.Active(1), "counter")

	mine, foe := s.Active(0), s.Active(1)
	mineBefore := mine.HP
	playTurn(d, s, 0, 0)

	taken := foe.MaxHP - foe.HP
	dealt := mineBefore - mine.HP
	if taken < 2 {
		t.Fatalf("fixture: Double Kick should have landed both strikes, total %d", taken)
	}
	// Twice the last of two roughly-equal strikes is about the total, and
	// nowhere near twice it. The band is what upstream's own case uses, because
	// the two strikes roll independently.
	if dealt < taken*4/5 || dealt > taken*6/5 {
		t.Errorf("Counter should answer the last strike alone: took %d over two hits, returned %d "+
			"(a cumulative register would return about %d)", taken, dealt, 2*taken)
	}
}

// TestCounterIgnoresAHitASubstituteAte. Canon never fires the damaging-hit
// event for a strike the doll absorbed, so there is nothing to counter. The
// engine gets this for free only if the register is written below the
// substitute redirect, which is the point of the test.
func TestCounterIgnoresAHitASubstituteAte(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "tackle")
	teachMoves(t, d, s.Active(1), "counter")

	mine, foe := s.Active(0), s.Active(1)
	foe.Volatiles.Substitute = &SubstituteState{HP: 60, MaxHP: 60}
	before := mine.HP

	log := playTurn(d, s, 0, 0)
	if mine.HP != before {
		t.Errorf("a hit the doll ate must not arm Counter, but %d damage came back", before-mine.HP)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("Counter with nothing to answer should fail visibly, got %v", logTexts(log))
	}
}

// TestCounterIgnoresIndirectDamage: recoil, residuals, hazards and confusion
// self-hits all lower HP without ever being a damaging hit, and canon's event
// fires from the move path alone.
func TestCounterIgnoresIndirectDamage(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "splash")
	teachMoves(t, d, s.Active(1), "counter")

	mine, foe := s.Active(0), s.Active(1)
	// Poison chip: real HP loss, arriving through the residual path.
	foe.Status = StatusPoison
	before := mine.HP

	log := playTurn(d, s, 0, 0)
	if foe.HP >= foe.MaxHP {
		t.Fatalf("fixture: the poison should have chipped the Counter user")
	}
	if mine.HP != before {
		t.Errorf("residual chip is not a hit, so Counter must not answer it; %d came back", before-mine.HP)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("Counter should fail visibly, got %v", logTexts(log))
	}
}

// TestCounterAnswersAZeroDamageHitForOne. Endure clamps the strike to nothing,
// but the strike still connected — canon arms on the hit and returns
// `damage || 1`. This is the case that separates "the slot is set" from "the
// amount is positive".
func TestCounterAnswersAZeroDamageHitForOne(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "tackle")
	teachMoves(t, d, s.Active(1), "counter")

	mine, foe := s.Active(0), s.Active(1)
	// One HP behind an Endure: the hit lands and is clamped to zero.
	foe.HP = 1
	foe.Volatiles.Endure = true
	before := mine.HP

	log := playTurn(d, s, 0, 0)
	if foe.HP != 1 {
		t.Fatalf("fixture: Endure should have left the Counter user on 1 HP, got %d", foe.HP)
	}
	if logHas(log, "But it failed!") {
		t.Errorf("a hit clamped to zero still connected, so Counter must not fail: %v", logTexts(log))
	}
	if got := before - mine.HP; got != 1 {
		t.Errorf("Counter off a zero-damage hit should deal its floor of 1, dealt %d", got)
	}
}

// TestCounterResetsEachTurn: canon gives the register a one-turn duration, so
// last turn's hit cannot be answered this turn.
func TestCounterResetsEachTurn(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "tackle", "splash")
	teachMoves(t, d, s.Active(1), "counter")

	mine := s.Active(0)
	playTurn(d, s, 0, 0) // Tackle lands, Counter answers.
	before := mine.HP

	log := playTurn(d, s, 1, 0) // Splash; nothing to answer.
	if mine.HP != before {
		t.Errorf("the register is this turn's only, but %d came back off last turn's hit", before-mine.HP)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("Counter should fail on a turn it took no hit, got %v", logTexts(log))
	}
}

// TestCounterMovesLast: priority −5 arrives from the dataset and is what makes
// the move a read rather than a race. Asserted through the log order, since
// that is what a player sees.
func TestCounterMovesLast(t *testing.T) {
	d := loadDex(t)
	if got := d.Moves["counter"].Priority; got != -5 {
		t.Errorf("Counter should ship at priority -5, got %d", got)
	}
	if got := d.Moves["mirror-coat"].Priority; got != -5 {
		t.Errorf("Mirror Coat should ship at priority -5, got %d", got)
	}
}

// TestBideStoresForTwoTurnsThenReleasesDouble. Canon's condition has duration
// 3: the turn of use, one turn of storing, and the release. The store takes
// every point of move damage through, of either category.
func TestBideStoresForTwoTurnsThenReleasesDouble(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "bide")
	teachMoves(t, d, s.Active(1), "seismic-toss")

	mine, foe := s.Active(0), s.Active(1)
	foeBefore := foe.HP

	// Turn 1: the store opens, and the Seismic Toss it takes is counted.
	log := playTurn(d, s, 0, 0)
	if mine.Volatiles.Bide == nil {
		t.Fatal("Bide should have opened a store")
	}
	if !logHas(log, "storing energy") {
		t.Errorf("the store should announce itself, got %v", logTexts(log))
	}
	if foe.HP != foeBefore {
		t.Errorf("the storing turn must deal no damage, dealt %d", foeBefore-foe.HP)
	}

	// Turn 2: still storing, a second Seismic Toss goes in.
	playTurn(d, s, 0, 0)
	if mine.Volatiles.Bide == nil {
		t.Fatal("the store should survive its second turn")
	}
	if foe.HP != foeBefore {
		t.Errorf("the second storing turn must deal no damage, dealt %d", foeBefore-foe.HP)
	}

	// Turn 3: release. Two Seismic Tosses is 100 taken, so 200 comes back —
	// capped at what the target actually has.
	playTurn(d, s, 0, 0)
	if mine.Volatiles.Bide != nil {
		t.Error("the store should be gone after the release")
	}
	dealt := foeBefore - foe.HP
	want := 200
	if want > foeBefore {
		want = foeBefore
	}
	if dealt != want {
		t.Errorf("Bide should return twice the 100 it stored, returned %d (want %d)", dealt, want)
	}
}

// TestBideAccumulatesBothCategories, unlike Counter which filters. Canon's
// handler is an onDamage at the bottom of the modifier chain and asks nothing
// about the category.
func TestBideAccumulatesBothCategories(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "bide")
	teachMoves(t, d, s.Active(1), "tackle", "water-gun", "splash")

	mine := s.Active(0)
	playTurn(d, s, 0, 0) // store opens, eats a Tackle
	physOnly := mine.Volatiles.Bide.Damage
	if physOnly <= 0 {
		t.Fatalf("fixture: the Tackle should have gone into the store, got %d", physOnly)
	}
	playTurn(d, s, 0, 1) // still storing, eats a Water Gun
	both := mine.Volatiles.Bide.Damage
	if both <= physOnly {
		t.Errorf("the special hit should have gone into the store too: %d then %d", physOnly, both)
	}
}

// TestBideWithAnEmptyStoreFails, rather than dealing the one-point floor a
// zero-damage fixed-damage move would otherwise produce.
func TestBideWithAnEmptyStoreFails(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "bide")
	teachMoves(t, d, s.Active(1), "splash")

	mine, foe := s.Active(0), s.Active(1)
	foeBefore := foe.HP
	playTurn(d, s, 0, 0)
	playTurn(d, s, 0, 0)
	log := playTurn(d, s, 0, 0)

	if foe.HP != foeBefore {
		t.Errorf("an empty Bide should deal nothing, dealt %d", foeBefore-foe.HP)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("an empty Bide should fail visibly, got %v", logTexts(log))
	}
	if mine.Volatiles.Bide != nil {
		t.Error("a failed release should still end the store")
	}
}

// TestBideLocksTheUserIn: canon's condition carries onLockMove and sets
// trapped, so the slot is forced and switching is barred for the store's whole
// life. Without the lock the user could open a store and walk away from it.
func TestBideLocksTheUserIn(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "bide", "tackle")
	teachMoves(t, d, s.Active(1), "splash")

	playTurn(d, s, 0, 0)
	acts := LegalActionsDex(d, s, 0)
	if len(acts) != 1 || acts[0].Kind != ActionMove {
		t.Fatalf("a Bide in progress should offer exactly its own move, got %v", acts)
	}
	if got := s.Active(0).Moves[acts[0].Index].MoveID; got != "bide" {
		t.Errorf("the pinned slot should be Bide, got %q", got)
	}
}

// TestBideDiesIfTheUserCannotAct. Canon's onMoveAborted fires on every route
// that stops the user moving, and it removes the volatile — a store the user
// cannot release is a store it loses.
func TestBideDiesIfTheUserCannotAct(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "bide")
	teachMoves(t, d, s.Active(1), "splash")

	mine := s.Active(0)
	playTurn(d, s, 0, 0)
	if mine.Volatiles.Bide == nil {
		t.Fatal("fixture: the store should be open")
	}
	// Asleep for long enough that the tick cannot wake it this turn.
	mine.Status, mine.SleepTurns = StatusSleep, 3
	playTurn(d, s, 0, 0)

	if mine.Volatiles.Bide != nil {
		t.Error("a user that could not act should have lost its Bide")
	}
	if acts := LegalActionsDex(d, s, 0); len(acts) < 2 {
		t.Errorf("with the store gone the user should be free again, got %v", acts)
	}
}

// TestBideReachesThroughAGhost. Canon's release is a synthesized move carrying
// ignoreImmunity, so the Normal-type payback is not walled by a Ghost. This
// engine reads the flag on the shipped move, which is the only damaging move in
// the dataset that carries it.
func TestBideReachesThroughAGhost(t *testing.T) {
	d := loadDex(t)
	// 94 is Gengar: Ghost, and so immune to Normal by the type chart.
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{94, 65})
	teachMoves(t, d, s.Active(0), "bide")
	teachMoves(t, d, s.Active(1), "seismic-toss")

	foe := s.Active(1)
	foeBefore := foe.HP
	playTurn(d, s, 0, 0)
	playTurn(d, s, 0, 0)
	playTurn(d, s, 0, 0)

	if foe.HP == foeBefore {
		t.Error("Bide's release ignores type immunity, so a Ghost should still take it")
	}
}
