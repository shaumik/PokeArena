package engine

import (
	"testing"
)

// items_regress_test.go pins the bugs an adversarial review of the berry
// framework turned up. Each one shipped green through the behavior tests and
// the full-battle sweep, because each is a *missing* interaction rather than a
// wrong one: nothing crashed, no invariant broke, the battle just played out
// differently from canon. That is the class of defect this file exists for.

// TestResistBerryIgnoresSubstituteHits is the worst of them: the ×0.5 lived in
// computeDamage while the consume decision lived in dealDamage *after* the
// Substitute early-return, so a hit absorbed by a doll was halved and the berry
// was never spent. A resist berry behind a Substitute became a permanent 50%
// resistance to its type.
//
// Canon: the berry neither reduces the damage nor activates — the doll took the
// hit, not the holder.
func TestResistBerryIgnoresSubstituteHits(t *testing.T) {
	d := loadDex(t)
	subDamage := func(item ItemKind) int {
		// Charizard (Fire/Flying) takes 2x from Water; Blastoise supplies it.
		s, err := NewBattle(d, "b", "Holder", []int{6}, "Attacker", []int{9}, 11)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		holder := s.Active(0)
		holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		holder.Item = item
		holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		holder.Volatiles.Substitute = &SubstituteState{HP: 10000}
		s.Active(1).Moves = []MoveSlot{{MoveID: "surf", PP: 15, MaxPP: 15}}

		before := holder.Volatiles.Substitute.HP
		splashTurn(d, s)
		if s.Active(0).Item != item {
			t.Errorf("%s was consumed by a hit the Substitute absorbed", item)
		}
		return before - s.Active(0).Volatiles.Substitute.HP
	}

	bare := subDamage(ItemNone)
	withBerry := subDamage(ItemPasshoBerry)
	if bare <= 0 {
		t.Fatalf("setup: the Substitute took no damage")
	}
	if withBerry != bare {
		t.Errorf("Passho Berry reduced damage dealt to a Substitute: %d vs %d bare", withBerry, bare)
	}
}

// TestResistBerryIgnoresOHKOMoves: an OHKO move overwrites the computed damage
// with the target's full HP, so the berry's halving is thrown away — but the
// consume check keyed only on type and effectiveness, so the berry was spent
// and the log announced a reduction that never happened.
func TestResistBerryIgnoresOHKOMoves(t *testing.T) {
	d := loadDex(t)
	// Horn Drill is Normal, so Chilan (which answers any Normal hit) is the
	// berry that would fire. Snorlax is Normal — no type immunity in the way.
	s, err := NewBattle(d, "b", "Holder", []int{6}, "Attacker", []int{112}, 3)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	holder.Item = ItemChilanBerry
	holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	// Accuracy 0 would skip the roll; Horn Drill's real 30 makes this flaky, so
	// the move is forced to land by giving the attacker No Guard.
	s.Active(1).Ability = "no-guard"
	s.Active(1).Moves = []MoveSlot{{MoveID: "horn-drill", PP: 5, MaxPP: 5}}

	log := splashTurn(d, s)

	if logHas(log, "Chilan Berry weakened the damage") {
		t.Errorf("Chilan announced a reduction on a one-hit KO; log: %v", log)
	}
	if s.Active(0).Item != ItemChilanBerry && !s.Active(0).Fainted {
		t.Errorf("Chilan was consumed by an OHKO move")
	}
	// The berry must not have been spent even though the holder fainted: the
	// team slot still carries it.
	if got := s.Sides[0].Team[0].Item; got != ItemChilanBerry {
		t.Errorf("Chilan spent for nothing on an OHKO: item = %q", got)
	}
}

