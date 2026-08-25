package engine

import (
	"strings"
	"testing"

	"pokearena/internal/domain"
)

// calledmoves_behavior_test.go covers the moves that resolve as some other
// move — Sleep Talk, Copycat, Metronome, Mirror Move and Me First — plus the
// two that were filed with them and are not calls at all: Snore, which is only
// "usable while asleep", and Mimic, which rewrites a slot.
//
// The shape they share is a substitution, and the two things that separates it
// from a real call are both pinned below: the called move costs no PP, and the
// *caller* remains what the user last used, so a Disable landed on a Sleep Talk
// user names Sleep Talk rather than whatever it rolled.

func calledBattle(t *testing.T, d *domain.Dex, seed uint64, mine ...string) *BattleState {
	t.Helper()
	s := neutralBattle(t, d, seed, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), mine...)
	teachMoves(t, d, s.Active(1), "splash")
	return s
}

// TestSnoreNeedsTheUserAsleep. The flag lets the move be selected while asleep;
// the refusal is a separate rule on the same move, and without it Snore is an
// unconditional 50-power sound attack on 78 of the 80 species.
func TestSnoreNeedsTheUserAsleep(t *testing.T) {
	d := loadDex(t)
	for _, asleep := range []bool{false, true} {
		s := calledBattle(t, d, 11, "snore")
		mine, foe := s.Active(0), s.Active(1)
		if asleep {
			mine.Status, mine.SleepTurns = StatusSleep, 3
		}
		before := foe.HP

		log := playTurn(d, s, 0, 0)
		hit := foe.HP < before
		if hit != asleep {
			t.Errorf("asleep=%v: Snore connected=%v, want %v (log %v)", asleep, hit, asleep, logTexts(log))
		}
		if !asleep && !logHas(log, "But it failed!") {
			t.Errorf("an awake Snore should fail visibly, got %v", logTexts(log))
		}
	}
}

// TestSleepTalkNeedsTheUserAsleep, by the same rule and the same flag.
func TestSleepTalkNeedsTheUserAsleep(t *testing.T) {
	d := loadDex(t)
	s := calledBattle(t, d, 11, "sleep-talk", "tackle")
	foe := s.Active(1)
	before := foe.HP

	log := playTurn(d, s, 0, 0)
	if foe.HP != before {
		t.Errorf("an awake Sleep Talk should call nothing, but %d damage landed", before-foe.HP)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("it should fail visibly, got %v", logTexts(log))
	}
}

// TestSleepTalkCallsOneOfTheUsersOtherMoves, and never itself: Sleep Talk
// carries the flag that is its own skip list.
func TestSleepTalkCallsOneOfTheUsersOtherMoves(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(1); seed <= 10; seed++ {
		s := calledBattle(t, d, seed, "sleep-talk", "tackle")
		mine, foe := s.Active(0), s.Active(1)
		mine.Status, mine.SleepTurns = StatusSleep, 5
		before := foe.HP

		log := playTurn(d, s, 0, 0)
		if foe.HP >= before {
			t.Fatalf("seed %d: Sleep Talk should have called the Tackle, log %v", seed, logTexts(log))
		}
		if !logHas(log, "used Sleep Talk") {
			t.Errorf("seed %d: the caller should announce itself too, got %v", seed, logTexts(log))
		}
	}
}

// TestACalledMoveCostsNoPP: the caller pays, the callee does not. That is what
// makes a call a call rather than a second action.
func TestACalledMoveCostsNoPP(t *testing.T) {
	d := loadDex(t)
	s := calledBattle(t, d, 11, "sleep-talk", "tackle")
	mine := s.Active(0)
	mine.Status, mine.SleepTurns = StatusSleep, 5
	callerPP, calleePP := mine.Moves[0].PP, mine.Moves[1].PP

	playTurn(d, s, 0, 0)
	if mine.Moves[0].PP != callerPP-1 {
		t.Errorf("the caller should have paid one PP: %d -> %d", callerPP, mine.Moves[0].PP)
	}
	if mine.Moves[1].PP != calleePP {
		t.Errorf("the called move should have paid nothing: %d -> %d", calleePP, mine.Moves[1].PP)
	}
}

