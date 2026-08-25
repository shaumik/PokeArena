package engine

import "testing"

// items_abilityshield_behavior_test.go covers the one item that touches five
// unrelated parts of the engine.
//
// Each of the five is a separate way to take a Pokémon's ability away, and the
// shield refuses each in a different place — so a test per place, plus the two
// that pin the ordering the whole thing hangs on. The ordering pair is the
// point of the file: the shield beats Neutralizing Gas but loses to a Gastro
// Acid that already landed, and an implementation that got either backwards
// would still pass every other test here.

// shieldBattle builds a mirror of Snorlax with the shield on side 0.
func shieldBattle(t *testing.T, seed uint64) (*BattleState, *Pokemon, *Pokemon) {
	t.Helper()
	d := loadDex(t)
	s := neutralBattle(t, d, seed, []int{143, 65}, []int{143, 65})
	mine, foe := s.Active(0), s.Active(1)
	mine.Item = ItemAbilityShield
	teachMoves(t, d, mine, "splash")
	teachMoves(t, d, foe, "splash")
	return s, mine, foe
}

// --- the write path: ability-setting moves ---

// TestAbilityShieldRefusesWorrySeed. The declarative half of the item: an
// ability-setting move resolves, the write does not happen.
func TestAbilityShieldRefusesWorrySeed(t *testing.T) {
	d := loadDex(t)
	s, mine, foe := shieldBattle(t, 11)
	mine.Ability = "inner-focus"
	teachMoves(t, d, foe, "worry-seed")

	log := playTurn(d, s, 0, 0)
	if mine.Ability != "inner-focus" {
		t.Errorf("the shield should have refused Worry Seed, ability is now %q", mine.Ability)
	}
	if !logHas(log, "Ability Shield") {
		t.Errorf("the block should announce itself, got %v", logTexts(log))
	}
}

// TestAbilityShieldBlockIsNotAFailure. Canon's onSetAbility returns null rather
// than false, and Worry Seed hands that straight back out of onHit — so the
// move resolved and did nothing. A "But it failed!" here would be a line canon
// never prints, and would arm Stomping Tantrum besides.
func TestAbilityShieldBlockIsNotAFailure(t *testing.T) {
	d := loadDex(t)
	s, mine, foe := shieldBattle(t, 11)
	mine.Ability = "inner-focus"
	teachMoves(t, d, foe, "worry-seed")

	log := playTurn(d, s, 0, 0)
	if logHas(log, "But it failed!") {
		t.Errorf("a shielded write is a block, not a failure: %v", logTexts(log))
	}
	if foe.Volatiles.MoveThisTurnFailed {
		t.Error("nor should it record a failed move for Stomping Tantrum")
	}
}

// TestAbilityShieldRefusesSkillSwapFromEitherSide. Canon's skillSwap runs the
// SetAbility event on the target and then on the source, returning on the first
// refusal — before any write — so neither side moves and it does not matter
// which one holds the shield.
func TestAbilityShieldRefusesSkillSwapFromEitherSide(t *testing.T) {
	d := loadDex(t)
	for _, tc := range []struct {
		name        string
		shieldSide  int
		swapperSide int
	}{
		{"targeted", 0, 1},
		{"used by the holder", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := shieldBattle(t, 11)
			mine, foe := s.Active(0), s.Active(1)
			mine.Item, foe.Item = ItemNone, ItemNone
			s.Active(tc.shieldSide).Item = ItemAbilityShield
			mine.Ability, foe.Ability = "inner-focus", "insomnia"
			teachMoves(t, d, s.Active(tc.swapperSide), "skill-swap")
			teachMoves(t, d, s.Active(1-tc.swapperSide), "splash")

			playTurn(d, s, 0, 0)
			if mine.Ability != "inner-focus" || foe.Ability != "insomnia" {
				t.Errorf("nothing should have moved: mine %q, foe %q", mine.Ability, foe.Ability)
			}
		})
	}
}

