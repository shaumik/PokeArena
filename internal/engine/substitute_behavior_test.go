package engine

import (
	"fmt"
	"testing"

	"pokearena/internal/domain"
)

// substitute_behavior_test.go plays REAL BATTLES for a set of mechanics that
// were previously only pinned by tests calling unexported engine internals
// (applyVolatile, dealDamage, applyStatusMove, tickLockedMove, ...). Those
// tests describe the rules correctly but they are not portable: a port of this
// engine to another language has no applyVolatile to call, so a test written
// against it cannot be translated. Everything in this file goes through the
// front door — NewBattleFromPicks + ResolveTurn + the log — so the same
// scenario can be re-expressed in any implementation of the same game.
//
// Two habits are used throughout, and both are deliberate:
//
//   - Random outcomes are MEASURED over many seeds, never pinned to one lucky
//     seed. "Outrage lasts 2 or 3 turns" is checked by running 60 battles and
//     asserting every run landed in {2,3} and that both values were seen —
//     which fails for a duration that is always 2, always 3, or sometimes 4.
//     A single hand-picked seed would pin Go's RNG, not the game's rule.
//   - Damage comparisons read the "X took N damage." line out of the log
//     rather than an HP delta, and take the min/max over many seeds. The
//     damage roll spans 85%-100% (a 1.176x band), so a 1.3x or 0.5x modifier
//     separates the two bands completely and can be asserted without knowing
//     a single exact number.

// --- fixtures -------------------------------------------------------------

// mon is a one-line TeamPick. ability "" means "run no ability at all" (see
// duel); item "" means empty-handed; the moves are the exact slots, in order.
// Going through picks rather than NewBattle's dex-number teams is what lets a
// test hand a species precisely the moves the mechanic needs.
func mon(dexNo int, ability, item string, moves ...string) TeamPick {
	return TeamPick{DexNo: dexNo, Ability: ability, Item: item, MoveIDs: moves}
}

// duel builds a battle from explicit picks and blanks every ability slot the
// pick did not name.
//
// Blanking is the same discipline berryBattle uses: a species' default ability
// is noise in a mechanics test, and here it is worse than noise. Clefable's
// Cute Charm infatuates the Dragonite locked into a contact rampage and ends
// the lock early; Golem's Sturdy answers a one-hit KO before the test has
// asked it to; Snorlax's Immunity refuses a status the terrain test is trying
// to land. So: a pick with no ability runs bare, and a pick that names one
// runs exactly that one and nothing else.
func duel(t *testing.T, seed uint64, left, right []TeamPick) (*domain.Dex, *BattleState) {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattleFromPicks(d, "b", "P1", left, "P2", right, seed)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	for side, picks := range [][]TeamPick{left, right} {
		for i := range picks {
			if picks[i].Ability == "" {
				s.Sides[side].Team[i].Ability = AbilityNone
			}
		}
	}
	return d, s
}

// slots is the pair of "use move slot N" actions for one turn.
func slots(left, right int) [2]Action {
	return [2]Action{{Kind: ActionMove, Index: left}, {Kind: ActionMove, Index: right}}
}

