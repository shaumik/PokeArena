package engine

import "testing"

// TestHeavyDutyBootsStopThePoisonNotTheAbsorb. The boots check used to sit at
// the top of applyHazardsOnSwitchIn, above every branch, with a note saying the
// item covers the whole hazard category. It does not cover one branch:
// upstream's toxicspikes onSwitchIn asks about groundedness and the Poison-type
// absorb *before* it asks about the boots, so a booted grounded Poison-type
// soaks the layers up and clears the field for whatever comes in next. Stopping
// the wearer being poisoned is not the same as stopping the layers being
// absorbed — and leaving them laid is the difference between a Poison-type
// pivot answering Toxic Spikes and not.
func TestHeavyDutyBootsStopThePoisonNotTheAbsorb(t *testing.T) {
	d := loadDex(t)
	const (
		muk     = 89  // Poison — absorbs
		weezing = 110 // Poison/Flying — Levitate, so it never touches the layers
		snorlax = 143 // Normal — takes the poison
		tspikes = 2
	)
	entry := func(dexNo int, item ItemKind, ability AbilityKind) (layersLeft int, status StatusCond) {
		s, err := NewBattle(d, "b", "In", []int{snorlax, dexNo}, "Setter", []int{snorlax}, 1)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Sides[0].Conditions.Hazards.ToxicSpikes = tspikes
		s.Sides[0].Team[1].Item = item
		s.Sides[0].Team[1].Ability = ability
		var log []LogLine
		doSwitch(s, 0, 1, NewRNG(1), &log)
		return s.Sides[0].Conditions.Hazards.ToxicSpikes, s.Active(0).Status
	}

	// Baseline: a bare Poison-type absorbs.
	if left, st := entry(muk, ItemNone, AbilityNone); left != 0 || st != StatusNone {
		t.Fatalf("baseline: a grounded Poison-type should absorb the layers and take nothing; "+
			"layers=%d status=%q", left, st)
	}
	// The case: booted, it still absorbs.
	if left, st := entry(muk, ItemHeavyDutyBoots, AbilityNone); left != 0 || st != StatusNone {
		t.Errorf("a booted grounded Poison-type should still absorb the Toxic Spikes; "+
			"layers=%d status=%q", left, st)
	}
	// And the boots still do their job on something that would be poisoned.
	if left, st := entry(snorlax, ItemHeavyDutyBoots, AbilityNone); left != tspikes || st != StatusNone {
		t.Errorf("boots should keep the poison off and leave the layers laid; layers=%d status=%q",
			left, st)
	}
	if _, st := entry(snorlax, ItemNone, AbilityNone); st != StatusToxic {
		t.Errorf("control: two layers should badly poison an unbooted Normal-type, got %q", st)
	}
	// The grounding gate is untouched: a Levitating Poison-type does not absorb.
	if left, _ := entry(weezing, ItemNone, AbilityLevitate); left != tspikes {
		t.Errorf("a Levitating Poison-type should not absorb the layers; layers=%d", left)
	}
}

// TestPoisonTouchAndStenchAreRefusedByWhatRefusesSecondaries. Both abilities
// carried a note saying that because the effect is the ability's own rather
// than a move secondary, Shield Dust does not suppress it. Upstream says the
// opposite for both, and says it twice: poisontouch's handler opens with the
// comment "Despite not being a secondary, Shield Dust / Covert Cloak block
// Poison Touch's effect" and the check, and stench is not an ability effect at
// all — its onModifyMove pushes a real {chance: 10, volatileStatus: 'flinch'}
// onto move.secondaries, so everything that refuses added effects refuses it.
func TestPoisonTouchAndStenchAreRefusedByWhatRefusesSecondaries(t *testing.T) {
	const seeds = 60
	// poisonedIn plays `seeds` battles of a contact move into the given
	// defender and reports whether the rider ever landed. Measured over many
	// seeds rather than pinned to one, per the house rule: at 30% a single seed
	// says nothing either way.
	poisonedIn := func(ability, item string) bool {
		for seed := uint64(1); seed <= seeds; seed++ {
			d, s := duel(t, seed,
				[]TeamPick{mon(dexPinsir, "poison-touch", "", "tackle")},
				[]TeamPick{mon(dexSnorlax, ability, item, "splash")})
			ResolveTurn(d, s, slots(0, 0))
			if s.Active(1).Status == StatusPoison {
				return true
			}
		}
		return false
	}
	if !poisonedIn("", "") {
		t.Fatalf("baseline: Poison Touch never poisoned across %d seeds", seeds)
	}
	if poisonedIn("shield-dust", "") {
		t.Errorf("Shield Dust should refuse Poison Touch")
	}
	if poisonedIn("", "covert-cloak") {
		t.Errorf("a Covert Cloak should refuse Poison Touch")
	}

	flinchedIn := func(ability, item string) bool {
		for seed := uint64(1); seed <= seeds*3; seed++ {
			d, s := duel(t, seed,
				[]TeamPick{mon(dexPinsir, "stench", "", "tackle")},
				[]TeamPick{mon(dexSnorlax, ability, item, "splash")})
			log := ResolveTurn(d, s, slots(0, 0))
			if logHas(log, "flinched") {
				return true
			}
		}
		return false
	}
	if !flinchedIn("", "") {
		t.Fatalf("baseline: Stench never flinched across %d seeds", seeds*3)
	}
	if flinchedIn("shield-dust", "") {
		t.Errorf("Shield Dust should refuse Stench's flinch — upstream pushes it as a real secondary")
	}
	if flinchedIn("", "covert-cloak") {
		t.Errorf("a Covert Cloak should refuse Stench's flinch")
	}
}
