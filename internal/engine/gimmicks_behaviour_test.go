package engine

import "testing"

// gimmicks_behaviour_test.go plays the gimmick and lock/restrict volatiles as
// whole battles: every mechanic below is reached by choosing the move through
// ResolveTurn and then reading the public state and the turn log, never by
// calling the engine's internals.
//
// That is the point of the file. The mechanics here were pinned only by tests
// that hand-applied the volatile, which proves the handler does what the
// handler does but proves nothing about the move ever reaching it — a move
// missing from the dispatch table, a target picked from the wrong side, or a
// flag swept away one turn too early all survive that kind of test. Most of
// these effects are also only meaningful across turns (Magic Coat bounces the
// *next* status move, Snatch steals the *next* self-boost, Smack Down grounds
// a flier so a *later* Ground move connects), so each test below resolves the
// turns that make the rule visible.

// battleWithMoves builds a 1v1 with explicit movesets, so a test never depends
// on learnset order or on which four moves the team builder happened to pick.
// Abilities are cleared on both sides: none of the rules below involve one, and
// an ability that quietly halves a hit or refuses a status is exactly the kind
// of second live mechanic that makes a failure hard to read.
//
// The seed is arbitrary and load-bearing nowhere: nothing in this file asserts
// on a random roll, and every test here passes on any seed. Damage assertions
// are "did HP move at all", never an exact number.
func battleWithMoves(t *testing.T, dex1 int, moves1 []string, dex2 int, moves2 []string) *BattleState {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{dex1}, "P2", []int{dex2}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for side, ids := range [][]string{moves1, moves2} {
		slots := make([]MoveSlot, 0, len(ids))
		for _, id := range ids {
			slots = append(slots, MoveSlot{MoveID: id, PP: 20, MaxPP: 20})
		}
		s.Active(side).Moves = slots
		s.Active(side).Ability = AbilityNone
	}
	return s
}

// moveTurn resolves one turn where side 0 picks slot i0 and side 1 picks slot i1.
func moveTurn(t *testing.T, s *BattleState, i0, i1 int) []LogLine {
	t.Helper()
	d := loadDex(t)
	return ResolveTurn(d, s, [2]Action{
		{Kind: ActionMove, Index: i0},
		{Kind: ActionMove, Index: i1},
	})
}

// --- Stockpile ---

// TestStockpileMoveStacksToThreeAcrossTurns pins the canon rule that Stockpile
// banks up to three charges, each one worth +1 Defense and +1 Sp. Def, and that
// a fourth attempt fails outright rather than boosting further.
//
// Spit Up and Swallow already had battle tests, but they were handed a
// stockpile count directly — nothing checked that using the actual move builds
// one. A Stockpile that boosted without incrementing (or incremented without
// boosting) passed the whole suite; here the count and the two stages have to
// agree turn by turn.
func TestStockpileMoveStacksToThreeAcrossTurns(t *testing.T) {
	s := battleWithMoves(t, 143, []string{"stockpile", "splash"}, 143, []string{"splash"})
	user := s.Active(0)

	for want := 1; want <= 3; want++ {
		moveTurn(t, s, 0, 0)
		if user.Volatiles.Stockpile == nil || user.Volatiles.Stockpile.Count != want {
			t.Fatalf("after %d uses Stockpile count = %v, want %d", want, user.Volatiles.Stockpile, want)
		}
		if user.Stages.Def != want || user.Stages.SpD != want {
			t.Errorf("after %d uses stages Def=%d SpD=%d, want %d/%d",
				want, user.Stages.Def, user.Stages.SpD, want, want)
		}
	}

	// Fourth use: the cap holds and the move says so.
	log := moveTurn(t, s, 0, 0)
	if !logHas(log, "But it failed") {
		t.Errorf("a fourth Stockpile should fail loudly; log: %v", logTexts(log))
	}
	if user.Volatiles.Stockpile.Count != 3 {
		t.Errorf("Stockpile stacked past the cap: count=%d", user.Volatiles.Stockpile.Count)
	}
	if user.Stages.Def != 3 || user.Stages.SpD != 3 {
		t.Errorf("a failed Stockpile still boosted: Def=%d SpD=%d, want 3/3", user.Stages.Def, user.Stages.SpD)
	}
}