// TestTheCallerStaysTheUsersLastMove. Canon writes that register from the
// outside of the call and never from the inside, which is why a Disable aimed
// at a Sleep Talk user disables Sleep Talk.
func TestTheCallerStaysTheUsersLastMove(t *testing.T) {
	d := loadDex(t)
	// Alakazam (65) outspeeds Snorlax (143), so the foe's Splash resolves first
	// and the Sleep Talk is genuinely the last thing the battle saw.
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{65, 143})
	teachMoves(t, d, s.Active(0), "sleep-talk", "tackle")
	teachMoves(t, d, s.Active(1), "splash")
	mine := s.Active(0)
	mine.Status, mine.SleepTurns = StatusSleep, 5

	playTurn(d, s, 0, 0)
	if mine.Volatiles.LastMoveID != "sleep-talk" {
		t.Errorf("the user's last move should be the caller, got %q", mine.Volatiles.LastMoveID)
	}
	// And the battle's own register keeps the other answer, which is what
	// Copycat repeats.
	if s.LastMoveUsedID != "tackle" {
		t.Errorf("the battle's last move should be the called one, got %q", s.LastMoveUsedID)
	}
}

// TestSleepTalkedMoveStillMeetsGravity. The gate that refuses the airborne
// moves runs against the slot the controller picked, so the substituted move
// needs its own check — canon carries both an onBeforeMove and an onModifyMove
// for exactly this.
func TestSleepTalkedMoveStillMeetsGravity(t *testing.T) {
	d := loadDex(t)
	s := calledBattle(t, d, 11, "sleep-talk", "fly")
	mine, foe := s.Active(0), s.Active(1)
	mine.Status, mine.SleepTurns = StatusSleep, 5
	s.PseudoWeather.Gravity = &PWTimer{TurnsLeft: 5}
	before := foe.HP

	log := playTurn(d, s, 0, 0)
	if foe.HP != before || mine.Volatiles.Charging != nil {
		t.Errorf("Gravity should have refused the called Fly, got %v", logTexts(log))
	}
	if !logHas(log, "because of gravity") {
		t.Errorf("the refusal should name gravity, got %v", logTexts(log))
	}
}

// TestSleepTalkedTrumpCardReadsTheCallersPP. Canon computes Trump Card's power
// from `move.sourceEffect || move.id`, so a Trump Card a Sleep Talk rolled
// measures Sleep Talk's slot — the only slot that paid. This is the rule the
// ported case was reaching for and could not build, because upstream gets its
// permanently-asleep user from Comatose.
func TestSleepTalkedTrumpCardReadsTheCallersPP(t *testing.T) {
	d := loadDex(t)

	power := func(callerPP int) int {
		s := calledBattle(t, d, 11, "sleep-talk", "trump-card")
		mine, foe := s.Active(0), s.Active(1)
		mine.Status, mine.SleepTurns = StatusSleep, 5
		// Trump Card's own slot is left full, so any reading of *its* PP gives
		// the same answer in both runs and only the caller's can move.
		mine.Moves[0].PP = callerPP
		before := foe.HP
		playTurn(d, s, 0, 0)
		return before - foe.HP
	}

	roomy, last := power(5), power(1)
	if roomy <= 0 || last <= 0 {
		t.Fatalf("fixture: both calls should have connected, got %d and %d", roomy, last)
	}
	if last <= roomy {
		t.Errorf("a Sleep Talk down to its last PP should make the Trump Card it calls hit far harder: "+
			"%d with PP to spare, %d on the last", roomy, last)
	}
}

