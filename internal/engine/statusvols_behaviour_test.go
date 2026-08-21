package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// statusvols_behaviour_test.go plays the status-adjacent volatiles
// (statusvols.go) and the aim volatiles (aim.go) as *whole battles*: every
// assertion here is reached by handing ResolveTurn a move index and reading
// the public battle state and turn log afterwards.
//
// Why a second layer of tests for mechanics that already have unit tests:
// the existing coverage for Attract, Yawn, Curse, Destiny Bond, Focus Energy,
// Foresight and Minimize calls the apply* handlers and tickStatusVols
// directly. That pins the handler but not the wiring — a move whose dataset
// entry loses its `primary.volatile`, a dispatch table that stops routing
// Curse by user type, or an end-of-turn hook that stops calling
// tickStatusVols would leave every one of those unit tests green while the
// mechanic is dead in an actual battle. These tests fail in that case, and
// they are the ones that survive a port to another language, because they
// only ever touch the exported surface.
//
// Probabilistic rules (Attract's 50% immobilize, Focus Energy's crit ratio,
// Minimize's evasion) are pinned by *measuring a rate over many battle
// seeds*, never by picking the one seed that makes a roll land the desired
// way. A seed-picked test pins Go's RNG rather than the game's rule and
// translates to nothing.

// svBattle builds a bare battle with abilities and held items stripped, in
// the spirit of berryBattle: the volatile under test should be the only live
// mechanic on the field. (Gengar's Levitate would eat the Earthquake the
// Destiny Bond tests need; Snorlax's Thick Fat and Immunity have bitten
// fixtures in this package before.)
func svBattle(t *testing.T, d *domain.Dex, seed uint64, team0, team1 []int) *BattleState {
	t.Helper()
	s, err := NewBattle(d, "b", "P1", team0, "P2", team1, seed)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for side := 0; side < 2; side++ {
		for i := range s.Sides[side].Team {
			p := &s.Sides[side].Team[i]
			p.Ability = AbilityNone
			p.Item = ItemNone
		}
	}
	return s
}

// svGive replaces a Pokémon's move list with exactly these slugs at full PP.
// Writing the exported Moves field is fixture arrangement, not a back door —
// the moves are then used through ResolveTurn like any other.
func svGive(t *testing.T, d *domain.Dex, p *Pokemon, ids ...string) {
	t.Helper()
	p.Moves = nil
	for _, id := range ids {
		m, ok := d.Moves[id]
		if !ok {
			t.Fatalf("move %q is missing from the dex", id)
		}
		p.Moves = append(p.Moves, MoveSlot{MoveID: id, PP: m.PP, MaxPP: m.PP})
	}
}

// svTurn resolves one turn in which each side uses the move at the given
// slot index.
func svTurn(d *domain.Dex, s *BattleState, i0, i1 int) []LogLine {
	return ResolveTurn(d, s, [2]Action{
		{Kind: ActionMove, Index: i0},
		{Kind: ActionMove, Index: i1},
	})
}

// svRate replays the same scripted battle under `seeds` different battle
// seeds and returns the fraction of runs where `once` reported true. Seeds
// are walked from 1 upward — no seed is ever chosen for the roll it
// produces.
func svRate(t *testing.T, seeds int, once func(seed uint64) bool) float64 {
	t.Helper()
	hits := 0
	for seed := uint64(1); seed <= uint64(seeds); seed++ {
		if once(seed) {
			hits++
		}
	}
	return float64(hits) / float64(seeds)
}

// svAssertRate fails unless the measured frequency lands inside [lo, hi].
// The windows below are wide enough (3+ sigma at the sample sizes used) that
// an honest engine passes on any RNG, and narrow enough that a rule that is
// off — a 50% roll that became "always" or "never", a 1/24 crit that never
// became 1/2 — falls outside.
func svAssertRate(t *testing.T, label string, seeds int, lo, hi float64, once func(seed uint64) bool) {
	t.Helper()
	got := svRate(t, seeds, once)
	if got < lo || got > hi {
		t.Errorf("%s: measured %.3f over %d battle seeds, want within [%.2f, %.2f]", label, got, seeds, lo, hi)
	}
}