// --- Smack Down ---

// TestSmackDownMoveGroundsFlyingForLaterGroundMoves pins the canon rule that
// Smack Down knocks a Flying-type out of the air, and that the grounding lasts
// past the turn it was applied — a Ground move that was refused before Smack
// Down lands afterwards.
//
// This is the whole reason the move exists, and it is only visible across three
// turns: the immunity, the knock-down, and the Ground move that now connects.
// Chansey is the attacker because its Attack stat is small enough that neither
// hit comes close to a KO, which keeps all three turns in one battle.
func TestSmackDownMoveGroundsFlyingForLaterGroundMoves(t *testing.T) {
	s := battleWithMoves(t, 113, []string{"mud-slap", "smack-down"}, 6, []string{"splash"})
	flier := s.Active(1)

	// Turn 1: Charizard is Fire/Flying, so Ground does not affect it at all.
	before := flier.HP
	log := moveTurn(t, s, 0, 0)
	if flier.HP != before {
		t.Fatalf("setup: Ground should not affect a Flying-type (%d -> %d)", before, flier.HP)
	}
	if !logHas(log, "It doesn't affect") {
		t.Fatalf("setup: expected a type-immunity line; log: %v", logTexts(log))
	}

	// Turn 2: Smack Down connects and pulls it down.
	before = flier.HP
	log = moveTurn(t, s, 1, 0)
	if flier.HP >= before {
		t.Errorf("Smack Down should deal damage (%d -> %d)", before, flier.HP)
	}
	if !flier.Volatiles.SmackDown {
		t.Fatalf("Smack Down did not ground the target; log: %v", logTexts(log))
	}
	if !logHas(log, "fell straight down") {
		t.Errorf("missing the Smack Down line; log: %v", logTexts(log))
	}

	// Turn 3: the same Ground move now lands, on a later turn than the grounding.
	before = flier.HP
	log = moveTurn(t, s, 0, 0)
	if flier.HP >= before {
		t.Errorf("Ground should hit a smacked-down flier (%d -> %d); log: %v", before, flier.HP, logTexts(log))
	}
}

// TestSmackDownMoveCancelsMagnetRiseInBattle pins the canon rule that Smack
// Down strips an active Magnet Rise: a Pokémon floating on electromagnetism is
// pulled back to earth and Ground moves resume hitting it immediately.
//
// The two volatiles both answer "is this target grounded?", so an engine that
// sets the Smack Down flag without clearing Magnet Rise still reports the
// target as airborne and the move accomplishes nothing. Playing it out is the
// only way to see which volatile wins.
func TestSmackDownMoveCancelsMagnetRiseInBattle(t *testing.T) {
	s := battleWithMoves(t, 113, []string{"mud-slap", "smack-down", "splash"}, 143, []string{"magnet-rise", "splash"})
	target := s.Active(1)

	// Turn 1: the foe rises. (Chansey out-speeds Snorlax, so the Ground move
	// waits until the next turn to be refused rather than landing first.)
	log := moveTurn(t, s, 2, 0)
	if target.Volatiles.MagnetRise == nil {
		t.Fatalf("setup: Magnet Rise did not take; log: %v", logTexts(log))
	}

	// Turn 2: while it floats, the Ground move does nothing.
	before := target.HP
	log = moveTurn(t, s, 0, 1)
	if target.HP != before {
		t.Fatalf("setup: Magnet Rise should null the Ground move (%d -> %d); log: %v",
			before, target.HP, logTexts(log))
	}

	// Turn 3: Smack Down cancels it.
	moveTurn(t, s, 1, 1)
	if target.Volatiles.MagnetRise != nil {
		t.Errorf("Smack Down should cancel Magnet Rise; still %+v", target.Volatiles.MagnetRise)
	}
	if !target.Volatiles.SmackDown {
		t.Errorf("Smack Down flag not set on the target")
	}

	// Turn 4: Ground connects again.
	before = target.HP
	log = moveTurn(t, s, 0, 1)
	if target.HP >= before {
		t.Errorf("Ground should hit once Magnet Rise is cancelled (%d -> %d); log: %v",
			before, target.HP, logTexts(log))
	}
}

