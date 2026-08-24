package engine

import "testing"

// yawn_gating_behavior_test.go pins the *refusals* of Yawn, which is where the
// move is interesting and where this engine had it backwards in two places at
// once. Yawn resolves in two stages — the move arms a countdown, and two
// end-of-turn ticks later the countdown asks for a Sleep — and every guard in
// the game lands on one stage, the other, or both. Getting that split wrong is
// invisible to a test that only checks "does the victim eventually fall
// asleep", because both stages usually agree.
//
// The three cases where they do not agree are all in here:
//
//   - Safeguard refuses the countdown but not the Sleep, so Safeguard-then-Yawn
//     and Yawn-then-Safeguard come out opposite ways round.
//   - Electric Terrain refuses both; Misty Terrain refuses only the Sleep, even
//     though both terrains refuse a plain sleep move. So the two terrains
//     disagree about whether Yawn itself fails.
//   - Insomnia / Vital Spirit / Leaf Guard refuse both, which means the move
//     has to re-make an ability check that inflictStatus would have made for it
//     two turns later.
//
// Everything here goes through NewBattleFromPicks + ResolveTurn (see
// substitute_behavior_test.go for the fixture helpers and for why), and every
// scenario is swept over a band of seeds rather than pinned to one: Yawn is
// deterministic but the moves around it share an RNG stream, and a rule that
// only holds for seed 7 is not a rule.

const yawnGateSeeds = 12

// yawnSafeguardFirst plays "Safeguard goes up, then the foe tries to Yawn
// through it" and reports whether the countdown was armed, whether the
// protected Pokémon was ever put to sleep, and whether the block was announced
// (the volatile branch of applyEffectFields logs "protected by Safeguard!"
// rather than the generic "But it failed!"). Splash fills the slots that must
// not do anything, so no result depends on who moved first.
func yawnSafeguardFirst(t *testing.T, seed uint64) (armed, slept, announced bool) {
	t.Helper()
	d, s := duel(t, seed,
		[]TeamPick{mon(dexSnorlax, "", "", "splash", "yawn")},
		[]TeamPick{mon(dexSnorlax, "", "", "safeguard", "splash")})
	ResolveTurn(d, s, slots(0, 0)) // Splash / Safeguard
	log := ResolveTurn(d, s, slots(1, 1))
	armed = s.Active(1).Volatiles.Yawn != nil
	announced = logHas(log, "protected by Safeguard")
	// Two more quiet turns: if a countdown were armed after all, this is where
	// it would collect. Checking the volatile alone would miss an engine that
	// armed nothing but slept the target from somewhere else.
	ResolveTurn(d, s, slots(0, 1))
	ResolveTurn(d, s, slots(0, 1))
	slept = s.Active(1).Status == StatusSleep
	return armed, slept, announced
}

// yawnBeforeSafeguard plays it the other way round: the Yawn lands on a bare
// field and the Safeguard goes up while the countdown is already running.
func yawnBeforeSafeguard(t *testing.T, seed uint64) bool {
	t.Helper()
	d, s := duel(t, seed,
		[]TeamPick{mon(dexSnorlax, "", "", "yawn", "splash")},
		[]TeamPick{mon(dexSnorlax, "", "", "splash", "safeguard")})
	ResolveTurn(d, s, slots(0, 0)) // Yawn / Splash — the countdown is armed
	ResolveTurn(d, s, slots(1, 1)) // Splash / Safeguard — and it expires here
	return s.Active(1).Status == StatusSleep
}