// TestAbilityShieldStopsTraceCopying, and does not re-arm. Trace's copy is a
// write to the tracer's own ability, so the shield refuses it wherever it came
// from; and because the copy fires from the entry hook, a shield knocked off
// afterwards does not hand the Trace back.
func TestAbilityShieldStopsTraceCopying(t *testing.T) {
	d := loadDex(t)
	s, mine, foe := shieldBattle(t, 11)
	mine.Ability, foe.Ability = "trace", "insomnia"

	playTurn(d, s, 0, 0)
	if mine.Ability != "trace" {
		t.Fatalf("the shield should have refused the Trace, ability is now %q", mine.Ability)
	}

	mine.Item = ItemNone
	playTurn(d, s, 0, 0)
	if mine.Ability != "trace" {
		t.Errorf("losing the shield later should not re-arm Trace, ability is now %q", mine.Ability)
	}
}

// --- the read path: suppression ---

// TestAbilityShieldSurvivesNeutralizingGas. The gas is a live read of the
// field, so the shield simply exempts its holder from it.
func TestAbilityShieldSurvivesNeutralizingGas(t *testing.T) {
	d := loadDex(t)
	s, mine, foe := shieldBattle(t, 11)
	mine.Ability = "inner-focus"
	foe.Ability = AbilityNeutralizingGas
	seedAbilitySuppression(s)

	playTurn(d, s, 0, 0)
	if mine.Volatiles.AbilitySuppressed {
		t.Error("the shield holder should be exempt from the gas")
	}
	if !foe.Volatiles.AbilitySuppressed && foe.Ability != AbilityNeutralizingGas {
		t.Error("fixture: the gas holder should be emitting")
	}
}

// TestAbilityShieldAcquiredMidGasLiftsSuppression — the shield is read from the
// field every sync, so picking one up while the gas is still out works.
func TestAbilityShieldAcquiredMidGasLiftsSuppression(t *testing.T) {
	d := loadDex(t)
	s, mine, foe := shieldBattle(t, 11)
	mine.Item = ItemNone
	mine.Ability = "inner-focus"
	foe.Ability = AbilityNeutralizingGas
	seedAbilitySuppression(s)

	playTurn(d, s, 0, 0)
	if !mine.Volatiles.AbilitySuppressed {
		t.Fatal("fixture: the holder should start suppressed by the gas")
	}
	mine.Item = ItemAbilityShield
	playTurn(d, s, 0, 0)
	if mine.Volatiles.AbilitySuppressed {
		t.Error("a shield picked up mid-gas should lift the suppression immediately")
	}
}

// TestAbilityShieldAcquiredAfterGastroAcidDoesNotLift — the other half of the
// pair, and the one an implementation is likely to get wrong. Gastro Acid is a
// sticky volatile on the victim and canon reads it *before* the shield, so a
// shield picked up afterwards changes nothing.
func TestAbilityShieldAcquiredAfterGastroAcidDoesNotLift(t *testing.T) {
	d := loadDex(t)
	s, mine, foe := shieldBattle(t, 11)
	mine.Item = ItemNone
	mine.Ability = "inner-focus"
	teachMoves(t, d, foe, "gastro-acid")

	playTurn(d, s, 0, 0)
	if !mine.Volatiles.GastroAcid {
		t.Fatal("fixture: Gastro Acid should have landed on an unshielded target")
	}
	mine.Item = ItemAbilityShield
	teachMoves(t, d, foe, "splash")
	playTurn(d, s, 0, 0)
	if !mine.Volatiles.AbilitySuppressed {
		t.Error("a shield picked up after Gastro Acid landed must not undo it")
	}
}

// TestAbilityShieldRefusesGastroAcidOutright is why the pair above can both be
// true: the shield's protection from Gastro Acid is at application time, not
// read time, because read time is already lost to the volatile.
func TestAbilityShieldRefusesGastroAcidOutright(t *testing.T) {
	d := loadDex(t)
	s, mine, foe := shieldBattle(t, 11)
	mine.Ability = "inner-focus"
	teachMoves(t, d, foe, "gastro-acid")

	log := playTurn(d, s, 0, 0)
	if mine.Volatiles.GastroAcid {
		t.Error("the volatile should never have been applied through the shield")
	}
	if mine.Volatiles.AbilitySuppressed {
		t.Error("and the holder's ability should still be live")
	}
	if logHas(log, "But it failed!") {
		t.Errorf("canon returns null here, not false: %v", logTexts(log))
	}
}