// --- Magic Coat ---

// TestMagicCoatMoveBouncesTheFoesStatusMove pins two canon rules at once:
// Magic Coat's +4 priority means it is up before the status move it answers
// even against a same-speed foe, and the shield covers exactly one turn — it is
// not still standing on the turn after.
//
// (This engine degrades the bounce: the move is blocked rather than reflected
// back at its user. The rule under test is that the status move does not land
// on the coater.)
//
// A one-turn shield that never expires is the failure this guards: with both
// sides on Growl, an engine that forgets to clear the flag would leave the
// coater permanently immune to status, which the second turn here catches.
func TestMagicCoatMoveBouncesTheFoesStatusMove(t *testing.T) {
	s := battleWithMoves(t, 143, []string{"growl", "splash"}, 143, []string{"magic-coat", "splash"})
	coater := s.Active(1)

	// Turn 1: Magic Coat goes up first (priority +4) and eats the Growl.
	log := moveTurn(t, s, 0, 0)
	if coater.Stages.Atk != 0 {
		t.Errorf("Growl should not land through Magic Coat; coater Atk stage = %d", coater.Stages.Atk)
	}
	if !logHas(log, "bounced the move back") {
		t.Errorf("missing the Magic Coat line; log: %v", logTexts(log))
	}
	if coater.Volatiles.MagicCoat {
		t.Errorf("Magic Coat should be spent after it blocks a move")
	}

	// Turn 2: no Magic Coat this turn, so the same Growl lands. This is also
	// the proof that turn 1 was blocked by the coat and not by something else.
	log = moveTurn(t, s, 0, 1)
	if coater.Stages.Atk != -1 {
		t.Errorf("Growl should land the turn after Magic Coat; Atk stage = %d, want -1; log: %v",
			coater.Stages.Atk, logTexts(log))
	}
}

// TestMagicCoatMoveOnlyAnswersMovesAimedAtIt pins the canon rule that Magic
// Coat catches exactly the status moves the foe aims at the coater. An attack
// goes straight through it, and so does a status move the foe aims at itself —
// a foe that spends its turn on Swords Dance keeps the boost.
//
// Both negatives matter for the same reason: a Magic Coat that answered
// everything would be a +4-priority Protect that also cancels the opponent's
// setup, which is a far stronger move than the one in the dex. The self-target
// half is the one a real battle can see going wrong, since a "bounced" Swords
// Dance leaves its user with nothing.
func TestMagicCoatMoveOnlyAnswersMovesAimedAtIt(t *testing.T) {
	// A damaging move is not a status move, so the coat is irrelevant to it.
	s := battleWithMoves(t, 143, []string{"tackle"}, 143, []string{"magic-coat"})
	coater := s.Active(1)
	before := coater.HP
	log := moveTurn(t, s, 0, 0)
	if !logHas(log, "shrouded itself") {
		t.Fatalf("setup: Magic Coat never went up; log: %v", logTexts(log))
	}
	if coater.HP >= before {
		t.Errorf("Magic Coat must not block a damaging move (%d -> %d); log: %v",
			before, coater.HP, logTexts(log))
	}

	// A self-targeted status move is aimed away from the coater and resolves
	// normally for the Pokémon that chose it.
	s = battleWithMoves(t, 143, []string{"swords-dance"}, 143, []string{"magic-coat"})
	user := s.Active(0)
	log = moveTurn(t, s, 0, 0)
	if logHas(log, "bounced the move back") {
		t.Errorf("Magic Coat must not bounce a self-targeted status move; log: %v", logTexts(log))
	}
	if user.Stages.Atk != 2 {
		t.Errorf("Swords Dance should still boost its user through a Magic Coat; Atk stage = %d, want 2; log: %v",
			user.Stages.Atk, logTexts(log))
	}
}

// --- Snatch ---