// svAssertNever fails on the first seed where the outcome happens at all.
func svAssertNever(t *testing.T, label string, seeds int, once func(seed uint64) bool) {
	t.Helper()
	for seed := uint64(1); seed <= uint64(seeds); seed++ {
		if once(seed) {
			t.Fatalf("%s: happened on battle seed %d, and must never happen", label, seed)
		}
	}
}

// svAssertAlways is the mirror of svAssertNever.
func svAssertAlways(t *testing.T, label string, seeds int, once func(seed uint64) bool) {
	t.Helper()
	for seed := uint64(1); seed <= uint64(seeds); seed++ {
		if !once(seed) {
			t.Fatalf("%s: failed to happen on battle seed %d, and must always happen", label, seed)
		}
	}
}

// --- Attract (applyAttractVolatile, gendersAttract) ---

// TestAttractInBattleLandsAndImmobilizesAboutHalfTheTurns pins the two halves
// of infatuation that a player actually experiences: using the move puts the
// foe in love, and an infatuated Pokémon then loses about half of its turns.
//
// The rate is measured across 500 battle seeds rather than read off a chosen
// seed. That matters twice over: it is the only honest way to state "50%",
// and it catches the two silent breakages a single-seed test misses — a roll
// stuck on always (the foe is permanently disabled, which would warp every
// match) and a roll stuck on never (Attract becomes a wasted turn).
//
// The turn log and the HP ledger are cross-checked on every seed: whenever
// "immobilized by love" is printed the attacker's Tackle must have dealt no
// damage, and whenever it is not printed the Tackle must have landed. A log
// line that prints without stopping the move is exactly the kind of cosmetic
// pass a log-only assertion would bless.
func TestAttractInBattleLandsAndImmobilizesAboutHalfTheTurns(t *testing.T) {
	d := loadDex(t)

	const seeds = 500
	immobilized := 0
	for seed := uint64(1); seed <= seeds; seed++ {
		s := svBattle(t, d, seed, []int{143}, []int{143})
		user, foe := s.Active(0), s.Active(1)
		user.Gender = domain.GenderMale
		foe.Gender = domain.GenderFemale
		svGive(t, d, user, "attract", "splash")
		svGive(t, d, foe, "tackle")

		// Turn 1: Attract lands. The foe may or may not get its Tackle off
		// this turn — the roll is already live — so the measurement is taken
		// on turn 2, where every battle contributes exactly one roll.
		log1 := svTurn(d, s, 0, 0)
		if !foe.Volatiles.Attract {
			t.Fatalf("seed %d: Attract did not infatuate an opposite-gender foe; log: %v", seed, logTexts(log1))
		}
		if !logHas(log1, "fell in love") {
			t.Fatalf("seed %d: missing the infatuation log; got %v", seed, logTexts(log1))
		}

		hpBefore := user.HP
		log2 := svTurn(d, s, 1, 0) // user Splashes, foe tries to Tackle
		blocked := logHas(log2, "immobilized by love")
		hit := user.HP < hpBefore
		if blocked && hit {
			t.Fatalf("seed %d: the foe was immobilized by love and still dealt damage; log: %v", seed, logTexts(log2))
		}
		if !blocked && !hit {
			t.Fatalf("seed %d: the foe was not immobilized yet its Tackle did nothing; log: %v", seed, logTexts(log2))
		}
		if blocked {
			immobilized++
		}
	}

	rate := float64(immobilized) / float64(seeds)
	if rate < 0.42 || rate > 0.58 {
		t.Errorf("infatuated Pokémon lost %.3f of its turns over %d battle seeds, want ~0.50 (window [0.42, 0.58])", rate, seeds)
	}
}