// TestCopycatRepeatsWhatTheBattleLastSaw, which includes a move that some other
// caller called. The register is the battle's rather than any Pokemon's.
func TestCopycatRepeatsWhatTheBattleLastSaw(t *testing.T) {
	d := loadDex(t)
	// The copier is the faster body on both turns, so nothing lands between the
	// move it means to copy and the copy — the register really is the *last*
	// move anyone used, and a Splash in between would be what it repeated.
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{65, 143})
	teachMoves(t, d, s.Active(0), "swords-dance", "splash")
	teachMoves(t, d, s.Active(1), "splash", "copycat")

	mine, foe := s.Active(0), s.Active(1)
	playTurn(d, s, 0, 0) // side 0 sets up, after the foe's Splash
	if mine.Stages.Atk == 0 {
		t.Fatal("fixture: the Swords Dance should have landed")
	}
	playTurn(d, s, 1, 1) // side 1 copies it, moving first
	if foe.Stages.Atk != 2 {
		t.Errorf("Copycat should have repeated the Swords Dance, got %+d", foe.Stages.Atk)
	}
}

// TestCopycatFailsOnAMoveThatRefusesToBeCopied. The flag is upstream's own
// denylist and it is the whole of the refusal.
func TestCopycatFailsOnAMoveThatRefusesToBeCopied(t *testing.T) {
	d := loadDex(t)
	// Destiny Bond rather than Protect, which carries the same flag but also
	// +4 priority: it would resolve *before* the foe's Splash on the setup turn
	// and the register would hold the Splash instead.
	if !d.Moves["destiny-bond"].HasFlag("fail-copycat") {
		t.Fatal("fixture: Destiny Bond should carry the flag Copycat reads")
	}
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{65, 143})
	teachMoves(t, d, s.Active(0), "destiny-bond", "splash")
	teachMoves(t, d, s.Active(1), "splash", "copycat")

	playTurn(d, s, 0, 0)
	log := playTurn(d, s, 1, 1)
	if !logHas(log, "But it failed!") {
		t.Errorf("Copycat should refuse a move flagged against it, got %v", logTexts(log))
	}
}

// TestMetronomeRollsSomethingAndSaysWhat. The pool is every curated move
// upstream marks reachable, minus the two shapes a called move cannot be, and
// the roll has to actually vary.
func TestMetronomeRollsSomethingAndSaysWhat(t *testing.T) {
	d := loadDex(t)
	seen := map[string]bool{}
	for seed := uint64(1); seed <= 25; seed++ {
		s := calledBattle(t, d, seed, "metronome")
		log := playTurn(d, s, 0, 0)
		var called string
		for _, l := range log {
			if strings.Contains(l.Text, " used ") && !strings.Contains(l.Text, "used Metronome") {
				called = l.Text
			}
		}
		if called == "" {
			t.Fatalf("seed %d: Metronome should have rolled something, got %v", seed, logTexts(log))
		}
		seen[called] = true
	}
	if len(seen) < 5 {
		t.Errorf("25 seeds produced only %d distinct rolls, so the draw is not moving", len(seen))
	}
}

// TestACalledMoveIsNeverATwoTurnOrARampage. Both arm a volatile that points at
// a slot index, and a called move has no slot — so the pool excludes them. The
// divergence is deliberate and this is where it is stated.
func TestACalledMoveIsNeverATwoTurnOrARampage(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(1); seed <= 40; seed++ {
		s := calledBattle(t, d, seed, "metronome")
		mine := s.Active(0)
		playTurn(d, s, 0, 0)
		if mine.Volatiles.Charging != nil {
			t.Fatalf("seed %d: a called move armed a charge pointing at Metronome's own slot", seed)
		}
		if mine.Volatiles.LockedMove != nil {
			t.Fatalf("seed %d: a called move armed a rampage pointing at Metronome's own slot", seed)
		}
	}
}

// TestMirrorMoveReflectsTheTargetsLastMove, and only what carries the flag
// upstream gates it on.
func TestMirrorMoveReflectsTheTargetsLastMove(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "splash", "mirror-move")
	teachMoves(t, d, s.Active(1), "growl")

	mine, foe := s.Active(0), s.Active(1)
	playTurn(d, s, 0, 0) // the foe uses Growl
	if mine.Stages.Atk != -1 {
		t.Fatalf("fixture: the Growl should have landed, got %+d", mine.Stages.Atk)
	}
	playTurn(d, s, 1, 0) // reflect it back
	if foe.Stages.Atk != -1 {
		t.Errorf("Mirror Move should have thrown the Growl back, got %+d", foe.Stages.Atk)
	}
}

