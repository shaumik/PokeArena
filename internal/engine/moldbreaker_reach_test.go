package engine

import (
	"testing"
)

// Mold Breaker was a flag consulted at five hand-placed call sites rather than
// a fact about the field, so the gates that were not on that list were
// unreachable — and four of them could not have been fixed with a one-line
// guard, because the attacker was not in their signature at all.
//
// It is state now: executeMove records the ability-ignoring attacker for as
// long as its move is resolving (BattleState.moldBreaker), which is Showdown's
// own shape and is what gives the flag the reach the gates needed. These tests
// are the untagged half of the evidence; the Showdown port covers the same
// ground against upstream's cases, behind a build tag CI does not compile.

// TestMoldBreakerReachesShieldDust: Shield Dust refuses a move's added effects,
// and abilityBlocksSecondaries took only the defender, so the attacker could
// never be asked about. Body Slam's 30% paralysis is the observable.
func TestMoldBreakerReachesShieldDust(t *testing.T) {
	paralyzedIn := func(ability string) bool {
		for seed := uint64(1); seed <= 40; seed++ {
			d, s := duel(t, seed,
				[]TeamPick{mon(dexPinsir, ability, "", "body-slam")},
				[]TeamPick{mon(dexSnorlax, "shield-dust", "", "splash")})
			for turn := 0; turn < 6; turn++ {
				ResolveTurn(d, s, slots(0, 0))
				if s.Active(1).Status == StatusParalysis {
					return true
				}
			}
		}
		return false
	}
	if paralyzedIn("") {
		t.Fatalf("baseline: Shield Dust let Body Slam's paralysis through")
	}
	if !paralyzedIn("mold-breaker") {
		t.Errorf("Mold Breaker should negate Shield Dust; 40 seeds x 6 turns of Body Slam " +
			"never paralyzed")
	}
}

// TestMoldBreakerReachesClearBody: abilityBlocksStatLowerByFoe took the
// defender and the stat, and nothing else.
func TestMoldBreakerReachesClearBody(t *testing.T) {
	drop := func(ability string) int {
		d, s := duel(t, 7,
			[]TeamPick{mon(dexPinsir, ability, "", "growl")},
			[]TeamPick{mon(dexSnorlax, "clear-body", "", "splash")})
		ResolveTurn(d, s, slots(0, 0))
		return s.Active(1).Stages.Atk
	}
	if got := drop(""); got != 0 {
		t.Fatalf("baseline: Clear Body let Growl through (atk %d)", got)
	}
	if got := drop("mold-breaker"); got != -1 {
		t.Errorf("Mold Breaker vs Clear Body: atk stage %d, want -1", got)
	}
}

// TestMoldBreakerReachesStickyHold: itemIsRemovable took only the holder.
//
// Worth stating what is *not* being fixed here: upstream's sticky-hold names
// Knock Off explicitly, and a note in items.go used to read that as an
// exemption. It is the opposite — an extra reason to refuse — and
// test/sim/abilities/stickyhold.js asserts the item survives Knock Off. So the
// plain case still refuses; only the mold breaker gets through.
func TestMoldBreakerReachesStickyHold(t *testing.T) {
	knockOff := func(ability string) ItemKind {
		d, s := duel(t, 11,
			[]TeamPick{mon(dexPinsir, ability, "", "knock-off")},
			[]TeamPick{mon(dexSnorlax, "sticky-hold", "leftovers", "splash")})
		ResolveTurn(d, s, slots(0, 0))
		return s.Active(1).Item
	}
	if got := knockOff(""); got != ItemLeftovers {
		t.Fatalf("baseline: Sticky Hold lost the item to Knock Off (%q)", got)
	}
	if got := knockOff("mold-breaker"); got != ItemNone {
		t.Errorf("Mold Breaker vs Sticky Hold: item %q, want it knocked off", got)
	}
}