// TestAttractInBattleRefusesSameGenderAndGenderless is the guard half of the
// same rule, and it guards a bug this engine actually shipped: Attract used
// to land on anything, genderless legendaries and same-sex targets included,
// at 100% accuracy. Stacked with paralysis that is close to a permanent
// lockout, so the rule is a balance rule, not a flavour one.
//
// Every refusing combination is asserted with "never over many seeds" rather
// than "not on this seed": infatuation is the only randomness in the fixture,
// so a rule that leaked would show up on some seed.
func TestAttractInBattleRefusesSameGenderAndGenderless(t *testing.T) {
	d := loadDex(t)
	M, F, N := domain.GenderMale, domain.GenderFemale, domain.GenderGenderless

	refused := []struct {
		label        string
		user, target string
	}{
		{"male onto male", M, M},
		{"female onto female", F, F},
		{"genderless user onto a female", N, F},
		{"male onto a genderless target", M, N},
		{"genderless onto genderless", N, N},
	}
	for _, c := range refused {
		c := c
		svAssertNever(t, "Attract from a "+c.label, 40, func(seed uint64) bool {
			s := svBattle(t, d, seed, []int{143}, []int{143})
			user, foe := s.Active(0), s.Active(1)
			user.Gender, foe.Gender = c.user, c.target
			svGive(t, d, user, "attract")
			svGive(t, d, foe, "splash")
			log := svTurn(d, s, 0, 0)
			if !logHas(log, "But it failed!") {
				t.Fatalf("Attract from a %s should announce a failure; got %v", c.label, logTexts(log))
			}
			return foe.Volatiles.Attract
		})
	}

	// And the follow-through: a foe that was never infatuated never loses a
	// turn to love, no matter how the dice fall.
	svAssertNever(t, "a same-gender foe losing a turn to love", 60, func(seed uint64) bool {
		s := svBattle(t, d, seed, []int{143}, []int{143})
		user, foe := s.Active(0), s.Active(1)
		user.Gender, foe.Gender = M, M
		svGive(t, d, user, "attract", "splash")
		svGive(t, d, foe, "tackle")
		svTurn(d, s, 0, 0)
		hpBefore := user.HP
		log := svTurn(d, s, 1, 0)
		return logHas(log, "in love") || user.HP == hpBefore
	})
}

// TestAttractDoesNotStackOnAnAlreadyInfatuatedFoe: re-using Attract on a
// Pokémon that is already in love fails outright (canon). The reason to pin
// it through a battle is that the alternative — a silent re-apply — is
// invisible in the log and would let a player refresh the volatile forever,
// which matters the day infatuation gets a duration.
func TestAttractDoesNotStackOnAnAlreadyInfatuatedFoe(t *testing.T) {
	d := loadDex(t)
	s := svBattle(t, d, 1, []int{143}, []int{143})
	user, foe := s.Active(0), s.Active(1)
	user.Gender = domain.GenderFemale
	foe.Gender = domain.GenderMale
	svGive(t, d, user, "attract")
	svGive(t, d, foe, "splash")

	if log := svTurn(d, s, 0, 0); !logHas(log, "fell in love") {
		t.Fatalf("first Attract should land; got %v", logTexts(log))
	}
	log := svTurn(d, s, 0, 0)
	if logHas(log, "fell in love") {
		t.Errorf("second Attract re-announced infatuation instead of failing; got %v", logTexts(log))
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("second Attract should fail; got %v", logTexts(log))
	}
	if !foe.Volatiles.Attract {
		t.Errorf("the failed re-apply cleared the existing infatuation")
	}
}

// --- Yawn (applyYawnVolatile, tickStatusVols) ---