// TestAbilityShieldDoesNotRefireEntryHooksWhenItLiftsTheGas. Canon hangs the
// re-run of switch-in abilities on the gas *ending*, not on one Pokémon
// ceasing to be affected — so a holder that shields up mid-gas gets its ability
// back without announcing a fresh entry effect.
func TestAbilityShieldDoesNotRefireEntryHooksWhenItLiftsTheGas(t *testing.T) {
	d := loadDex(t)
	s, mine, foe := shieldBattle(t, 11)
	mine.Item = ItemNone
	mine.Ability = AbilityIntimidate
	foe.Ability = AbilityNeutralizingGas
	seedAbilitySuppression(s)

	playTurn(d, s, 0, 0)
	before := foe.Stages.Atk
	mine.Item = ItemAbilityShield
	log := playTurn(d, s, 0, 0)

	if foe.Stages.Atk != before {
		t.Errorf("shielding up must not fire a fresh Intimidate: foe Atk %d → %d", before, foe.Stages.Atk)
	}
	if logHas(log, "Neutralizing Gas wore off") {
		t.Errorf("the gas has not ended, only stopped applying to one holder: %v", logTexts(log))
	}
}

// --- the defender-side gates ---

// TestAbilityShieldKeepsSturdyAgainstMoldBreaker, end to end. Canon's
// suppressingAbility ends in `&& !target?.hasItem('Ability Shield')`, so this
// is the defender's call and not a fact about the attacker — which is exactly
// what the old direct abilityBreaksMold(atk) read at this gate could not
// express.
//
// The holder's HP is cut to a sliver so an ordinary Tackle is lethal; Sturdy
// only fires from full, and both halves of the fixture start there.
func TestAbilityShieldKeepsSturdyAgainstMoldBreaker(t *testing.T) {
	d := loadDex(t)

	survives := func(shielded bool) bool {
		s, mine, foe := shieldBattle(t, 11)
		if !shielded {
			mine.Item = ItemNone
		}
		mine.Ability = AbilitySturdy
		foe.Ability = AbilityMoldBreaker
		teachMoves(t, d, foe, "tackle")
		mine.MaxHP, mine.HP = 10, 10

		playTurn(d, s, 0, 0)
		return !mine.Fainted
	}

	if survives(false) {
		t.Fatal("fixture: an unshielded Sturdy should be broken through and the holder KO'd")
	}
	if !survives(true) {
		t.Error("a shielded Sturdy holder should still survive a Mold Breaker hit at 1 HP")
	}
}

// --- Klutz ---

// TestAbilityShieldWorksThroughKlutzButStillCannotBeFlung is the item's one
// genuine asymmetry: canon's ignoringItem consults ignoreKlutz on the ordinary
// path and skips it on the Fling path, so Klutz cannot switch the shield off
// but can still stop its holder throwing it.
func TestAbilityShieldWorksThroughKlutzButStillCannotBeFlung(t *testing.T) {
	_, mine, _ := shieldBattle(t, 11)
	mine.Ability = "klutz"

	if itemSuppressed(mine) {
		t.Error("Klutz must not switch off an item that ignores Klutz")
	}
	if !holdsAbilityShield(mine) {
		t.Error("so the shield is still protecting its Klutz holder")
	}
	if !itemSuppressedForFling(mine) {
		t.Error("but Klutz still stops the holder flinging it")
	}

	// Embargo is not exempted, and neither is Magic Room.
	mine.Volatiles.Embargo = &EmbargoState{Turns: 5}
	if !itemSuppressed(mine) || holdsAbilityShield(mine) {
		t.Error("Embargo should switch the shield off; only Klutz is exempted")
	}
	mine.Volatiles.Embargo = nil
	mine.Volatiles.MagicRoomHere = true
	if !itemSuppressed(mine) || holdsAbilityShield(mine) {
		t.Error("Magic Room should switch the shield off too")
	}
}