// substituteAbsorbed pulls the figure out of the "<name>'s substitute took the
// damage! (-N)" line — how much the doll actually soaked, which is not always
// the same as how hard it was hit.
func substituteAbsorbed(log []LogLine, name string) (int, bool) {
	for _, l := range log {
		var n int
		if _, err := fmt.Sscanf(l.Text, name+"'s substitute took the damage! (-%d)", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

// damageTaken pulls the figure out of the engine's "<name> took N damage."
// line. Read from the log rather than from an HP delta on purpose: end-of-turn
// residuals (Grassy Terrain's 1/16 heal, sandstorm chip, Leftovers) move HP
// too, and a damage test that silently measures "damage minus a heal" is a
// test that stops meaning what its name says the moment a residual changes.
func damageTaken(log []LogLine, name string) (int, bool) {
	for _, l := range log {
		var n int
		if _, err := fmt.Sscanf(l.Text, name+" took %d damage.", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

// hitSpread runs the same two-turn scenario over `seeds` seeds and reports the
// smallest and largest damage the turn-2 attack dealt to the defender. Side 0
// uses move slot 0 on turn 1 (the field-setter, or Splash for the control) and
// slot 1 on turn 2 (the attack); side 1 splashes throughout.
//
// Critical hits are dropped from the sample rather than seeded away: a crit
// multiplies the damage and would swamp the 85%-100% roll band that every
// comparison in this file relies on.
func hitSpread(t *testing.T, seeds int, atk, def TeamPick, defName string) (lo, hi int) {
	t.Helper()
	lo, hi = -1, -1
	for seed := uint64(1); seed <= uint64(seeds); seed++ {
		d, s := duel(t, seed, []TeamPick{atk}, []TeamPick{def})
		ResolveTurn(d, s, slots(0, 0))
		log := ResolveTurn(d, s, slots(1, 0))
		if logHas(log, "A critical hit!") {
			continue
		}
		n, ok := damageTaken(log, defName)
		if !ok {
			t.Fatalf("seed %d: no damage line for %s; log %v", seed, defName, logTexts(log))
		}
		if lo < 0 || n < lo {
			lo = n
		}
		if n > hi {
			hi = n
		}
	}
	if lo < 0 {
		t.Fatalf("every one of the %d seeds critted — no clean damage sample", seeds)
	}
	return lo, hi
}

// Species used below, by Pokédex number, so the fixtures read as names.
const (
	dexVenusaur  = 3
	dexCharizard = 6
	dexClefable  = 36
	dexGolbat    = 42
	dexAlakazam  = 65
	dexGolem     = 76
	dexGengar    = 94
	dexPinsir    = 127
	dexLapras    = 131
	dexJolteon   = 135
	dexSnorlax   = 143
	dexZapdos    = 145
	dexDragonite = 149
	dexMewtwo    = 150
)

// --- Substitute: setup (applySubstituteSetup) -----------------------------

// TestBattleSubstitutePaysQuarterMaxHPForItsDoll: the canonical price of a
// Substitute is MaxHP/4 off the user, and the doll it stands up has exactly
// the HP that was spent — a doll whose durability is unrelated to the price
// would be a different move. Integer division: the remainder stays with the
// user. Played as a real turn so the whole route from "the player chose
// Substitute" to "there is a doll" is what's under test, rather than the
// volatile handler in isolation.
func TestBattleSubstitutePaysQuarterMaxHPForItsDoll(t *testing.T) {
	d, s := duel(t, 1,
		[]TeamPick{mon(dexSnorlax, "", "", "substitute", "splash")},
		[]TeamPick{mon(dexSnorlax, "", "", "splash")})
	user := s.Active(0)
	full, cost := user.HP, user.MaxHP/4

	log := ResolveTurn(d, s, slots(0, 0))

	if user.Volatiles.Substitute == nil {
		t.Fatalf("no doll after using Substitute; log %v", logTexts(log))
	}
	if got := full - user.HP; got != cost {
		t.Errorf("Substitute cost %d HP, want MaxHP/4 = %d", got, cost)
	}
	if got := user.Volatiles.Substitute.HP; got != cost {
		t.Errorf("doll HP = %d, want the %d HP that was spent on it", got, cost)
	}
	if got := user.Volatiles.Substitute.MaxHP; got != cost {
		t.Errorf("doll MaxHP = %d, want %d", got, cost)
	}
	if !logHas(log, "put up a substitute") {
		t.Errorf("no setup line in the log: %v", logTexts(log))
	}
}

// TestBattleSubstitutePriceIsAQuarterOfMaxNotOfCurrentHP: the fraction is read
// off MAX HP, so a damaged Pokémon pays exactly what a healthy one pays and
// gets exactly the same doll. Reading current HP instead is invisible while
// everything is at full health — which is why this test takes a hit first —
// and it would quietly make Substitute cheaper and flimsier the longer a game
// went on, turning the engine's most important stall tool into a different
// move entirely.
func TestBattleSubstitutePriceIsAQuarterOfMaxNotOfCurrentHP(t *testing.T) {
	d, s := duel(t, 1,
		[]TeamPick{mon(dexSnorlax, "", "", "substitute", "splash")},
		[]TeamPick{mon(dexSnorlax, "", "", "splash", "tackle")})
	user := s.Active(0)
	cost := user.MaxHP / 4

	// Turn 1: the user does nothing and takes a Tackle, so it is no longer at
	// full HP when it pays for the doll.
	ResolveTurn(d, s, slots(1, 1))
	hurt := user.HP
	if hurt >= user.MaxHP {
		t.Fatalf("setup: the user was supposed to be damaged first (HP %d/%d)", hurt, user.MaxHP)
	}
	if hurt/4 == cost {
		t.Fatalf("setup: %d damage was not enough to tell MaxHP/4 from HP/4", user.MaxHP-hurt)
	}

	ResolveTurn(d, s, slots(0, 0))

	if user.Volatiles.Substitute == nil {
		t.Fatalf("no doll after using Substitute at %d/%d HP", hurt, user.MaxHP)
	}
	if got := hurt - user.HP; got != cost {
		t.Errorf("a damaged user paid %d HP, want MaxHP/4 = %d (it paid %d — a quarter of its CURRENT HP)",
			got, cost, hurt/4)
	}
	if got := user.Volatiles.Substitute.HP; got != cost {
		t.Errorf("a damaged user's doll has %d HP, want MaxHP/4 = %d", got, cost)
	}
}

// TestBattleSecondSubstituteFailsWhileTheFirstStands: Substitute into your own
// live Substitute is a wasted turn, not a free re-roll — it must not charge
// the HP again and must not refresh the doll. The failure mode this guards is
// an infinite-stall loop: a user that could top its doll back up every turn
// while chipping with residuals is unbeatable by anything that cannot break
// MaxHP/4 in one hit.
func TestBattleSecondSubstituteFailsWhileTheFirstStands(t *testing.T) {
	d, s := duel(t, 1,
		[]TeamPick{mon(dexSnorlax, "", "", "substitute")},
		[]TeamPick{mon(dexSnorlax, "", "", "splash")})
	user := s.Active(0)

	ResolveTurn(d, s, slots(0, 0))
	hpAfterSetup := user.HP
	dollAfterSetup := user.Volatiles.Substitute.HP

	log := ResolveTurn(d, s, slots(0, 0))

	if user.HP != hpAfterSetup {
		t.Errorf("the failed second Substitute still charged HP: %d → %d", hpAfterSetup, user.HP)
	}
	if got := user.Volatiles.Substitute.HP; got != dollAfterSetup {
		t.Errorf("the failed second Substitute refreshed the doll: %d → %d", dollAfterSetup, got)
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("a duplicate Substitute must say so out loud; log %v", logTexts(log))
	}
}

// TestBattleSubstituteFailsWhenTheCostWouldFaintTheUser: you may not kill
// yourself to put up a doll. At exactly MaxHP/4 the payment would leave 0 HP,
// so the move fails outright and the user keeps every point of HP. Canon is
// strict here (HP must be GREATER than the cost), and getting the comparison
// off by one is a suicide bug that a "cost is deducted correctly" test cannot
// see.
func TestBattleSubstituteFailsWhenTheCostWouldFaintTheUser(t *testing.T) {
	d, s := duel(t, 1,
		[]TeamPick{mon(dexSnorlax, "", "", "substitute")},
		[]TeamPick{mon(dexSnorlax, "", "", "splash")})
	user := s.Active(0)
	user.HP = user.MaxHP / 4 // exactly the price

	log := ResolveTurn(d, s, slots(0, 0))

	if user.Volatiles.Substitute != nil {
		t.Errorf("a doll was raised at exactly the cost in HP")
	}
	if user.HP != user.MaxHP/4 {
		t.Errorf("HP changed on a failed Substitute: now %d", user.HP)
	}
	if user.Fainted {
		t.Errorf("Substitute fainted its own user")
	}
	if !logHas(log, "But it failed!") {
		t.Errorf("missing fail line; log %v", logTexts(log))
	}
}

// --- Substitute: taking hits (applyDamageToSubstitute) ---------------------

// TestBattleSubstituteEatsTheHitAndTheHolderIsUntouched: the whole point of
// the doll is that the holder's HP does not move while it stands. Two turns,
// because that is how it happens in a game: the doll goes up on one turn and
// is struck on the next.
func TestBattleSubstituteEatsTheHitAndTheHolderIsUntouched(t *testing.T) {
	d, s := duel(t, 1,
		[]TeamPick{mon(dexSnorlax, "", "", "substitute", "splash")},
		[]TeamPick{mon(dexSnorlax, "", "", "splash", "tackle")})
	holder := s.Active(0)

	ResolveTurn(d, s, slots(0, 0))
	hpBehindTheDoll := holder.HP
	dollBefore := holder.Volatiles.Substitute.HP

	log := ResolveTurn(d, s, slots(1, 1))

	if holder.HP != hpBehindTheDoll {
		t.Errorf("holder HP moved behind a live doll: %d → %d", hpBehindTheDoll, holder.HP)
	}
	if holder.Volatiles.Substitute == nil {
		t.Fatalf("doll broke to one Tackle it should have survived; log %v", logTexts(log))
	}
	if got := holder.Volatiles.Substitute.HP; got >= dollBefore {
		t.Errorf("doll absorbed nothing: %d → %d", dollBefore, got)
	}
	if !logHas(log, "substitute took the damage") {
		t.Errorf("the redirect must be announced; log %v", logTexts(log))
	}
}

// TestBattleBreakingHitDoesNotOverflowOntoTheHolder: Gen 5+ semantics — a hit
// that overshoots the doll's remaining HP breaks it and the surplus is
// DISCARDED. Older generations leaked the overflow through, and a port that
// reinvents that leak turns Substitute from a hard shield into a speed bump.
//
// The doll is also only ever credited with what it could actually take: the
// announced figure is capped at the doll's remaining HP, not the raw damage
// roll. That figure is what the engine hands back as "damage dealt" for drain
// and recoil accounting, so an uncapped number would let a drain move heal
// off HP that never existed.
//
// Mewtwo's Psychic deals more than a Snorlax doll's 58 HP on every roll, so
// the assertion holds for every seed rather than a chosen one — which is what
// the loop is checking.
func TestBattleBreakingHitDoesNotOverflowOntoTheHolder(t *testing.T) {
	for seed := uint64(1); seed <= 20; seed++ {
		d, s := duel(t, seed,
			[]TeamPick{mon(dexMewtwo, "", "", "splash", "psychic")},
			[]TeamPick{mon(dexSnorlax, "", "", "substitute", "splash")})
		holder := s.Active(1)

		ResolveTurn(d, s, slots(0, 0))
		hpBehindTheDoll := holder.HP
		dollBefore := holder.Volatiles.Substitute.HP

		log := ResolveTurn(d, s, slots(1, 1))

		if holder.Volatiles.Substitute != nil {
			t.Fatalf("seed %d: doll survived a hit far bigger than itself", seed)
		}
		if holder.HP != hpBehindTheDoll {
			t.Fatalf("seed %d: overflow from the breaking hit reached the holder: %d → %d",
				seed, hpBehindTheDoll, holder.HP)
		}
		absorbed, ok := substituteAbsorbed(log, holder.Name)
		if !ok {
			t.Fatalf("seed %d: no absorb line; log %v", seed, logTexts(log))
		}
		if absorbed != dollBefore {
			t.Fatalf("seed %d: the doll was credited with %d absorbed, want its whole %d HP and no more",
				seed, absorbed, dollBefore)
		}
		if !logHas(log, "substitute faded") {
			t.Fatalf("seed %d: the break must be announced; log %v", seed, logTexts(log))
		}
	}
}

// --- Substitute: transparency (bypassesSubstitute) ------------------------

// TestBattleSoundMoveIgnoresTheSubstitute: sound-flagged moves treat the doll
// as if it were not there — the holder takes the damage and the doll is not
// even scratched. This is the counterplay that stops Substitute from being an
// unconditional wall, so it has to be true for every roll, not most of them.
func TestBattleSoundMoveIgnoresTheSubstitute(t *testing.T) {
	for seed := uint64(1); seed <= 20; seed++ {
		d, s := duel(t, seed,
			[]TeamPick{mon(dexSnorlax, "", "", "splash", "hyper-voice")},
			[]TeamPick{mon(dexSnorlax, "", "", "substitute", "splash")})
		holder := s.Active(1)

		ResolveTurn(d, s, slots(0, 0))
		hpBehindTheDoll := holder.HP
		dollBefore := holder.Volatiles.Substitute.HP

		log := ResolveTurn(d, s, slots(1, 1))

		if holder.HP >= hpBehindTheDoll {
			t.Fatalf("seed %d: Hyper Voice did not reach the holder (HP %d → %d); log %v",
				seed, hpBehindTheDoll, holder.HP, logTexts(log))
		}
		if holder.Volatiles.Substitute == nil {
			t.Fatalf("seed %d: a sound move destroyed the doll instead of ignoring it", seed)
		}
		if got := holder.Volatiles.Substitute.HP; got != dollBefore {
			t.Fatalf("seed %d: a sound move chipped the doll it should pass through: %d → %d",
				seed, dollBefore, got)
		}
		if logHas(log, "substitute took the damage") {
			t.Fatalf("seed %d: the doll was credited with a hit it never took; log %v",
				seed, logTexts(log))
		}
	}
}

// TestBattleInfiltratorStrikesThroughTheSubstitute: Infiltrator makes foe
// substitutes transparent to EVERY move its holder uses, not just to the
// sound-flagged ones. The bare-ability control in the same test is what makes
// it real: the identical Golbat with no ability puts its Tackle into the doll,
// so the only difference between the two halves is Infiltrator itself.
func TestBattleInfiltratorStrikesThroughTheSubstitute(t *testing.T) {
	run := func(ability string) (holderLost bool, dollChipped bool) {
		t.Helper()
		d, s := duel(t, 1,
			[]TeamPick{mon(dexGolbat, ability, "", "splash", "tackle")},
			[]TeamPick{mon(dexSnorlax, "", "", "substitute", "splash")})
		holder := s.Active(1)
		ResolveTurn(d, s, slots(0, 0))
		hpBehindTheDoll := holder.HP
		dollBefore := holder.Volatiles.Substitute.HP
		ResolveTurn(d, s, slots(1, 1))
		return holder.HP < hpBehindTheDoll, holder.Volatiles.Substitute.HP < dollBefore
	}

	if hurt, chipped := run(""); hurt || !chipped {
		t.Errorf("baseline: a plain Tackle should hit the doll and spare the holder "+
			"(holder hurt=%v, doll chipped=%v)", hurt, chipped)
	}
	if hurt, chipped := run("infiltrator"); !hurt || chipped {
		t.Errorf("Infiltrator: Tackle should reach the holder and leave the doll alone "+
			"(holder hurt=%v, doll chipped=%v)", hurt, chipped)
	}
}

// --- Rampage moves (lockedMoveDuration, tickLockedMove) -------------------

// rampageRun plays one Outrage rampage to its end and reports how many times
// Outrage was actually used and whether fatigue confusion landed. Dragonite's
// Outrage into a pure-Fairy Clefable deals no damage, so nobody faints and the
// battle stays in the choosing phase for the whole lock — and it is canon that
// a rampage into an immune target still locks and still fatigues.
//
// Clefable runs bare deliberately: Cute Charm would infatuate the Dragonite on
// contact, and an infatuated user that fails to act clears the lock early,
// which would corrupt the duration sample this measures.
func rampageRun(t *testing.T, seed uint64, item string) (uses int, fatigued bool, last []LogLine) {
	t.Helper()
	d, s := duel(t, seed,
		[]TeamPick{mon(dexDragonite, "", item, "outrage")},
		[]TeamPick{mon(dexClefable, "", "", "splash")})
	for turn := 1; turn <= 6 && !fatigued; turn++ {
		if s.Phase != PhaseChoosing {
			t.Fatalf("seed %d: battle left the choosing phase mid-rampage", seed)
		}
		last = ResolveTurn(d, s, slots(0, 0))
		if logHas(last, "used Outrage") {
			uses++
		}
		if logHas(last, "confused due to fatigue") {
			fatigued = true
		}
	}
	return uses, fatigued, last
}

// TestBattleRampageRunsTwoOrThreeTurnsAcrossManySeeds: the rampage duration is
// a uniform 2-or-3 roll. That is a DISTRIBUTION, so it is measured as one —
// sixty battles, every one of which must land in {2,3}, and both outcomes must
// actually show up. A duration hard-wired to 2, hard-wired to 3, or allowed to
// reach 4 all fail here, and none of those would be caught by a single battle
// on a hand-picked seed.
//
// The count also has to be the number of times the move was USED: an
// off-by-one in the tick would either fatigue the user a turn early (2 uses
// becomes 1) or hand it a free extra turn of a 120-BP move.
func TestBattleRampageRunsTwoOrThreeTurnsAcrossManySeeds(t *testing.T) {
	const runs = 60
	seen := map[int]int{}
	for seed := uint64(1); seed <= runs; seed++ {
		uses, fatigued, log := rampageRun(t, seed, "")
		if !fatigued {
			t.Fatalf("seed %d: the rampage never ended in fatigue (%d uses); last log %v",
				seed, uses, logTexts(log))
		}
		if uses < 2 || uses > 3 {
			t.Fatalf("seed %d: Outrage was used %d times, want 2 or 3", seed, uses)
		}
		seen[uses]++
	}
	if seen[2] == 0 || seen[3] == 0 {
		t.Errorf("over %d rampages the duration was always %v — a 2-or-3 roll must produce both",
			runs, seen)
	}
}

// TestBattleRampageFatigueIsAnsweredByAPersimBerry: the confusion a rampage
// ends in is real confusion, so a held confusion cure has to answer it. The
// bug this guards is specific and was real: fatigue sets the volatile directly
// instead of going through the normal confusion route, so unless the cure is
// invoked at that point too, a Persim/Lum holder walks out of its own Outrage
// confused for 2-5 turns with an unused berry in hand.
//
// Persim rather than Lum because Persim cures confusion and nothing else —
// there is no other status in this battle for it to have reacted to.
func TestBattleRampageFatigueIsAnsweredByAPersimBerry(t *testing.T) {
	for seed := uint64(1); seed <= 15; seed++ {
		d, s := duel(t, seed,
			[]TeamPick{mon(dexDragonite, "", "persim-berry", "outrage")},
			[]TeamPick{mon(dexClefable, "", "", "splash")})
		var log []LogLine
		fatigued := false
		for turn := 1; turn <= 6 && !fatigued; turn++ {
			log = ResolveTurn(d, s, slots(0, 0))
			fatigued = logHas(log, "confused due to fatigue")
		}
		if !fatigued {
			t.Fatalf("seed %d: rampage never fatigued", seed)
		}
		user := s.Active(0)
		if user.Volatiles.Confusion != nil {
			t.Fatalf("seed %d: the Persim Berry did not cure fatigue confusion", seed)
		}
		if user.Item != ItemNone {
			t.Fatalf("seed %d: the berry cured the confusion but was never eaten", seed)
		}
		if !logHas(log, "snapped out of its confusion") {
			t.Fatalf("seed %d: the cure must be announced; log %v", seed, logTexts(log))
		}
	}
}

// --- Explosion (applySelfDestruct) ----------------------------------------

// TestBattleExplosionKnocksOutItsOwnUser: the cost of a 250-BP move is the
// user's life, paid every time — the user hits 0 HP and faints in the same
// turn, and the battle moves to the replace phase asking that side for a new
// Pokémon. A self-destruct that forgets to charge its user is the strongest
// move in the game for free.
func TestBattleExplosionKnocksOutItsOwnUser(t *testing.T) {
	d, s := duel(t, 1,
		[]TeamPick{mon(dexSnorlax, "", "", "explosion"), mon(dexCharizard, "", "", "splash")},
		[]TeamPick{mon(dexSnorlax, "", "", "splash"), mon(dexCharizard, "", "", "splash")})
	user := s.Active(0)
	foe := s.Active(1)
	foeFull := foe.HP

	log := ResolveTurn(d, s, slots(0, 0))

	if !user.Fainted || user.HP != 0 {
		t.Errorf("the user survived its own Explosion: HP=%d fainted=%v", user.HP, user.Fainted)
	}
	if !logHas(log, "exploded!") {
		t.Errorf("the detonation must be announced; log %v", logTexts(log))
	}
	if foe.HP >= foeFull {
		t.Errorf("Explosion did no damage to the foe: %d → %d", foeFull, foe.HP)
	}
	if s.Phase != PhaseReplace || !s.Replace[0] {
		t.Errorf("phase=%v replace=%v, want the exploding side to owe a replacement",
			s.Phase, s.Replace)
	}
}

// TestBattleExplosionStillDetonatesAgainstAnImmuneGhost: a Normal-type
// Explosion cannot touch a Ghost — and the user blows up anyway. The
// self-destruct is a cost of USING the move, not a consequence of it landing,
// which is exactly why the tail has to run on the immune path as well as the
// hit path. Getting this wrong makes Explosion risk-free against the one type
// it should never be aimed at.
func TestBattleExplosionStillDetonatesAgainstAnImmuneGhost(t *testing.T) {
	d, s := duel(t, 1,
		[]TeamPick{mon(dexSnorlax, "", "", "explosion"), mon(dexCharizard, "", "", "splash")},
		[]TeamPick{mon(dexGengar, "", "", "splash")})
	user := s.Active(0)
	ghost := s.Active(1)
	ghostFull := ghost.HP

	log := ResolveTurn(d, s, slots(0, 0))

	if ghost.HP != ghostFull {
		t.Errorf("a Ghost took Normal-type Explosion damage: %d → %d", ghostFull, ghost.HP)
	}
	if !logHas(log, "It doesn't affect") {
		t.Errorf("the immunity must be announced; log %v", logTexts(log))
	}
	if !user.Fainted || user.HP != 0 {
		t.Errorf("the user did not detonate on an immune target: HP=%d fainted=%v",
			user.HP, user.Fainted)
	}
	if !logHas(log, "exploded!") {
		t.Errorf("the detonation must be announced even on the immune path; log %v",
			logTexts(log))
	}
}

// --- One-hit KO moves (resolveOHKOImmunity) -------------------------------

// TestBattleSheerColdCannotTouchAnIceType: Sheer Cold is refused outright by
// Ice-types. The type chart does NOT give this — Ice vs Ice is an ordinary
// 0.5x matchup — so it is a rule of the move, and an engine that leans on the
// chart alone will happily let a Lapras be deleted by a 30% roll.
//
// Asserted as "never, over 40 battles" on the Ice side and "at least once,
// over the same 40" on a non-Ice control: an immunity that is really just an
// accuracy problem would fail the control.
func TestBattleSheerColdCannotTouchAnIceType(t *testing.T) {
	const runs = 40
	for seed := uint64(1); seed <= runs; seed++ {
		d, s := duel(t, seed,
			[]TeamPick{mon(dexSnorlax, "", "", "sheer-cold")},
			[]TeamPick{mon(dexLapras, "", "", "splash")})
		ice := s.Active(1)
		log := ResolveTurn(d, s, slots(0, 0))
		if ice.HP != ice.MaxHP || ice.Fainted {
			t.Fatalf("seed %d: Sheer Cold reached an Ice-type (HP %d/%d, fainted=%v); log %v",
				seed, ice.HP, ice.MaxHP, ice.Fainted, logTexts(log))
		}
		if !logHas(log, "It doesn't affect") {
			t.Fatalf("seed %d: the refusal must be announced; log %v", seed, logTexts(log))
		}
	}
	kos := 0
	for seed := uint64(1); seed <= runs; seed++ {
		d, s := duel(t, seed,
			[]TeamPick{mon(dexSnorlax, "", "", "sheer-cold")},
			[]TeamPick{mon(dexSnorlax, "", "", "splash")})
		ResolveTurn(d, s, slots(0, 0))
		if s.Active(1).Fainted {
			kos++
		}
	}
	if kos == 0 {
		t.Errorf("control: Sheer Cold never landed on a non-Ice target in %d battles — "+
			"the Ice half above proves nothing if the move never works at all", runs)
	}
}

// TestBattleSturdyRefusesAOneHitKO: Sturdy (Gen 5+) does not merely survive a
// one-hit KO at 1 HP the way it clamps an ordinary overkill hit — it refuses
// the move before it is even rolled, and says so. The distinction matters: a
// Sturdy holder that "survives at 1 HP" can be finished by any chip, while one
// that refuses the move is untouched.
//
// The Mold Breaker control is what proves the immunity is Sturdy's and not
// something else about Golem: the same Fissure from a mold-breaking user goes
// through and KOs.
func TestBattleSturdyRefusesAOneHitKO(t *testing.T) {
	const runs = 40
	for seed := uint64(1); seed <= runs; seed++ {
		d, s := duel(t, seed,
			[]TeamPick{mon(dexSnorlax, "", "", "fissure")},
			[]TeamPick{mon(dexGolem, "sturdy", "", "splash")})
		wall := s.Active(1)
		log := ResolveTurn(d, s, slots(0, 0))
		if wall.Fainted || wall.HP != wall.MaxHP {
			t.Fatalf("seed %d: Fissure got through Sturdy (HP %d/%d, fainted=%v)",
				seed, wall.HP, wall.MaxHP, wall.Fainted)
		}
		if !logHas(log, "unaffected by the one-hit KO") {
			t.Fatalf("seed %d: Sturdy must announce the refusal; log %v", seed, logTexts(log))
		}
		if !wall.AbilityRevealed {
			t.Fatalf("seed %d: an ability that visibly acted must be revealed to the foe", seed)
		}
	}
	kos := 0
	for seed := uint64(1); seed <= runs; seed++ {
		d, s := duel(t, seed,
			[]TeamPick{mon(dexPinsir, "mold-breaker", "", "fissure")},
			[]TeamPick{mon(dexGolem, "sturdy", "", "splash")})
		ResolveTurn(d, s, slots(0, 0))
		if s.Active(1).Fainted {
			kos++
		}
	}
	if kos == 0 {
		t.Errorf("control: Mold Breaker never got a Fissure through Sturdy in %d battles — "+
			"Sturdy's refusal has to be an ability, not a blanket immunity", runs)
	}
}

// --- Terrain: status gating (terrainBlocksStatus) -------------------------

// terrainStatusRun sets a terrain on turn 1 and aims a status move at the foe
// on turn 2, over many seeds, and reports how often the status stuck. Setter
// "splash" means "no terrain" — the control arm.
func terrainStatusRun(t *testing.T, runs int, setter, statusMove string, target TeamPick) int {
	t.Helper()
	stuck := 0
	for seed := uint64(1); seed <= uint64(runs); seed++ {
		d, s := duel(t, seed,
			[]TeamPick{mon(dexSnorlax, "", "", setter, statusMove)},
			[]TeamPick{target})
		ResolveTurn(d, s, slots(0, 0))
		ResolveTurn(d, s, slots(1, 0))
		if s.Active(1).Status != StatusNone {
			stuck++
		}
	}
	return stuck
}

// TestBattleMistyTerrainRefusesEveryStatusOnAGroundedTarget: Misty Terrain is
// a blanket status shield for anything standing in it — not a sleep-only
// shield like Electric Terrain. Paralysis is the case that separates the two,
// so it is the one asserted here; the control arm (same battle, no terrain)
// proves Thunder Wave lands perfectly well when the mist is not there.
func TestBattleMistyTerrainRefusesEveryStatusOnAGroundedTarget(t *testing.T) {
	const runs = 30
	grounded := mon(dexSnorlax, "", "", "splash")

	if stuck := terrainStatusRun(t, runs, "misty-terrain", "thunder-wave", grounded); stuck != 0 {
		t.Errorf("Thunder Wave paralyzed a grounded target through Misty Terrain %d/%d times", stuck, runs)
	}
	if stuck := terrainStatusRun(t, runs, "misty-terrain", "spore", grounded); stuck != 0 {
		t.Errorf("Spore put a grounded target to sleep through Misty Terrain %d/%d times", stuck, runs)
	}
	if stuck := terrainStatusRun(t, runs, "splash", "thunder-wave", grounded); stuck == 0 {
		t.Errorf("control: Thunder Wave never landed in %d terrain-free battles", runs)
	}
}

// TestBattleMistyTerrainDoesNotProtectAFloatingTarget: terrain only reaches
// what is standing on the ground. A Flying-type is out of the mist and takes
// the paralysis it would have taken on a bare field. Skipping the grounded
// check turns every terrain into a field-wide effect and quietly deletes the
// entire Flying/Levitate counterplay.
func TestBattleMistyTerrainDoesNotProtectAFloatingTarget(t *testing.T) {
	const runs = 30
	flyer := mon(dexCharizard, "", "", "splash")
	if stuck := terrainStatusRun(t, runs, "misty-terrain", "thunder-wave", flyer); stuck == 0 {
		t.Errorf("a Flying-type was shielded by Misty Terrain in all %d battles — "+
			"terrain must not reach an ungrounded target", runs)
	}
}

// TestBattleElectricTerrainRefusesSleepOnly: Electric Terrain keeps a grounded
// Pokémon awake and does nothing else. Both halves are needed: an engine that
// treats every terrain as a blanket status shield passes the sleep half and
// fails the paralysis half, and an engine that forgets the shield entirely
// fails the other way round.
func TestBattleElectricTerrainRefusesSleepOnly(t *testing.T) {
	const runs = 30
	grounded := mon(dexSnorlax, "", "", "splash")

	if stuck := terrainStatusRun(t, runs, "electric-terrain", "spore", grounded); stuck != 0 {
		t.Errorf("Spore slept a grounded target through Electric Terrain %d/%d times", stuck, runs)
	}
	if stuck := terrainStatusRun(t, runs, "electric-terrain", "thunder-wave", grounded); stuck == 0 {
		t.Errorf("Electric Terrain blocked paralysis in all %d battles — it blocks sleep only", runs)
	}
}

// --- Terrain: damage (terrainDamageMult) ----------------------------------

// TestBattleTerrainBoostsItsOwnTypeForAGroundedAttacker: each of the three
// offensive terrains gives a grounded attacker's matching-type moves a boost.
// The assertion needs no exact number: the damage roll spans 85%-100%, a
// 1.176x band, so a genuine boost lifts the whole boosted band clear above the
// unboosted one. If the multiplier were dropped, or applied to the wrong type,
// the two bands would overlap and this fails.
func TestBattleTerrainBoostsItsOwnTypeForAGroundedAttacker(t *testing.T) {
	const runs = 30
	cases := []struct {
		name     string
		terrain  string
		attack   string
		attacker TeamPick
		target   TeamPick
	}{
		{
			"electric", "electric-terrain", "thunderbolt",
			mon(dexJolteon, "", ""), mon(dexSnorlax, "", "", "splash"),
		},
		{
			"grassy", "grassy-terrain", "seed-bomb",
			mon(dexVenusaur, "", ""), mon(dexSnorlax, "", "", "splash"),
		},
		{
			"psychic", "psychic-terrain", "psychic",
			mon(dexAlakazam, "", ""), mon(dexSnorlax, "", "", "splash"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			boosted := c.attacker
			boosted.MoveIDs = []string{c.terrain, c.attack}
			plain := c.attacker
			plain.MoveIDs = []string{"splash", c.attack}

			bLo, bHi := hitSpread(t, runs, boosted, c.target, "Snorlax")
			pLo, pHi := hitSpread(t, runs, plain, c.target, "Snorlax")
			if bLo <= pHi {
				t.Errorf("%s terrain: boosted damage %d-%d does not sit above plain %d-%d",
					c.name, bLo, bHi, pLo, pHi)
			}
		})
	}
}

// TestBattleTerrainBoostSkipsAnAirborneAttacker: the boost is paid to the
// attacker for STANDING in the terrain, so a Flying-type gets nothing. Zapdos
// and Jolteon fire the same Thunderbolt; only the grounded one is boosted, and
// the airborne one's damage band must be identical with and without the
// terrain up.
func TestBattleTerrainBoostSkipsAnAirborneAttacker(t *testing.T) {
	const runs = 30
	target := mon(dexSnorlax, "", "", "splash")

	tLo, tHi := hitSpread(t, runs,
		mon(dexZapdos, "", "", "electric-terrain", "thunderbolt"), target, "Snorlax")
	nLo, nHi := hitSpread(t, runs,
		mon(dexZapdos, "", "", "splash", "thunderbolt"), target, "Snorlax")

	if tLo != nLo || tHi != nHi {
		t.Errorf("an airborne attacker was affected by Electric Terrain: %d-%d in terrain vs %d-%d without",
			tLo, tHi, nLo, nHi)
	}
}

// TestBattleMistyTerrainHalvesDragonMovesOnAGroundedTarget: the mist is a
// defensive effect here and it keys on the DEFENDER being grounded, not the
// attacker — Dragonite is Flying and still gets halved, because it is the
// Snorlax standing in the mist that matters. Mixing up which side the check
// reads is the easy bug, and it would leave this test's damage bands
// overlapping.
func TestBattleMistyTerrainHalvesDragonMovesOnAGroundedTarget(t *testing.T) {
	const runs = 30
	target := mon(dexSnorlax, "", "", "splash")

	mLo, mHi := hitSpread(t, runs,
		mon(dexDragonite, "", "", "misty-terrain", "dragon-claw"), target, "Snorlax")
	pLo, pHi := hitSpread(t, runs,
		mon(dexDragonite, "", "", "splash", "dragon-claw"), target, "Snorlax")

	if mHi >= pLo {
		t.Errorf("Misty Terrain did not halve Dragon Claw: %d-%d in mist vs %d-%d without",
			mLo, mHi, pLo, pHi)
	}
}

// TestBattleGrassyTerrainHalvesEarthquake: tall grass absorbs the ground
// shake, so Earthquake against a grounded target is halved. Note the damage is
// read from the log line rather than an HP delta — Grassy Terrain also heals
// every grounded Pokémon 1/16 at end of turn, and an HP-delta measurement
// would silently be reporting "damage minus that heal".
func TestBattleGrassyTerrainHalvesEarthquake(t *testing.T) {
	const runs = 30
	target := mon(dexSnorlax, "", "", "splash")

	gLo, gHi := hitSpread(t, runs,
		mon(dexVenusaur, "", "", "grassy-terrain", "earthquake"), target, "Snorlax")
	pLo, pHi := hitSpread(t, runs,
		mon(dexVenusaur, "", "", "splash", "earthquake"), target, "Snorlax")

	if gHi >= pLo {
		t.Errorf("Grassy Terrain did not halve Earthquake: %d-%d in grass vs %d-%d without",
			gLo, gHi, pLo, pHi)
	}
}

// --- Magic Room (applyMagicRoomSetter) ------------------------------------

// TestBattleMagicRoomSuspendsHeldItemsAndReCastingLiftsIt: Magic Room switches
// every held item on the field off, and — like Trick Room — casting it again
// while it is up switches them back on rather than failing. The three turns
// are the three states: item working, item suspended, item working again.
//
// The per-Pokémon mirror is asserted too. Item lookup has no battle state in
// hand at most of its call sites, so the field's Magic Room is mirrored onto
// each active; if a setter forgets to push that mirror, the room is up on the
// field and every item on the board keeps working.
func TestBattleMagicRoomSuspendsHeldItemsAndReCastingLiftsIt(t *testing.T) {
	d, s := duel(t, 1,
		[]TeamPick{mon(dexSnorlax, "", "", "tackle", "magic-room")},
		[]TeamPick{mon(dexSnorlax, "", "leftovers", "splash")})
	holder := s.Active(1)

	// Turn 1: a plain hit, then Leftovers pays out at end of turn.
	log := ResolveTurn(d, s, slots(0, 0))
	if !logHas(log, "restored") {
		t.Fatalf("baseline: Leftovers did not heal on a normal turn; log %v", logTexts(log))
	}
	hpBefore := holder.HP

	// Turn 2: the room goes up and the item goes quiet.
	log = ResolveTurn(d, s, slots(1, 0))
	if s.PseudoWeather.MagicRoom == nil {
		t.Fatalf("Magic Room was not raised; log %v", logTexts(log))
	}
	if !logHas(log, "held items lose their effects") {
		t.Errorf("raising Magic Room must be announced; log %v", logTexts(log))
	}
	for i := 0; i < 2; i++ {
		if !s.Active(i).Volatiles.MagicRoomHere {
			t.Errorf("side %d's active was not told the room is up", i)
		}
	}
	if holder.HP != hpBefore {
		t.Errorf("Leftovers healed inside Magic Room: %d → %d", hpBefore, holder.HP)
	}

	// Turn 3: casting it again takes the room back down, and the item resumes
	// in the same turn's residual phase.
	log = ResolveTurn(d, s, slots(1, 0))
	if s.PseudoWeather.MagicRoom != nil {
		t.Errorf("re-casting Magic Room should dismiss it, not refresh it")
	}
	if !logHas(log, "Magic Room wore off") {
		t.Errorf("dismissing Magic Room must be announced; log %v", logTexts(log))
	}
	for i := 0; i < 2; i++ {
		if s.Active(i).Volatiles.MagicRoomHere {
			t.Errorf("side %d's active still thinks the room is up", i)
		}
	}
	if holder.HP <= hpBefore {
		t.Errorf("Leftovers did not resume once the room was dismissed: %d → %d", hpBefore, holder.HP)
	}
}

// --- Weather defensive boosts (defenseMult) -------------------------------

// TestBattleSnowThickensAnIceTypesDefense: snow gives Ice-types +50% Defense —
// the modern replacement for hail's chip damage, and the entire reason to set
// snow at all. Physical only: the boost is Defense, not Special Defense.
func TestBattleSnowThickensAnIceTypesDefense(t *testing.T) {
	const runs = 30
	ice := mon(dexLapras, "", "", "splash")

	sLo, sHi := hitSpread(t, runs,
		mon(dexSnorlax, "", "", "snowscape", "tackle"), ice, "Lapras")
	cLo, cHi := hitSpread(t, runs,
		mon(dexSnorlax, "", "", "splash", "tackle"), ice, "Lapras")
	if sHi >= cLo {
		t.Errorf("snow did not thicken an Ice-type's Defense: %d-%d in snow vs %d-%d clear",
			sLo, sHi, cLo, cHi)
	}
}

// TestBattleSnowLeavesSpecialAttacksAndNonIceAlone: the two ways to
// over-apply the snow boost. A special move against the same Lapras is
// untouched (snow boosts Defense, not Sp. Def), and a physical move against a
// non-Ice target is untouched (the boost is a typing perk, not weather-wide
// bulk). Both are asserted as exact band equality, because "no change" is the
// claim.
func TestBattleSnowLeavesSpecialAttacksAndNonIceAlone(t *testing.T) {
	const runs = 30

	ice := mon(dexLapras, "", "", "splash")
	sLo, sHi := hitSpread(t, runs, mon(dexSnorlax, "", "", "snowscape", "psychic"), ice, "Lapras")
	cLo, cHi := hitSpread(t, runs, mon(dexSnorlax, "", "", "splash", "psychic"), ice, "Lapras")
	if sLo != cLo || sHi != cHi {
		t.Errorf("snow moved a SPECIAL hit on an Ice-type: %d-%d vs %d-%d clear", sLo, sHi, cLo, cHi)
	}

	plain := mon(dexSnorlax, "", "", "splash")
	sLo, sHi = hitSpread(t, runs, mon(dexSnorlax, "", "", "snowscape", "tackle"), plain, "Snorlax")
	cLo, cHi = hitSpread(t, runs, mon(dexSnorlax, "", "", "splash", "tackle"), plain, "Snorlax")
	if sLo != cLo || sHi != cHi {
		t.Errorf("snow thickened a NON-Ice target: %d-%d vs %d-%d clear", sLo, sHi, cLo, cHi)
	}
}

// TestBattleSandstormThickensARockTypesSpecialDefense: the mirror-image rule —
// sandstorm gives Rock-types +50% Special Defense. It is the other half of
// defenseMult and it targets the other stat, so an implementation that boosts
// one stat for both weathers passes one of these tests and fails the other.
func TestBattleSandstormThickensARockTypesSpecialDefense(t *testing.T) {
	const runs = 30
	rock := mon(dexGolem, "", "", "splash")

	sLo, sHi := hitSpread(t, runs,
		mon(dexSnorlax, "", "", "sandstorm", "psychic"), rock, "Golem")
	cLo, cHi := hitSpread(t, runs,
		mon(dexSnorlax, "", "", "splash", "psychic"), rock, "Golem")
	if sHi >= cLo {
		t.Errorf("sandstorm did not thicken a Rock-type's Sp. Def: %d-%d in sand vs %d-%d clear",
			sLo, sHi, cLo, cHi)
	}

	// And not the physical side: the sand boost is Sp. Def only.
	pLo, pHi := hitSpread(t, runs, mon(dexSnorlax, "", "", "sandstorm", "tackle"), rock, "Golem")
	qLo, qHi := hitSpread(t, runs, mon(dexSnorlax, "", "", "splash", "tackle"), rock, "Golem")
	if pLo != qLo || pHi != qHi {
		t.Errorf("sandstorm moved a PHYSICAL hit on a Rock-type: %d-%d vs %d-%d clear",
			pLo, pHi, qLo, qHi)
	}
}