// TestYawnPutsTheTargetToSleepOnTheFollowingTurn pins the delay, which is the
// entire point of the move: Yawn is balanced by giving the victim one turn to
// switch out or attack. An off-by-one in the end-of-turn countdown either
// makes it a 100%-accurate Hypnosis (sleep the moment it lands) or leaves the
// target awake forever, and both are the same one-character change.
//
// Played as three turns so the "still awake after the turn it landed" state
// is observed, not inferred.
func TestYawnPutsTheTargetToSleepOnTheFollowingTurn(t *testing.T) {
	d := loadDex(t)
	s := svBattle(t, d, 7, []int{143}, []int{143})
	user, foe := s.Active(0), s.Active(1)
	svGive(t, d, user, "yawn", "splash")
	svGive(t, d, foe, "splash")

	log1 := svTurn(d, s, 0, 0)
	if !logHas(log1, "grew drowsy") {
		t.Fatalf("Yawn should announce drowsiness; got %v", logTexts(log1))
	}
	if foe.Status == StatusSleep {
		t.Fatalf("Yawn slept the target on the turn it landed — the one-turn grace period is gone")
	}
	if foe.Volatiles.Yawn == nil {
		t.Fatalf("the drowsy countdown was not armed")
	}

	svTurn(d, s, 1, 0) // both Splash; the countdown runs out at end of turn
	if foe.Status != StatusSleep {
		t.Fatalf("Yawn should sleep the target at the end of the following turn; status = %q", foe.Status)
	}
	if foe.Volatiles.Yawn != nil {
		t.Errorf("the drowsy countdown should clear once the sleep is inflicted, got %+v", foe.Volatiles.Yawn)
	}

	// And the sleep is a real sleep: the victim cannot act.
	log3 := svTurn(d, s, 1, 0)
	if !logHas(log3, "fast asleep") {
		t.Errorf("the yawned target should be snoozing on the next turn; got %v", logTexts(log3))
	}
}

// TestYawnFailsOnAnAlreadyStatusedTarget: Yawn refuses a target that
// already carries a non-volatile status, and refuses to stack on itself.
// Both matter for the same reason — without them Yawn is a free way to
// overwrite a burn or a paralysis with sleep, which is strictly the better
// status, and stacking would reset the grace period every turn.
func TestYawnFailsOnAnAlreadyStatusedTarget(t *testing.T) {
	d := loadDex(t)

	// Already paralysed: Yawn does nothing at all.
	s := svBattle(t, d, 3, []int{143}, []int{143})
	user, foe := s.Active(0), s.Active(1)
	foe.Status = StatusParalysis
	svGive(t, d, user, "yawn", "splash")
	svGive(t, d, foe, "splash")

	log := svTurn(d, s, 0, 0)
	if !logHas(log, "But it failed!") {
		t.Errorf("Yawn on a paralysed target should fail; got %v", logTexts(log))
	}
	if foe.Volatiles.Yawn != nil {
		t.Fatalf("Yawn armed its countdown on an already-statused target")
	}
	svTurn(d, s, 1, 0)
	svTurn(d, s, 1, 0)
	if foe.Status != StatusParalysis {
		t.Errorf("the paralysis was overwritten, status = %q", foe.Status)
	}

	// Already drowsy: the second Yawn fails rather than refreshing the timer.
	s2 := svBattle(t, d, 3, []int{143}, []int{143})
	user2, foe2 := s2.Active(0), s2.Active(1)
	svGive(t, d, user2, "yawn")
	svGive(t, d, foe2, "splash")
	svTurn(d, s2, 0, 0)
	log2 := svTurn(d, s2, 0, 0)
	if !logHas(log2, "But it failed!") {
		t.Errorf("a second Yawn on a drowsy target should fail; got %v", logTexts(log2))
	}
	if foe2.Status != StatusSleep {
		t.Errorf("the original countdown should still have fired on schedule; status = %q", foe2.Status)
	}
}

// --- Curse (applyCurse, applyCurseFoeVolatile, tickStatusVols) ---