// TestBattleSafeguardRefusesYawnButNotAnAlreadyArmedYawn is the whole Safeguard
// rule, and it needs both halves or it is worse than nothing.
//
// Showdown's safeguard condition mentions yawn twice, in opposite directions:
// onTryAddVolatile refuses it alongside confusion, and onSetStatus exempts it
// by name. Reading only the exemption gives "Yawn bypasses Safeguard", which is
// what this engine used to say in two comments and implement in one predicate —
// and the half-right reading is stable under the obvious test, because a Yawn
// that lands before the Safeguard really does put its victim under. The
// exemption is not a license for Yawn to land; it is what stops a Safeguard put
// up *later* from canceling a countdown that is already running.
//
// So the pair of assertions is the test. An engine that gates neither stage
// fails the first half; an engine that gates both fails the second.
func TestBattleSafeguardRefusesYawnButNotAnAlreadyArmedYawn(t *testing.T) {
	for seed := uint64(1); seed <= yawnGateSeeds; seed++ {
		armed, slept, announced := yawnSafeguardFirst(t, seed)
		if armed {
			t.Fatalf("seed %d: Yawn armed its countdown through an active Safeguard", seed)
		}
		if slept {
			t.Fatalf("seed %d: a Pokémon behind its own Safeguard was put to sleep by Yawn", seed)
		}
		if !announced {
			t.Fatalf("seed %d: a Safeguard-refused Yawn should say so in the log — a silent "+
				"refusal reads to a player as a Yawn that simply missed", seed)
		}

		if !yawnBeforeSafeguard(t, seed) {
			t.Fatalf("seed %d: a Yawn armed before the Safeguard went up should still "+
				"collect its sleep — Showdown exempts yawn in onSetStatus", seed)
		}
	}
}

// yawnUnderTerrain sets a terrain on turn 1, casts Yawn into it on turn 2, and
// then lets two quiet turns run so a countdown that was armed has time to
// expire. Returns whether the countdown was armed and whether the target ever
// slept — the two halves the two terrains disagree about.
func yawnUnderTerrain(t *testing.T, seed uint64, setter string, target TeamPick) (armed, slept bool) {
	t.Helper()
	d, s := duel(t, seed,
		[]TeamPick{mon(dexSnorlax, "", "", setter, "yawn", "splash")},
		[]TeamPick{target})
	ResolveTurn(d, s, slots(0, 0))
	ResolveTurn(d, s, slots(1, 0))
	armed = s.Active(1).Volatiles.Yawn != nil
	ResolveTurn(d, s, slots(2, 0))
	ResolveTurn(d, s, slots(2, 0))
	slept = s.Active(1).Status == StatusSleep
	return armed, slept
}

// TestBattleElectricTerrainFailsYawnOutrightAndMistyTerrainDoesNot is the
// terrain asymmetry, asserted as a pair because either half alone reads as a
// bug in the other.
//
// Both terrains refuse a plain sleep move on a grounded target, so the tempting
// implementation asks the one predicate that answers that question
// (terrainBlocksStatus) and fails the move whenever it says yes. That is wrong
// for Misty Terrain: its condition's onTryAddVolatile names confusion and
// nothing else, so the drowsiness lands and it is the onSetStatus half, two
// turns later, that keeps the victim awake. Electric Terrain's condition does
// carry an onTryAddVolatile for yawn, so there the move itself fails.
//
// The observable difference is exactly this: under Misty Terrain the target
// spends two turns visibly drowsy and then does not fall asleep; under Electric
// Terrain it is never drowsy at all.
func TestBattleElectricTerrainFailsYawnOutrightAndMistyTerrainDoesNot(t *testing.T) {
	grounded := mon(dexSnorlax, "", "", "splash")
	for seed := uint64(1); seed <= yawnGateSeeds; seed++ {
		armed, slept := yawnUnderTerrain(t, seed, "electric-terrain", grounded)
		if armed {
			t.Fatalf("seed %d: Yawn made a grounded target drowsy under Electric Terrain — "+
				"the terrain refuses the volatile, not just the sleep", seed)
		}
		if slept {
			t.Fatalf("seed %d: a grounded target slept under Electric Terrain", seed)
		}

		armed, slept = yawnUnderTerrain(t, seed, "misty-terrain", grounded)
		if !armed {
			t.Fatalf("seed %d: Yawn failed under Misty Terrain — Misty's onTryAddVolatile "+
				"names confusion only, so the drowsiness should land", seed)
		}
		if slept {
			t.Fatalf("seed %d: Misty Terrain let the drowsy target fall asleep", seed)
		}
	}
}