// TestSnatchMoveStealsTheFoesSelfBoost pins the canon rule that Snatch takes
// the foe's next self-targeted status move for itself — the boost lands on the
// thief, not on the Pokémon that chose it — and that the theft is armed for
// exactly one turn.
//
// Snatch's +4 priority is load-bearing here: both sides are the same species,
// so if the priority were not honored the Swords Dance would resolve before the
// steal was armed and the thief would get nothing.
func TestSnatchMoveStealsTheFoesSelfBoost(t *testing.T) {
	s := battleWithMoves(t, 143, []string{"swords-dance", "splash"}, 143, []string{"snatch", "splash"})
	user, thief := s.Active(0), s.Active(1)

	log := moveTurn(t, s, 0, 0)
	if user.Stages.Atk != 0 {
		t.Errorf("a snatched Swords Dance must not boost its original user; Atk stage = %d", user.Stages.Atk)
	}
	if thief.Stages.Atk != 2 {
		t.Errorf("the snatcher should receive the +2 Atk; Atk stage = %d; log: %v", thief.Stages.Atk, logTexts(log))
	}
	if !logHas(log, "snatched the move") {
		t.Errorf("missing the Snatch line; log: %v", logTexts(log))
	}
	if thief.Volatiles.Snatch {
		t.Errorf("Snatch should be spent after a steal")
	}

	// Turn 2: nothing left to steal with, so the boost stays home.
	moveTurn(t, s, 0, 1)
	if user.Stages.Atk != 2 {
		t.Errorf("Swords Dance should boost its user the turn after Snatch; Atk stage = %d, want 2", user.Stages.Atk)
	}
	if thief.Stages.Atk != 2 {
		t.Errorf("the snatcher should not keep stealing; Atk stage = %d, want 2", thief.Stages.Atk)
	}
}

// TestSnatchMoveIgnoresFoeTargetedStatusMoves pins the canon rule that Snatch
// only steals moves the user aims at itself. Growl is a status move too, but it
// is aimed at the opponent, so it resolves normally and drops the snatcher's
// Attack.
//
// Without the target check Snatch would swallow every status move in the game —
// and worse, "stealing" a foe-targeted move would redirect it back at its own
// user. The negative case is what keeps the steal narrow.
func TestSnatchMoveIgnoresFoeTargetedStatusMoves(t *testing.T) {
	s := battleWithMoves(t, 143, []string{"growl"}, 143, []string{"snatch"})
	user, thief := s.Active(0), s.Active(1)

	log := moveTurn(t, s, 0, 0)
	if logHas(log, "snatched the move") {
		t.Errorf("Snatch must not steal a foe-targeted status move; log: %v", logTexts(log))
	}
	if thief.Stages.Atk != -1 {
		t.Errorf("Growl should land on the snatcher; Atk stage = %d, want -1; log: %v",
			thief.Stages.Atk, logTexts(log))
	}
	if user.Stages.Atk != 0 {
		t.Errorf("Growl must not be turned back on its user; Atk stage = %d", user.Stages.Atk)
	}
}

// --- Grudge ---

// TestGrudgeMovePersistsUntilTheHolderLeaves pins Grudge's lifetime: the move
// arms the holder and the arming survives to later turns, but leaves the field
// with it when the holder switches out.
//
// (Canon has Grudge drain the PP of the move that KOs the holder. This engine
// registers the volatile only — the payoff is not modeled — so the rule under
// test is the arming and its lifetime.)
//
// Lifetime is the part a real battle can check and an apply-the-volatile test
// cannot: Grudge sits next to Snatch and Magic Coat in the same bag, and those
// two are wiped by the end-of-turn sweep. A Grudge swept with them would be
// dead before the faint it is waiting for.
func TestGrudgeMovePersistsUntilTheHolderLeaves(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143, 113}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for i := 0; i < 2; i++ {
		s.Active(i).Ability = AbilityNone
		s.Active(i).Moves = []MoveSlot{
			{MoveID: "grudge", PP: 5, MaxPP: 5},
			{MoveID: "splash", PP: 40, MaxPP: 40},
		}
	}
	user := s.Active(0)

	log := moveTurn(t, s, 0, 1)
	if !user.Volatiles.Grudge {
		t.Fatalf("Grudge did not arm; log: %v", logTexts(log))
	}
	if !logHas(log, "bear a grudge") {
		t.Errorf("missing the Grudge line; log: %v", logTexts(log))
	}

	// A later turn: still armed. Snatch and Magic Coat would already be gone.
	moveTurn(t, s, 1, 1)
	if !user.Volatiles.Grudge {
		t.Errorf("Grudge should survive the end-of-turn sweep")
	}

	// Switching out takes it with them.
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 1}})
	if s.Sides[0].Team[0].Volatiles.Grudge {
		t.Errorf("Grudge should clear when its holder leaves the field")
	}
}

