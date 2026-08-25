package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// lockon_behavior_test.go covers Lock-On and Mind Reader — one mechanic
// upstream ships under two names, with identical accuracy, PP, refusal and
// volatile, differing only in what they say and in a Z-move this engine does
// not model.
//
// The two things worth pinning are the two that read backwards. The volatile
// goes on the *user*, not on the thing it aims at, so the aim leaves when the
// aimer does. And it beats the accuracy roll rather than improving it, so it
// answers evasion, Bright Powder and an OHKO move's 30% alike — but it does not
// answer an immunity, because an immunity was never an accuracy problem.

// lockOnBattle builds two Snorlax with the given moves and a foe that will
// dodge everything it can.
func lockOnBattle(t *testing.T, d *domain.Dex, seed uint64, mine ...string) *BattleState {
	t.Helper()
	s := neutralBattle(t, d, seed, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), mine...)
	teachMoves(t, d, s.Active(1), "splash")
	return s
}

// TestLockOnBeatsEvasion. Maximum evasion takes a 100%-accurate move down to
// roughly a third; an aim taken the turn before overwrites the number outright,
// so the follow-up lands on every seed.
func TestLockOnBeatsEvasion(t *testing.T) {
	d := loadDex(t)
	for _, move := range []string{"lock-on", "mind-reader"} {
		t.Run(move, func(t *testing.T) {
			for seed := uint64(1); seed <= 12; seed++ {
				s := lockOnBattle(t, d, seed, move, "tackle")
				foe := s.Active(1)
				foe.Stages.Eva = 6

				playTurn(d, s, 0, 0)
				if s.Active(0).Volatiles.LockOn == nil {
					t.Fatalf("seed %d: %s should have taken aim", seed, move)
				}
				before := foe.HP
				log := playTurn(d, s, 1, 0)
				if foe.HP >= before {
					t.Fatalf("seed %d: a locked-on Tackle should land through +6 evasion, log %v", seed, logTexts(log))
				}
			}
		})
	}
}

// TestLockOnDoesNotBeatAnImmunity. The auto-hit block sits below the two
// refusals in resolveAccuracy on purpose: canon's immunity step runs above its
// accuracy step, so an aim buys nothing against a target the move cannot touch.
func TestLockOnDoesNotBeatAnImmunity(t *testing.T) {
	d := loadDex(t)
	// 94 is Gengar, the dex's only Ghost, and so untouchable by Normal.
	s := neutralBattle(t, d, 7, []int{143, 65}, []int{94, 65})
	teachMoves(t, d, s.Active(0), "lock-on", "tackle")
	teachMoves(t, d, s.Active(1), "splash")

	foe := s.Active(1)
	playTurn(d, s, 0, 0)
	before := foe.HP
	log := playTurn(d, s, 1, 0)

	if foe.HP != before {
		t.Errorf("an aim is not a way past a type immunity, but %d damage landed", before-foe.HP)
	}
	if !logHas(log, "doesn't affect") {
		t.Errorf("the immunity should still be what is announced, got %v", logTexts(log))
	}
}

// TestLockOnFailsWhenAlreadyAimed, and it fails on the *user's* own volatile —
// so a Mind Reader after a Lock-On fails too. They are one condition upstream.
func TestLockOnFailsWhenAlreadyAimed(t *testing.T) {
	d := loadDex(t)
	s := lockOnBattle(t, d, 7, "lock-on", "mind-reader")

	playTurn(d, s, 0, 0)
	log := playTurn(d, s, 1, 0)
	if !logHas(log, "But it failed!") {
		t.Errorf("Mind Reader on top of a live Lock-On should fail, got %v", logTexts(log))
	}
}