// TestGhostCurseCostsHalfTheUsersHPAndChipsTheFoeEveryTurn plays the Ghost
// branch as a battle. Two things are easy to lose here and both are checked
// with exact HP arithmetic rather than "some damage happened": the user pays
// exactly half its max HP up front, and the victim then loses exactly a
// quarter of its max HP at the end of *every* turn, including the turn the
// curse landed.
//
// The recurring chip is the half that a single-tick test cannot see: a curse
// that cleared itself after one tick, or that never got wired into the
// end-of-turn sweep, is a move that costs its user half its health for
// nothing.
func TestGhostCurseCostsHalfTheUsersHPAndChipsTheFoeEveryTurn(t *testing.T) {
	d := loadDex(t)
	s := svBattle(t, d, 11, []int{94}, []int{143}) // Gengar (Ghost/Poison) vs Snorlax
	ghost, foe := s.Active(0), s.Active(1)
	svGive(t, d, ghost, "curse", "splash")
	svGive(t, d, foe, "splash")

	ghostBefore, foeBefore := ghost.HP, foe.HP
	wantCost, wantChip := ghost.MaxHP/2, foe.MaxHP/4

	log1 := svTurn(d, s, 0, 0)
	if !logHas(log1, "was cursed") {
		t.Fatalf("Ghost Curse should curse the foe; got %v", logTexts(log1))
	}
	if !foe.Volatiles.Curse {
		t.Fatalf("the curse volatile never reached the foe")
	}
	if got := ghostBefore - ghost.HP; got != wantCost {
		t.Errorf("Ghost Curse self-cost = %d HP, want %d (half of %d)", got, wantCost, ghost.MaxHP)
	}
	if got := foeBefore - foe.HP; got != wantChip {
		t.Errorf("first Curse chip = %d HP, want %d (a quarter of %d)", got, wantChip, foe.MaxHP)
	}

	// The chip repeats, turn after turn, with no further input from the user.
	for turn := 2; turn <= 3; turn++ {
		before := foe.HP
		svTurn(d, s, 1, 0)
		if got := before - foe.HP; got != wantChip {
			t.Fatalf("Curse chip on turn %d = %d HP, want %d", turn, got, wantChip)
		}
		if !foe.Volatiles.Curse {
			t.Fatalf("the curse fell off after turn %d — it lasts until the victim switches", turn)
		}
	}
}

// TestNonGhostCurseBoostsTheUserAndLeavesTheFoeAlone pins the type-routed
// fork. Curse is one move ID with two completely different behaviours, and
// the dataset entry says target "foe" — so an engine that trusts the dataset
// instead of dispatching on the user's type would make a Snorlax pay half its
// health to curse the opponent. The whole-battle check is what proves the
// dispatch survives the real status-move path.
func TestNonGhostCurseBoostsTheUserAndLeavesTheFoeAlone(t *testing.T) {
	d := loadDex(t)
	s := svBattle(t, d, 5, []int{143}, []int{143}) // Snorlax (Normal) uses Curse
	user, foe := s.Active(0), s.Active(1)
	svGive(t, d, user, "curse", "splash")
	svGive(t, d, foe, "splash")

	userBefore, foeBefore := user.HP, foe.HP
	svTurn(d, s, 0, 0)

	if user.HP != userBefore {
		t.Errorf("a non-Ghost Curse should cost no HP; the user lost %d", userBefore-user.HP)
	}
	if user.Stages.Atk != 1 || user.Stages.Def != 1 || user.Stages.Spe != -1 {
		t.Errorf("non-Ghost Curse stages = Atk %d / Def %d / Spe %d, want +1 / +1 / -1",
			user.Stages.Atk, user.Stages.Def, user.Stages.Spe)
	}
	if foe.Volatiles.Curse {
		t.Errorf("a non-Ghost Curse cursed the foe")
	}
	// One more quiet turn: nothing is chipping the foe.
	svTurn(d, s, 1, 0)
	if foe.HP != foeBefore {
		t.Errorf("the foe lost %d HP to a non-Ghost Curse", foeBefore-foe.HP)
	}
}

// --- Destiny Bond (applyDestinyBondVolatile) ---