// TestPinchBerryFiresAfterSubstituteCost: Substitute pays a quarter of max HP,
// which can drop the user straight past its threshold. The status-move branch
// returned from executeMove before the pinch check, so the berry waited until
// end of turn — after the foe had already had its move.
func TestPinchBerryFiresAfterSubstituteCost(t *testing.T) {
	d, s := berryBattle(t, ItemSitrusBerry)
	holder := s.Active(0)
	holder.Moves = []MoveSlot{{MoveID: "substitute", PP: 10, MaxPP: 10}}
	// One HP above half; the 1/4 sub cost pushes it under.
	holder.HP = holder.MaxHP/2 + 1
	// Maxed Speed so the holder is guaranteed to move first, which is what
	// makes "before the foe's move" a meaningful assertion in a mirror match.
	holder.Stages.Spe = 6

	log := splashTurn(d, s)

	if s.Active(0).Item != ItemNone {
		t.Fatalf("Sitrus did not fire after the Substitute cost; log: %v", log)
	}
	// The distinction being tested is *when*: firing with the action means the
	// berry lands before the foe gets to move, which is the difference between
	// surviving the foe's attack and not. An end-of-turn trigger lands after it.
	if !itemFiredBeforeFoeMoved(log, "ate its Sitrus Berry") {
		t.Errorf("berry fired after the foe's move — that is the end-of-turn sweep, "+
			"not the action that paid the HP; log: %v", log)
	}
}

// itemFiredBeforeFoeMoved reports whether the side-0 item line named by sub
// appears before side 1's "used <move>!" line. It is the discriminator between
// "fired with the action" and "fired in the end-of-turn sweep" — an assertion
// on log distance is not, because a short turn puts the two within a line or
// two of each other either way.
func itemFiredBeforeFoeMoved(log []LogLine, sub string) bool {
	foeMoved, eat := -1, -1
	for i, l := range log {
		if foeMoved < 0 && l.Type == "move" && l.Side == 1 {
			foeMoved = i
		}
		if eat < 0 && logLineHas(l, sub) {
			eat = i
		}
	}
	return eat >= 0 && (foeMoved < 0 || eat < foeMoved)
}

// TestPinchBerryFiresAfterConfusionSelfHit: the same missing check on the other
// early-return path. canAct hurts the user and returns false, and executeMove
// returned immediately.
func TestPinchBerryFiresAfterConfusionSelfHit(t *testing.T) {
	d := loadDex(t)
	// The self-hit is a 33% roll, so seeds are swept until one produces it. The
	// sweep must find a case — a t.Skip here would silently retire the test.
	for seed := uint64(1); seed <= 80; seed++ {
		_, s := berryBattle(t, ItemSitrusBerry)
		s.RNGState, s.Seed = seed, seed
		holder := s.Active(0)
		holder.HP = holder.MaxHP/2 + 1
		holder.Stages.Spe = 6 // move first, so "before the foe" is meaningful
		holder.Volatiles.Confusion = &ConfusionState{Turns: 5}

		log := splashTurn(d, s)
		if !logHas(log, "hurt itself in its confusion") || s.Active(0).Fainted {
			continue
		}
		if s.Active(0).Item != ItemNone {
			t.Fatalf("seed %d: Sitrus did not fire after a confusion self-hit; log: %v", seed, log)
		}
		if !itemFiredBeforeFoeMoved(log, "ate its Sitrus Berry") {
			t.Fatalf("seed %d: berry fired after the foe's move — the self-hit path "+
				"fell through to the end-of-turn sweep; log: %v", seed, log)
		}
		return // found and verified the case
	}
	t.Fatal("no confusion self-hit occurred across 80 seeds — the fixture stopped exercising the path")
}

// TestAttackerChipBerryFiresOnTheKOHit: Jaboca and Rowap sit in the same family
// as Rough Skin and Rocky Helmet, which fire on the hit that KO'd their holder.
// The dispatcher gated on def.HP <= 0 — which is exactly that case — so they
// silently did nothing on the most consequential hit of the match, while the
// ability riders on the same Pokémon fired normally.
func TestAttackerChipBerryFiresOnTheKOHit(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	holder.Item = ItemJabocaBerry
	holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	holder.HP = 1 // any physical hit is lethal
	atk := s.Active(1)
	atk.Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	atkBefore := atk.HP
	wantChip := atk.MaxHP / 8

	log := splashTurn(d, s)

	if !s.Sides[0].Team[0].Fainted {
		t.Fatalf("setup: the holder was supposed to be KO'd; log: %v", log)
	}
	if got := atkBefore - s.Active(1).HP; got != wantChip {
		t.Errorf("Jaboca chipped %d on the KO hit, want %d; log: %v", got, wantChip, log)
	}
}