// TestMirrorMoveFailsWithNothingToReflect.
func TestMirrorMoveFailsWithNothingToReflect(t *testing.T) {
	d := loadDex(t)
	s := calledBattle(t, d, 11, "mirror-move")
	log := playTurn(d, s, 0, 0)
	if !logHas(log, "But it failed!") {
		t.Errorf("with the target having used nothing, Mirror Move should fail, got %v", logTexts(log))
	}
}

// TestMeFirstPreEmptsTheTargetsQueuedAttack at half again the power, and only
// when the target still has one queued.
func TestMeFirstPreEmptsTheTargetsQueuedAttack(t *testing.T) {
	d := loadDex(t)
	// Alakazam (65) outspeeds Snorlax (143), so side 0 moves first and the
	// target's Tackle is still pending when Me First looks for it.
	s := neutralBattle(t, d, 11, []int{65, 143}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "me-first")
	teachMoves(t, d, s.Active(1), "tackle")

	foe := s.Active(1)
	before := foe.HP
	log := playTurn(d, s, 0, 0)
	if foe.HP >= before {
		t.Errorf("Me First should have thrown the target's own Tackle at it, got %v", logTexts(log))
	}
}

// TestMeFirstFailsAgainstAStatusMove, one of the four refusals foeQueuedAttack
// already answers.
func TestMeFirstFailsAgainstAStatusMove(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{65, 143}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "me-first")
	teachMoves(t, d, s.Active(1), "growl")

	log := playTurn(d, s, 0, 0)
	if !logHas(log, "But it failed!") {
		t.Errorf("there is nothing to pre-empt about a status move, got %v", logTexts(log))
	}
}

// TestMimicOverwritesItsOwnSlot with the target's last move, at that move's own
// full PP.
func TestMimicOverwritesItsOwnSlot(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "splash", "mimic")
	teachMoves(t, d, s.Active(1), "growl")

	mine := s.Active(0)
	playTurn(d, s, 0, 0) // the foe uses Growl
	playTurn(d, s, 1, 0) // copy it into the Mimic slot

	if mine.Moves[1].MoveID != "growl" {
		t.Fatalf("the Mimic slot should now hold Growl, holds %q", mine.Moves[1].MoveID)
	}
	if want := d.Moves["growl"].PP; mine.Moves[1].PP != want || mine.Moves[1].MaxPP != want {
		t.Errorf("the copy should arrive at Growl's own full PP of %d, got %d/%d",
			want, mine.Moves[1].PP, mine.Moves[1].MaxPP)
	}
}

// TestMimicRevertsOnSwitchOut, which is the fourth use of the same memo the
// ability, the stats and the typing already share.
func TestMimicRevertsOnSwitchOut(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	for i := range s.Sides[0].Team {
		teachMoves(t, d, &s.Sides[0].Team[i], "splash", "mimic")
	}
	teachMoves(t, d, s.Active(1), "growl")

	playTurn(d, s, 0, 0)
	playTurn(d, s, 1, 0)
	if s.Active(0).Moves[1].MoveID != "growl" {
		t.Fatal("fixture: the Mimic should have landed")
	}

	ResolveTurn(d, s, [2]Action{switchTo(1), moveAt(0)})
	if got := s.Sides[0].Team[0].Moves[1].MoveID; got != "mimic" {
		t.Errorf("switching out should put Mimic back in its slot, holds %q", got)
	}
	if s.Sides[0].Team[0].BaseMoves != nil {
		t.Error("the memo should be spent once it has been used")
	}
}

// TestMimicRefusesAMoveTheUserAlreadyKnows, one of canon's four refusals.
func TestMimicRefusesAMoveTheUserAlreadyKnows(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "growl", "mimic")
	teachMoves(t, d, s.Active(1), "growl")

	mine := s.Active(0)
	playTurn(d, s, 0, 0)
	log := playTurn(d, s, 1, 0)
	if mine.Moves[1].MoveID != "mimic" {
		t.Errorf("the slot should be untouched, holds %q", mine.Moves[1].MoveID)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("Mimic should refuse a move the user already has, got %v", logTexts(log))
	}
}