// TestDestinyBondDragsTheKillerDownWithIt is the payoff turn, played whole:
// the bonded Pokémon uses the move, is knocked out by a direct attack on the
// same turn, and its killer faints with it. Nothing short of a real battle
// exercises the chain — the volatile has to be armed by the move, survive
// until the foe's attack resolves, and be read in the faint-resolution tail.
//
// Both sides carry a second Pokémon so the double KO leaves a legal battle to
// keep playing, and the replace phase is driven through to prove it does.
func TestDestinyBondDragsTheKillerDownWithIt(t *testing.T) {
	d := loadDex(t)
	s := svBattle(t, d, 13, []int{143, 6}, []int{94, 26}) // Snorlax+Charizard vs Gengar+Raichu
	killer, bonded := s.Active(0), s.Active(1)
	svGive(t, d, killer, "earthquake") // Ground: 2× on Gengar, and Levitate is stripped
	svGive(t, d, bonded, "destiny-bond")
	bonded.HP = 1 // Gengar outspeeds Snorlax, so the bond is armed before the hit

	log := svTurn(d, s, 0, 0)
	if !logHas(log, "trying to take its foe down") {
		t.Fatalf("Destiny Bond should announce itself; got %v", logTexts(log))
	}
	if !bonded.Fainted {
		t.Fatalf("the bonded Pokémon should have fainted to the Earthquake; HP = %d", bonded.HP)
	}
	if !killer.Fainted {
		t.Fatalf("Destiny Bond should have taken the attacker down too; log: %v", logTexts(log))
	}
	if !logHas(log, "took its attacker down") {
		t.Errorf("missing the Destiny Bond KO line; got %v", logTexts(log))
	}

	// The battle is not over — both sides still have a Pokémon, and the
	// engine must be sitting in the replace phase waiting for them.
	if s.Ended() {
		t.Fatalf("a double KO with reserves left should not end the battle")
	}
	if s.Phase != PhaseReplace {
		t.Fatalf("phase after a double KO = %v, want PhaseReplace", s.Phase)
	}
	var sw [2]*Action
	for side := 0; side < 2; side++ {
		acts := LegalActions(s, side)
		if len(acts) == 0 {
			t.Fatalf("side %d has no legal replacement", side)
		}
		a := acts[0]
		sw[side] = &a
	}
	ResolveReplace(s, sw)
	if s.Active(0).Fainted || s.Active(1).Fainted {
		t.Errorf("both sides should have sent out a healthy Pokémon")
	}
}

// TestDestinyBondDoesNotCarryIntoTheNextTurn: the bond is armed for the turn
// it is used and no longer. Without the end-of-turn clear it becomes a
// permanent "kill me and you die" aura for the rest of the Pokémon's life on
// the field, which is a different (and far stronger) move than the one canon
// has. The check that matters is the negative one, and it needs two turns to
// exist at all.
func TestDestinyBondDoesNotCarryIntoTheNextTurn(t *testing.T) {
	d := loadDex(t)
	s := svBattle(t, d, 17, []int{143, 6}, []int{94, 26})
	killer, bonded := s.Active(0), s.Active(1)
	svGive(t, d, killer, "splash", "earthquake")
	svGive(t, d, bonded, "destiny-bond", "splash")

	log1 := svTurn(d, s, 0, 0) // bond armed, nobody attacks
	if !logHas(log1, "trying to take its foe down") {
		t.Fatalf("Destiny Bond should announce itself; got %v", logTexts(log1))
	}
	if bonded.Volatiles.DestinyBond {
		t.Fatalf("Destiny Bond should be spent at the end of the turn it was used")
	}

	bonded.HP = 1
	svTurn(d, s, 1, 1) // now the KO lands, one turn too late for the bond
	if !bonded.Fainted {
		t.Fatalf("the Earthquake should have KO'd the bonded Pokémon; HP = %d", bonded.HP)
	}
	if killer.Fainted {
		t.Errorf("a Destiny Bond from the previous turn must not claim this turn's attacker")
	}
}

// --- Focus Energy (applyFocusEnergyVolatile) ---