// TestLeftoversHealsBeforeStatusChip is the ordering fix. Canon puts Leftovers
// at residual order 5 and poison/burn at 9/10, which is the entire reason to
// run the item: the heal is meant to out-pace the chip. Ticking it after the
// residual block meant a holder that canon keeps alive faints, and the heal
// never happens at all.
func TestLeftoversHealsBeforeStatusChip(t *testing.T) {
	d, s := berryBattle(t, ItemLeftovers)
	holder := s.Active(0)
	holder.Status = StatusPoison // 1/8 max HP per turn
	// Below the poison chip but above (chip - Leftovers heal), so the ordering
	// alone decides whether the holder survives the turn.
	heal, chip := holder.MaxHP/16, holder.MaxHP/8
	holder.HP = chip - heal + 1

	log := splashTurn(d, s)

	if s.Active(0).Fainted {
		t.Errorf("holder fainted: Leftovers ticked after the poison chip instead of before; log: %v", log)
	}
	if got := s.Active(0).HP; got != 1 {
		t.Errorf("HP = %d, want 1 (healed %d then chipped %d from %d)", got, heal, chip, chip-heal+1)
	}
}

// TestPinchBerryFiresBetweenResiduals: a holder pushed into range by the poison
// chip must eat before the sandstorm chip lands, not after the whole residual
// block. Checking once at the end let a berry-holder faint to a second residual
// it should have survived.
func TestPinchBerryFiresBetweenResiduals(t *testing.T) {
	d, s := berryBattle(t, ItemSitrusBerry)
	holder := s.Active(0)
	// Toxic well into its escalation: a plain 1/8 poison chip can never
	// threaten a holder that is by definition above its half-HP threshold, so
	// the scenario needs a residual big enough to cross the line and leave the
	// holder inside the next residual's range.
	holder.Status = StatusToxic
	holder.ToxicCounter = 8 // 8/16 of max HP
	s.Weather = &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}

	holder.HP = holder.MaxHP/2 + 1
	toxic, sand := holder.MaxHP*8/16, holder.MaxHP/16
	if holder.HP-toxic > sand {
		t.Fatalf("fixture does not set up the case: %d HP - %d toxic leaves %d, "+
			"which survives the %d sand chip regardless", holder.HP, toxic, holder.HP-toxic, sand)
	}

	log := splashTurn(d, s)

	if s.Active(0).Fainted {
		t.Errorf("holder fainted to the sandstorm chip after toxic put it in berry range "+
			"(toxic %d, sand %d); the berry must fire between the two residuals; log: %v",
			toxic, sand, log)
	}
	if s.Active(0).Item != ItemNone {
		t.Errorf("Sitrus never fired across the residual block; log: %v", log)
	}
}

// TestGluttonyEatsPinchBerriesEarly: Gluttony was registered inert with the
// note "no berry items exist to act on". Forty-two of them exist now, so the
// ability does its real job — a quarter-HP trigger becomes a half-HP one.
func TestGluttonyEatsPinchBerriesEarly(t *testing.T) {
	d, s := berryBattle(t, ItemSalacBerry)
	holder := s.Active(0)
	holder.Ability = AbilityGluttony
	holder.HP = holder.MaxHP / 2 // in range for Gluttony, not for a bare holder

	splashTurn(d, s)

	if s.Active(0).Item != ItemNone {
		t.Errorf("Gluttony did not pull the Salac trigger up to half HP")
	}
	if s.Active(0).Stages.Spe != 1 {
		t.Errorf("Salac did not boost Speed: stage = %d", s.Active(0).Stages.Spe)
	}
}

// TestGluttonyDoesNotDelayHalfHPBerries: Gluttony makes you eat earlier, never
// later. A Sitrus already at half HP must be untouched by it.
func TestGluttonyDoesNotDelayHalfHPBerries(t *testing.T) {
	d, s := berryBattle(t, ItemSitrusBerry)
	holder := s.Active(0)
	holder.Ability = AbilityGluttony
	holder.HP = holder.MaxHP / 2

	splashTurn(d, s)

	if s.Active(0).Item != ItemNone {
		t.Errorf("Gluttony suppressed a half-HP berry that was already in range")
	}
}