// TestMoldBreakerReachesDamp: dampActive asked the field, not the attacker.
func TestMoldBreakerReachesDamp(t *testing.T) {
	exploded := func(ability string) bool {
		d, s := duel(t, 13,
			[]TeamPick{mon(dexSnorlax, ability, "", "explosion"), mon(dexCharizard, "", "", "splash")},
			[]TeamPick{mon(dexSnorlax, "damp", "", "splash")})
		ResolveTurn(d, s, slots(0, 0))
		return s.Sides[0].Team[0].Fainted
	}
	if exploded("") {
		t.Fatalf("baseline: Damp did not fizzle Explosion")
	}
	if !exploded("mold-breaker") {
		t.Errorf("Mold Breaker should suppress Damp; Explosion still fizzled")
	}
}

// TestMoldBreakerReachesUnaware: the one gate that always had the attacker in
// scope. The check simply was not made.
func TestMoldBreakerReachesUnaware(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[dexSnorlax])
	atk.Stages.Atk = 6
	def := buildPokemon(d, d.Species[dexSnorlax])
	def.Ability = "unaware"
	m := d.Moves["body-slam"]

	blanked := computeDamage(d, &atk, &def, m, nil, nil, nil, nil, NewRNG(5))
	atk.Ability = AbilityMoldBreaker
	seen := computeDamage(d, &atk, &def, m, nil, nil, nil, nil, NewRNG(5))
	if seen.Damage <= blanked.Damage {
		t.Errorf("Mold Breaker vs Unaware: %d damage at +6 Atk, want more than the %d "+
			"Unaware blanks it to", seen.Damage, blanked.Damage)
	}

	// The attacker's own Unaware is untouched — Mold Breaker suppresses the
	// target's abilities, never its own.
	self := buildPokemon(d, d.Species[dexSnorlax])
	self.Ability = "unaware"
	boosted := buildPokemon(d, d.Species[dexSnorlax])
	boosted.Stages.Def = 6
	withUnaware := computeDamage(d, &self, &boosted, m, nil, nil, nil, nil, NewRNG(5))
	self.Ability = AbilityNone
	without := computeDamage(d, &self, &boosted, m, nil, nil, nil, nil, NewRNG(5))
	if withUnaware.Damage <= without.Damage {
		t.Errorf("the attacker's own Unaware should still ignore the target's +6 Def: "+
			"%d vs %d", withUnaware.Damage, without.Damage)
	}
}

// TestMoldBreakerReachesLevitateOnAForcedSwitch is the case that cannot be
// fixed by threading an attacker at all: the hazards run from the switch path,
// with no move in scope. Suppression as field state covers it because the drag
// and the hazards are still inside the mold breaker's move.
func TestMoldBreakerReachesLevitateOnAForcedSwitch(t *testing.T) {
	spiked := func(ability string) bool {
		d, s := duel(t, 17,
			[]TeamPick{mon(dexPinsir, ability, "", "spikes", "roar")},
			[]TeamPick{
				mon(dexGengar, "levitate", "", "splash"),
				mon(dexGengar, "levitate", "", "splash"),
			})
		ResolveTurn(d, s, slots(0, 0)) // Spikes
		before := s.Sides[1].Team[1].HP
		ResolveTurn(d, s, slots(1, 0)) // Roar
		return s.Sides[1].Team[1].HP < before
	}
	if spiked("") {
		t.Fatalf("baseline: a Levitate holder dragged in took Spikes without Mold Breaker")
	}
	if !spiked("mold-breaker") {
		t.Errorf("a Levitate holder dragged in by a mold breaker's Roar should land on the Spikes")
	}
}

// TestMoldBreakerSuppressionIsRestoredAfterTheMove: the flag is saved and
// restored rather than cleared, so a move resolving inside another one leaves
// the outer move's suppression intact — and nothing leaks past the turn.
func TestMoldBreakerSuppressionIsRestoredAfterTheMove(t *testing.T) {
	d, s := duel(t, 19,
		[]TeamPick{mon(dexPinsir, "mold-breaker", "", "tackle")},
		[]TeamPick{mon(dexSnorlax, "clear-body", "", "splash")})
	if s.moldBreaker != nil {
		t.Fatalf("setup: suppression is live before any move")
	}
	ResolveTurn(d, s, slots(0, 0))
	if s.moldBreaker != nil {
		t.Errorf("suppression outlived the move that set it")
	}
	// And with it down, Clear Body works again.
	if abilitySuppressed(s, s.Active(1)) {
		t.Errorf("abilitySuppressed still reports the defender suppressed after the turn")
	}
}