// --- Gastro Acid ---

// TestGastroAcidMoveMarksTheTargetOnce pins that Gastro Acid marks its target
// and that a second cast on an already-marked target fails rather than
// re-applying.
//
// (Canon suppresses the target's ability. This engine registers the volatile
// without threading suppression into the ability hooks, so the rule under test
// is the mark, its "already suppressed" guard, and its lifetime.)
//
// The guard matters even in the degraded form: it is what makes a wasted turn
// read as a failure in the log instead of a silent no-op, and it is the branch
// that will decide re-application when suppression is threaded in.
func TestGastroAcidMoveMarksTheTargetOnce(t *testing.T) {
	s := battleWithMoves(t, 151, []string{"gastro-acid", "splash"}, 113, []string{"splash"})
	target := s.Active(1)

	log := moveTurn(t, s, 0, 0)
	if !target.Volatiles.GastroAcid {
		t.Fatalf("Gastro Acid did not mark the target; log: %v", logTexts(log))
	}
	if !logHas(log, "ability was suppressed") {
		t.Errorf("missing the Gastro Acid line; log: %v", logTexts(log))
	}

	// Second cast on the same target: refused, and the mark is untouched.
	log = moveTurn(t, s, 0, 0)
	if !logHas(log, "But it failed") {
		t.Errorf("a second Gastro Acid should fail; log: %v", logTexts(log))
	}
	if !target.Volatiles.GastroAcid {
		t.Errorf("the failed second cast should leave the mark in place")
	}
}

// --- Embargo ---

// TestEmbargoMoveSuppressesLeftoversUntilItExpires pins the canon rule that
// Embargo stops the target using its held item for five turns, and that the
// item works again on the sixth — the target keeps holding it throughout.
//
// Leftovers is the clearest probe: a heal every turn, or not. Casting the real
// move also pins the timer's length end to end, which a test that installs the
// volatile by hand cannot — it has already chosen the number of turns itself.
func TestEmbargoMoveSuppressesLeftoversUntilItExpires(t *testing.T) {
	s := battleWithMoves(t, 143, []string{"embargo", "splash"}, 143, []string{"splash"})
	holder := s.Active(1)
	holder.Item = ItemLeftovers
	holder.HP = holder.MaxHP / 2

	before := holder.HP
	log := moveTurn(t, s, 0, 0)
	if holder.Volatiles.Embargo == nil {
		t.Fatalf("Embargo did not take; log: %v", logTexts(log))
	}
	if !logHas(log, "can't use items") {
		t.Errorf("missing the Embargo line; log: %v", logTexts(log))
	}
	if holder.HP != before {
		t.Fatalf("Leftovers healed on the turn Embargo landed (%d -> %d)", before, holder.HP)
	}
	if holder.Item != ItemLeftovers {
		t.Errorf("Embargo suppresses the item, it does not take it away; item = %v", holder.Item)
	}

	// Turns 2-5: still suppressed. The timer expires at the end of turn 5.
	for turn := 2; turn <= 5; turn++ {
		before = holder.HP
		log = moveTurn(t, s, 1, 0)
		if holder.HP != before {
			t.Fatalf("Leftovers healed on turn %d, still inside the Embargo (%d -> %d)", turn, before, holder.HP)
		}
	}
	if holder.Volatiles.Embargo != nil {
		t.Fatalf("Embargo should have expired after 5 turns; %+v", holder.Volatiles.Embargo)
	}
	if !logHas(log, "use items again") {
		t.Errorf("missing the Embargo expiry line; log: %v", logTexts(log))
	}

	// Turn 6: the item is live again.
	before = holder.HP
	moveTurn(t, s, 1, 0)
	if holder.HP <= before {
		t.Errorf("Leftovers should heal once the Embargo is over (%d -> %d)", before, holder.HP)
	}
}

// --- Imprison ---