// TestFocusEnergyLiftsTheCritRateInBattle measures what Focus Energy is for.
// The unit test for this reads the crit denominator out of a helper; that
// pins the table but not the effect, and the volatile could stop being set by
// the move — or stop being read by the damage roll — without disturbing it.
//
// Here the same battle is played with and without the setup turn and the
// critical hits are counted: ~1/24 becomes ~1/2 (Gen 6+ crit table, +2
// stages). Two measurements rather than one, because "crits happen" is also
// true of the baseline, and only the gap between them is the mechanic.
func TestFocusEnergyLiftsTheCritRateInBattle(t *testing.T) {
	d := loadDex(t)

	// A scripted battle: side 0 optionally pumps itself up, then Tackles.
	critOnAttackTurn := func(seed uint64, pump bool) bool {
		s := svBattle(t, d, seed, []int{143}, []int{143})
		atk, def := s.Active(0), s.Active(1)
		svGive(t, d, atk, "focus-energy", "tackle")
		svGive(t, d, def, "splash")
		if pump {
			log := svTurn(d, s, 0, 0)
			if !logHas(log, "getting pumped") {
				t.Fatalf("seed %d: Focus Energy should announce itself; got %v", seed, logTexts(log))
			}
			if !atk.Volatiles.FocusEnergy {
				t.Fatalf("seed %d: Focus Energy did not latch onto the user", seed)
			}
		} else {
			svTurn(d, s, 1, 0) // an ordinary Tackle turn, to keep the turn count equal
		}
		return logHas(svTurn(d, s, 1, 0), "A critical hit!")
	}

	const seeds = 400
	svAssertRate(t, "crit rate with Focus Energy", seeds, 0.42, 0.58, func(seed uint64) bool {
		return critOnAttackTurn(seed, true)
	})
	svAssertRate(t, "crit rate without Focus Energy", seeds, 0.0, 0.14, func(seed uint64) bool {
		return critOnAttackTurn(seed, false)
	})
}

// TestFocusEnergyCannotBeStacked: a second Focus Energy fails outright rather
// than pumping the ratio further. Canon, and a real balance edge — stacking
// crit stages to 3 makes every hit a guaranteed critical.
func TestFocusEnergyCannotBeStacked(t *testing.T) {
	d := loadDex(t)
	s := svBattle(t, d, 2, []int{143}, []int{143})
	atk, def := s.Active(0), s.Active(1)
	svGive(t, d, atk, "focus-energy", "tackle")
	svGive(t, d, def, "splash")

	svTurn(d, s, 0, 0)
	log := svTurn(d, s, 0, 0)
	if !logHas(log, "But it failed!") {
		t.Errorf("a second Focus Energy should fail; got %v", logTexts(log))
	}
	if !atk.Volatiles.FocusEnergy {
		t.Errorf("the failed re-use dropped the existing Focus Energy")
	}

	// The ratio did not climb to the guaranteed-crit tier: over many seeds
	// some Tackles still fail to crit.
	missedACrit := false
	for seed := uint64(1); seed <= 40 && !missedACrit; seed++ {
		s2 := svBattle(t, d, seed, []int{143}, []int{143})
		a2, d2 := s2.Active(0), s2.Active(1)
		svGive(t, d, a2, "focus-energy", "tackle")
		svGive(t, d, d2, "splash")
		svTurn(d, s2, 0, 0)
		svTurn(d, s2, 0, 0) // the failed second pump
		if !logHas(svTurn(d, s2, 1, 0), "A critical hit!") {
			missedACrit = true
		}
	}
	if !missedACrit {
		t.Errorf("two Focus Energys crit on all 40 seeds — the second one stacked into guaranteed crits")
	}
}

// --- Minimize and Foresight (applyMinimizeVolatile, applyForesightVolatile) ---