// TestLockOnLifecycle. Canon's duration is 2 and it ticks at the end of every
// turn, so the aim is taken on turn N, covers the move on turn N+1, and is gone
// once turn N+1's residuals have run. Both ends are asserted because getting
// either wrong gives a move that is either useless or permanent.
func TestLockOnLifecycle(t *testing.T) {
	d := loadDex(t)
	s := lockOnBattle(t, d, 7, "lock-on", "tackle")
	mine := s.Active(0)

	playTurn(d, s, 0, 0)
	lo := mine.Volatiles.LockOn
	if lo == nil {
		t.Fatal("the aim should be up after the move that took it")
	}
	if lo.TurnsLeft != 1 {
		t.Errorf("one end-of-turn tick should have run, leaving 1; got %d", lo.TurnsLeft)
	}

	playTurn(d, s, 1, 0)
	if mine.Volatiles.LockOn != nil {
		t.Error("the aim covers one move and then lapses; it should be gone")
	}
}

// TestLockOnIsNotSpentByTheMoveItHelps. The two ways the volatile can end look
// the same from outside — after a locked-on attack it is gone either way — so
// the fixture stretches the duration to tell them apart: if the move spent it
// there would be nothing left, and if only the clock touched it exactly one
// turn comes off.
//
// The distinction is the difference between this move and Laser Focus next
// door, which executeMove clears explicitly on every attempt.
func TestLockOnIsNotSpentByTheMoveItHelps(t *testing.T) {
	d := loadDex(t)
	s := lockOnBattle(t, d, 7, "lock-on", "tackle")
	mine := s.Active(0)

	playTurn(d, s, 0, 0)
	if mine.Volatiles.LockOn == nil {
		t.Fatal("fixture: the aim should be up")
	}
	mine.Volatiles.LockOn.TurnsLeft = 3

	playTurn(d, s, 1, 0)
	lo := mine.Volatiles.LockOn
	if lo == nil {
		t.Fatal("Lock-On has a duration, not a charge — attacking must not consume it")
	}
	if lo.TurnsLeft != 2 {
		t.Errorf("only the end-of-turn tick should have moved the clock: 3 -> %d, want 2", lo.TurnsLeft)
	}
}

// TestLockOnAimsAtAPokemonAndNotASlot. A foe that pivots out takes the lock
// with it: what came in was never aimed at.
func TestLockOnAimsAtAPokemonAndNotASlot(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 3, []int{143, 65}, []int{143, 65})
	teachMoves(t, d, s.Active(0), "lock-on", "tackle")
	for i := range s.Sides[1].Team {
		teachMoves(t, d, &s.Sides[1].Team[i], "splash")
	}

	playTurn(d, s, 0, 0)
	if s.Active(0).Volatiles.LockOn == nil {
		t.Fatal("fixture: the aim should be up")
	}
	// The foe pivots; the replacement is a different Pokemon.
	ResolveTurn(d, s, [2]Action{moveAt(1), switchTo(1)})

	if lockedOn(s, s.Active(0), s.Active(1)) {
		t.Error("the aim was at the Pokemon that left, so its replacement should be missable")
	}
}

// TestLockOnLeavesWithItsUser. The volatile is on the aimer, so the ordinary
// switch-out wipe ends it — which is the whole reason canon puts it there.
func TestLockOnLeavesWithItsUser(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 3, []int{143, 65}, []int{143, 65})
	for i := range s.Sides[0].Team {
		teachMoves(t, d, &s.Sides[0].Team[i], "lock-on", "tackle")
	}
	teachMoves(t, d, s.Active(1), "splash")

	playTurn(d, s, 0, 0)
	ResolveTurn(d, s, [2]Action{switchTo(1), moveAt(0)})
	if s.Active(0).Volatiles.LockOn != nil {
		t.Error("the aim belongs to the aimer and should not have survived the switch")
	}
	// And it does not come back with it.
	ResolveTurn(d, s, [2]Action{switchTo(0), moveAt(0)})
	if s.Active(0).Volatiles.LockOn != nil {
		t.Error("a returning aimer should not still be aiming")
	}
}