// TestImprisonMoveSealsSharedMovesInBattle pins the canon rule that Imprison
// seals every move the user and the opponent both know: the opponent may not
// pick a sealed move, and if a controller submits one anyway the attempt is
// refused and accomplishes nothing.
//
// Imprison's snapshot is taken at cast time from the two movesets, so it is
// wired through both sides at once — the sealed list lives on the user but is
// read from the foe's side. The control battle at the end is what proves the
// foe's Psychic was stopped by the seal rather than by a miss or an immunity.
func TestImprisonMoveSealsSharedMovesInBattle(t *testing.T) {
	// Mew (Speed 100) outruns Chansey (Speed 50), so Imprison is up before the
	// foe's move resolves on the very first turn.
	s := battleWithMoves(t, 151, []string{"imprison", "psychic"}, 113, []string{"psychic", "splash"})
	user, foe := s.Active(0), s.Active(1)

	before := user.HP
	log := moveTurn(t, s, 0, 0)
	if user.Volatiles.Imprison == nil {
		t.Fatalf("Imprison did not take; log: %v", logTexts(log))
	}
	if !logHas(log, "sealed any moves") {
		t.Errorf("missing the Imprison line; log: %v", logTexts(log))
	}
	if !logHas(log, "can't use the sealed") {
		t.Errorf("the foe's shared move should be refused out loud; log: %v", logTexts(log))
	}
	if user.HP != before {
		t.Errorf("a sealed move must not land (%d -> %d)", before, user.HP)
	}

	// The read path agrees with the resolution path: the sealed slot is not
	// offered to the foe at all, while its unshared slot still is.
	d := loadDex(t)
	var sawSealed, sawFree bool
	for _, a := range LegalActionsDex(d, s, 1) {
		if a.Kind != ActionMove {
			continue
		}
		switch a.Index {
		case 0:
			sawSealed = true
		case 1:
			sawFree = true
		}
	}
	if sawSealed {
		t.Errorf("LegalActions should not offer the foe a sealed move")
	}
	if !sawFree {
		t.Errorf("LegalActions should still offer the foe its unsealed move")
	}
	if foe.Volatiles.Imprison != nil {
		t.Errorf("Imprison lives on its user, not on the foe")
	}

	// Control: the same Psychic, from the same foe, with the user Splashing
	// instead of Imprisoning. If this does not hurt, the test above proves
	// nothing.
	c := battleWithMoves(t, 151, []string{"splash", "psychic"}, 113, []string{"psychic", "splash"})
	before = c.Active(0).HP
	moveTurn(t, c, 0, 0)
	if c.Active(0).HP >= before {
		t.Fatalf("control: an unsealed Psychic should deal damage (%d -> %d)", before, c.Active(0).HP)
	}
}

// TestImprisonMoveFailsWhenNoMovesAreShared pins the canon rule that Imprison
// needs at least one move in common: against a foe that shares nothing, the
// move fails and seals nothing at all.
//
// An engine that armed an empty seal would look harmless right up until some
// later comparison treated "no entries" as "matches everything", so the second
// half here checks the foe can still attack afterwards.
func TestImprisonMoveFailsWhenNoMovesAreShared(t *testing.T) {
	s := battleWithMoves(t, 151, []string{"imprison", "psychic"}, 113, []string{"tackle", "splash"})
	user := s.Active(0)

	log := moveTurn(t, s, 0, 1)
	if user.Volatiles.Imprison != nil {
		t.Errorf("Imprison should fail with no shared moves; got %+v", user.Volatiles.Imprison)
	}
	if !logHas(log, "But it failed") {
		t.Errorf("Imprison with nothing to seal should fail loudly; log: %v", logTexts(log))
	}

	// The foe's own moves are untouched by the failed cast.
	before := user.HP
	log = moveTurn(t, s, 1, 0)
	if user.HP >= before {
		t.Errorf("a failed Imprison must not seal anything (%d -> %d); log: %v", before, user.HP, logTexts(log))
	}
}

// --- Encore's PP checks (knowsMoveWithPP) ---