// TestFatigueConfusionFiresACureBerry: rampage fatigue sets the confusion
// volatile directly instead of going through applyConfusionVolatile, so the
// held-item cure never saw it. A Lum holder finished an Outrage and sat
// confused for 2-5 turns with an unused berry.
func TestFatigueConfusionFiresACureBerry(t *testing.T) {
	_, s := berryBattle(t, ItemLumBerry)
	holder := s.Active(0)
	holder.Volatiles.LockedMove = &LockedMoveState{MoveIdx: 0, Turns: 1}

	var log []LogLine
	tickLockedMove(holder, 0, NewRNG(1), &log)

	if !logHas(log, "became confused due to fatigue") {
		t.Fatalf("setup: fatigue confusion did not fire; log: %v", log)
	}
	if holder.Volatiles.Confusion != nil {
		t.Errorf("Lum Berry did not cure fatigue confusion")
	}
	if holder.Item != ItemNone {
		t.Errorf("Lum Berry not consumed")
	}
}

// TestStarfSkipsMaxedStats: canon filters the draw to stats that can still
// rise. Drawing uniformly meant a +6-Speed sweeper could eat its Starf, roll
// "speed", and get "won't go higher!" for its trouble.
func TestStarfSkipsMaxedStats(t *testing.T) {
	d, s := berryBattle(t, ItemStarfBerry)
	holder := s.Active(0)
	holder.HP = holder.MaxHP / 4
	// Everything maxed except Sp. Def, so the draw has exactly one legal answer.
	holder.Stages.Atk, holder.Stages.Def = 6, 6
	holder.Stages.SpA, holder.Stages.Spe = 6, 6

	log := splashTurn(d, s)

	if got := s.Active(0).Stages.SpD; got != 2 {
		t.Errorf("Starf did not pick the only stat that could rise: SpD = %d; log: %v", got, log)
	}
	if logHas(log, "won't go higher") {
		t.Errorf("Starf rolled a maxed stat; log: %v", log)
	}
}

// TestStarfStaysInReserveWhenEverythingIsMaxed: with no stat left to raise
// there is nothing to spend the berry on, so it is not spent.
func TestStarfStaysInReserveWhenEverythingIsMaxed(t *testing.T) {
	d, s := berryBattle(t, ItemStarfBerry)
	holder := s.Active(0)
	holder.HP = holder.MaxHP / 4
	holder.Stages = Stages{Atk: 6, Def: 6, SpA: 6, SpD: 6, Spe: 6}

	splashTurn(d, s)

	if s.Active(0).Item != ItemStarfBerry {
		t.Errorf("Starf spent on a guaranteed no-op")
	}
}

// TestItemLogLinesSurviveAPercentInTheName: the holder's name used to be
// interpolated into the *format string*, so a name containing a percent sign
// produced a corrupted line. Dex names are tame today; the fix removes the
// class of bug rather than relying on that.
func TestItemLogLinesSurviveAPercentInTheName(t *testing.T) {
	d, s := berryBattle(t, ItemLifeOrb)
	holder := s.Active(0)
	holder.Name = "Mr. 100%s Mime"

	var log []LogLine
	applyLifeOrbRecoil(holder, 0, &log)

	if len(log) == 0 {
		t.Fatal("no recoil line emitted")
	}
	if logHas(log, "%!") || logHas(log, "MISSING") || logHas(log, "EXTRA") {
		t.Errorf("format verb leaked into the log line: %q", log[0].Text)
	}
	if !logHas(log, "Mr. 100%s Mime") {
		t.Errorf("name mangled in the log line: %q", log[0].Text)
	}
	_ = d
}

// logLineHas is the single-line form of logHas, for tests that need the index
// of a matching line rather than just its presence.
func logLineHas(l LogLine, sub string) bool {
	return logHas([]LogLine{l}, sub)
}