// TestBattleElectricTerrainDoesNotRefuseYawnAgainstAnUngroundedTarget is the
// control that keeps the fix above from becoming "Electric Terrain switches
// Yawn off". Terrain reaches what stands on the ground and nothing else, so a
// Flying-type gets drowsy in the middle of an electric field and falls asleep
// on schedule. Without this, a terrain check that forgot isGrounded would look
// perfectly correct.
func TestBattleElectricTerrainDoesNotRefuseYawnAgainstAnUngroundedTarget(t *testing.T) {
	flyer := mon(dexCharizard, "", "", "splash")
	for seed := uint64(1); seed <= yawnGateSeeds; seed++ {
		armed, slept := yawnUnderTerrain(t, seed, "electric-terrain", flyer)
		if !armed {
			t.Fatalf("seed %d: a Flying-type was refused Yawn by Electric Terrain", seed)
		}
		if !slept {
			t.Fatalf("seed %d: a Flying-type should sleep on schedule — Electric Terrain "+
				"does not reach it", seed)
		}
	}
}

// yawnAgainstAbility casts Yawn at a target holding one ability, after however
// many setup turns the ability needs the field to have. weatherSetter "" means
// no setup turn at all.
func yawnAgainstAbility(t *testing.T, seed uint64, ability, weatherSetter string) (armed, slept bool) {
	t.Helper()
	user := mon(dexSnorlax, "", "", "yawn", "splash", "splash")
	if weatherSetter != "" {
		user = mon(dexSnorlax, "", "", weatherSetter, "yawn", "splash")
	}
	d, s := duel(t, seed,
		[]TeamPick{user},
		[]TeamPick{mon(dexSnorlax, ability, "", "splash")})
	yawnSlot := 0
	if weatherSetter != "" {
		ResolveTurn(d, s, slots(0, 0))
		yawnSlot = 1
	}
	ResolveTurn(d, s, slots(yawnSlot, 0))
	armed = s.Active(1).Volatiles.Yawn != nil
	ResolveTurn(d, s, slots(2, 0))
	ResolveTurn(d, s, slots(2, 0))
	slept = s.Active(1).Status == StatusSleep
	return armed, slept
}

// TestBattleSleeplessAbilitiesRefuseYawnOnUse: Insomnia, Vital Spirit and Leaf
// Guard each carry an onTryAddVolatile in Showdown's ability data that names
// yawn, so they refuse the drowsiness up front rather than letting the victim
// spend two turns yawning at a sleep that was never going to arrive. Leaf Guard
// is the interesting one because it only refuses while the sun is up, which
// makes it the check a stateless ability predicate cannot make — and the
// bare-ability control arm proves the move works at all, so a version that
// failed Yawn unconditionally could not pass this.
func TestBattleSleeplessAbilitiesRefuseYawnOnUse(t *testing.T) {
	for seed := uint64(1); seed <= yawnGateSeeds; seed++ {
		for _, ability := range []string{"insomnia", "vital-spirit"} {
			if armed, slept := yawnAgainstAbility(t, seed, ability, ""); armed || slept {
				t.Fatalf("seed %d: %s should refuse Yawn outright; armed=%v slept=%v",
					seed, ability, armed, slept)
			}
		}
		if armed, slept := yawnAgainstAbility(t, seed, "leaf-guard", "sunny-day"); armed || slept {
			t.Fatalf("seed %d: Leaf Guard in sun should refuse Yawn outright; armed=%v slept=%v",
				seed, armed, slept)
		}
		// Leaf Guard with no sun is just a body: the Yawn lands and collects.
		if armed, slept := yawnAgainstAbility(t, seed, "leaf-guard", ""); !armed || !slept {
			t.Fatalf("seed %d: Leaf Guard out of the sun should not stop Yawn; armed=%v slept=%v",
				seed, armed, slept)
		}
		// And the same body with no ability at all, so none of the above can
		// be passing because Yawn stopped working.
		if armed, slept := yawnAgainstAbility(t, seed, "", ""); !armed || !slept {
			t.Fatalf("seed %d: control — Yawn should land on a bare target; armed=%v slept=%v",
				seed, armed, slept)
		}
	}
}