// TestMinimizeRaisesEvasionAndForesightStripsIt plays the pair against each
// other, which is the only interesting thing either does.
//
// Minimize: the volatile flag is set (nothing reads it yet — Rollout and Body
// Slam doubling are unmodelled — but a port needs it present, and it is the
// only observable this handler has) and the +2 evasion actually makes a
// 100%-accurate move start missing. The rate is measured across seeds: at -2
// combined accuracy stages a 100% move lands 60% of the time, and asserting
// "it missed once" would pass just as happily on a broken 5% or 95%.
//
// Foresight: identifying the target zeroes that positive evasion, so the same
// Tackle goes back to landing every single time. The "always" side is checked
// with svAssertAlways rather than a rate — after Foresight there is no roll
// left to make.
func TestMinimizeRaisesEvasionAndForesightStripsIt(t *testing.T) {
	d := loadDex(t)

	// Baseline: no evasion in play, Tackle never misses.
	svAssertAlways(t, "Tackle landing on an unboosted target", 60, func(seed uint64) bool {
		s := svBattle(t, d, seed, []int{143}, []int{143})
		atk, def := s.Active(0), s.Active(1)
		svGive(t, d, atk, "tackle")
		svGive(t, d, def, "splash")
		before := def.HP
		svTurn(d, s, 0, 0)
		return def.HP < before
	})

	// Minimize alone: the flag lands, evasion goes to +2, and Tackle starts
	// missing about 40% of the time.
	svAssertRate(t, "Tackle landing on a minimized target", 500, 0.50, 0.70, func(seed uint64) bool {
		s := svBattle(t, d, seed, []int{143}, []int{143})
		atk, def := s.Active(0), s.Active(1)
		svGive(t, d, atk, "splash", "tackle")
		svGive(t, d, def, "minimize", "splash")
		svTurn(d, s, 0, 0)
		if !def.Volatiles.Minimize {
			t.Fatalf("seed %d: Minimize did not set its volatile", seed)
		}
		if def.Stages.Eva != 2 {
			t.Fatalf("seed %d: Minimize left evasion at stage %d, want +2", seed, def.Stages.Eva)
		}
		before := def.HP
		svTurn(d, s, 1, 1)
		return def.HP < before
	})

	// Minimize then Foresight: the evasion is stripped and Tackle is exact
	// again on every seed.
	svAssertAlways(t, "Tackle landing on a foresighted minimizer", 60, func(seed uint64) bool {
		s := svBattle(t, d, seed, []int{143}, []int{143})
		atk, def := s.Active(0), s.Active(1)
		svGive(t, d, atk, "splash", "foresight", "tackle")
		svGive(t, d, def, "minimize", "splash")
		// Two setup turns, not one: with both moves on the same turn a speed
		// tie could resolve Foresight first and let Minimize re-raise the
		// evasion behind it, which would make the test's result a coin flip.
		svTurn(d, s, 0, 0) // the target minimizes
		svTurn(d, s, 1, 1) // then it is identified
		if def.Stages.Eva != 0 {
			t.Fatalf("seed %d: Foresight left evasion at stage %d, want 0", seed, def.Stages.Eva)
		}
		if !def.Volatiles.Foresight {
			t.Fatalf("seed %d: Foresight did not set its volatile", seed)
		}
		before := def.HP
		svTurn(d, s, 2, 1)
		return def.HP < before
	})
}

// TestForesightLetsANormalMoveLandOnAGhostInBattle pins the other half of
// Foresight: it lifts the Ghost type's immunity to Normal and Fighting. Both
// states are played in the same battle so the "before" is a real immune turn
// and not an assumption — Tackle bounces off Gengar, Foresight identifies it,
// and the identical Tackle then takes HP off.
func TestForesightLetsANormalMoveLandOnAGhostInBattle(t *testing.T) {
	d := loadDex(t)
	s := svBattle(t, d, 19, []int{143}, []int{94}) // Snorlax vs Gengar (Ghost/Poison)
	atk, ghost := s.Active(0), s.Active(1)
	svGive(t, d, atk, "tackle", "foresight")
	svGive(t, d, ghost, "splash")

	before := ghost.HP
	svTurn(d, s, 0, 0)
	if ghost.HP != before {
		t.Fatalf("Tackle should not touch a Ghost before it is identified; it lost %d HP", before-ghost.HP)
	}

	log := svTurn(d, s, 1, 0)
	if !logHas(log, "was identified") {
		t.Fatalf("Foresight should announce the identification; got %v", logTexts(log))
	}

	before = ghost.HP
	svTurn(d, s, 0, 0)
	if ghost.HP >= before {
		t.Errorf("an identified Ghost should take neutral damage from Tackle; HP unchanged at %d", ghost.HP)
	}
}