// TestEncoreMoveFailsOnAPPlessLastMove pins the canon rule that Encore cannot
// lock a target into a move it can no longer use: if the move it just used is
// out of PP, Encore fails.
//
// Both halves run the identical script and differ only in the PP the move
// started with, so the failure can only be about PP. Without that check Encore
// would force a target into a move with nothing left, which either soft-locks
// the turn or silently rewrites it into Struggle.
func TestEncoreMoveFailsOnAPPlessLastMove(t *testing.T) {
	// Mew (Speed 100) is faster than Chansey (Speed 50) and Chansey's Attack is
	// low enough that its Tackle never threatens the battle.
	encoreAfterTackleWith := func(pp int) (*BattleState, []LogLine) {
		s := battleWithMoves(t, 151, []string{"encore", "splash"}, 113, []string{"tackle", "splash"})
		s.Active(1).Moves[0].PP = pp
		moveTurn(t, s, 1, 0) // the foe uses Tackle, spending one PP
		return s, moveTurn(t, s, 0, 1)
	}

	// One PP, now spent: the move is unusable, so Encore fails.
	spent, log := encoreAfterTackleWith(1)
	if spent.Active(1).Moves[0].PP != 0 {
		t.Fatalf("setup: Tackle should be out of PP, has %d", spent.Active(1).Moves[0].PP)
	}
	if spent.Active(1).Volatiles.Encore != nil {
		t.Errorf("Encore should fail on a move with no PP left; got %+v", spent.Active(1).Volatiles.Encore)
	}
	if !logHas(log, "But it failed") {
		t.Errorf("Encore on a PP-less move should fail loudly; log: %v", logTexts(log))
	}

	// Two PP, one spent: same script, and now Encore takes.
	left, log := encoreAfterTackleWith(2)
	if left.Active(1).Volatiles.Encore == nil {
		t.Errorf("Encore should take while the move still has PP; log: %v", logTexts(log))
	}
}

// TestEncoreMoveEndsWhenTheEncoredMoveRunsOutOfPP pins the canon rule that an
// Encore breaks early once the move it locked has no PP left, before its three
// turns are up — and that the target is free to choose again on the next turn.
//
// The two halves are the same script with a different starting PP, so the early
// break can only be the PP running out. This is the counterpart to the check
// above: one refuses to start an Encore that cannot be honored, the other ends
// one that no longer can be.
func TestEncoreMoveEndsWhenTheEncoredMoveRunsOutOfPP(t *testing.T) {
	play := func(pp int) (*BattleState, []LogLine) {
		s := battleWithMoves(t, 151, []string{"encore", "splash"}, 113, []string{"tackle", "splash"})
		s.Active(1).Moves[0].PP = pp
		moveTurn(t, s, 1, 0)           // foe Tackles: its last move is now Tackle
		return s, moveTurn(t, s, 0, 0) // Mew Encores first, foe Tackles again
	}

	// Two PP, both spent: the Encore is armed this turn and breaks at the end
	// of it, well inside its three-turn timer.
	drained, log := play(2)
	if drained.Active(1).Moves[0].PP != 0 {
		t.Fatalf("setup: Tackle should be out of PP, has %d", drained.Active(1).Moves[0].PP)
	}
	if !logHas(log, "received an encore") {
		t.Fatalf("setup: Encore never took; log: %v", logTexts(log))
	}
	if drained.Active(1).Volatiles.Encore != nil {
		t.Errorf("Encore should end when the encored move runs out of PP; got %+v",
			drained.Active(1).Volatiles.Encore)
	}
	if !logHas(log, "encore ended") {
		t.Errorf("missing the encore-ended line; log: %v", logTexts(log))
	}
	// Freed: the next turn's Splash is not redirected back into Tackle.
	log = moveTurn(t, drained, 1, 1)
	if logHas(log, "must use") {
		t.Errorf("the target should be free to pick again; log: %v", logTexts(log))
	}

	// Plenty of PP, same script: the Encore is still running and still forcing.
	held, log := play(10)
	if held.Active(1).Volatiles.Encore == nil {
		t.Fatalf("control: Encore should still be running with PP to spare; log: %v", logTexts(log))
	}
	log = moveTurn(t, held, 1, 1)
	if !logHas(log, "must use") {
		t.Errorf("control: a live Encore should refuse a different move; log: %v", logTexts(log))
	}
}
