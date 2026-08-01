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

// --- second review pass: the always-on modifier batch ---

// TestItemChipLinesAreNotCorrupt: itemDamage's format takes (name, amount).
// Rocky Helmet and the Jaboca/Rowap berries baked the name into the format and
// left one verb, so %d consumed the *string* and the amount spilled out as
// "%!(EXTRA int=39)" in every battle they fired in. The earlier
// percent-in-the-name test only covered Life Orb, which was already correct —
// this one sweeps every item that chips through itemDamage.
func TestItemChipLinesAreNotCorrupt(t *testing.T) {
	d := loadDex(t)
	// Each case is an item whose effect routes through itemDamage, driven far
	// enough to make it log.
	cases := []struct {
		name   string
		holder ItemKind
		move   string // the foe's move
		marker string
	}{
		{"rocky-helmet", ItemRockyHelmet, "body-slam", "Rocky Helmet"},
		{"jaboca-berry", ItemJabocaBerry, "body-slam", "was hurt"},
		{"rowap-berry", ItemRowapBerry, "water-gun", "was hurt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewBattle(d, "b", "Attacker", []int{143}, "Holder", []int{143}, 5)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			s.Active(0).Moves = []MoveSlot{{MoveID: tc.move, PP: 25, MaxPP: 25}}
			s.Active(1).Item = tc.holder
			s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

			log := splashTurn(d, s)

			if !logHas(log, tc.marker) {
				t.Fatalf("%s never fired; log: %v", tc.name, log)
			}
			assertNoFormatVerbsLeaked(t, log)
		})
	}
	// Black Sludge and Sticky Barb chip their own holder through the same
	// helper — swept here rather than in their own tests so a new chip item
	// can't be added with the old broken pattern.
	for _, item := range []ItemKind{ItemBlackSludge, ItemStickyBarb} {
		t.Run(string(item), func(t *testing.T) {
			_, s := berryBattle(t, item)
			s.Active(0).HP = s.Active(0).MaxHP / 2
			log := splashTurn(loadDex(t), s)
			assertNoFormatVerbsLeaked(t, log)
		})
	}
}

// assertNoFormatVerbsLeaked fails if any log line contains fmt's error markers
// for a malformed format string.
func assertNoFormatVerbsLeaked(t *testing.T, log []LogLine) {
	t.Helper()
	for _, l := range log {
		for _, bad := range []string{"%!", "(EXTRA", "MISSING", "%d", "%s"} {
			if logLineHas(l, bad) {
				t.Errorf("format verb leaked into a log line: %q", l.Text)
				break
			}
		}
	}
}

// TestFocusBandSavesAHolderAlreadyAtOneHP: the guard was `def.HP > 1`, which
// removed the exact case the band matters most in — the Sturdy or Sash survivor
// sitting on 1 HP. Canon rolls whenever the hit would be lethal.
func TestFocusBandSavesAHolderAlreadyAtOneHP(t *testing.T) {
	d := loadDex(t)
	saves := 0
	const trials = 300
	for seed := uint64(1); seed <= trials; seed++ {
		s, err := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		holder := s.Active(0)
		holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		holder.Item = ItemFocusBand
		holder.HP = 1
		holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
		splashTurn(d, s)
		if !s.Sides[0].Team[0].Fainted {
			saves++
			if got := s.Sides[0].Team[0].HP; got != 1 {
				t.Fatalf("a saved 1-HP holder should still be on 1 HP, got %d", got)
			}
		}
	}
	if saves == 0 {
		t.Errorf("Focus Band never saved a 1-HP holder across %d lethal hits", trials)
	}
}

// TestMetronomeStreakBreaksOnStruggleAndMisses: canon keys the count on the
// last move having succeeded. Struggle carries no move ID, so the tick used to
// skip it entirely and the streak survived; and the tick ran before the
// accuracy roll, so a whiff counted as a use.
func TestMetronomeStreakBreaksOnStruggleAndMisses(t *testing.T) {
	d := loadDex(t)

	t.Run("struggle resets", func(t *testing.T) {
		holder := buildPokemon(d, d.Species[143])
		holder.Ability = AbilityNone
		holder.Item = ItemMetronome
		m := d.Moves["body-slam"]
		for i := 0; i < 3; i++ {
			tickMetronome(&holder, m)
		}
		if holder.Volatiles.MetronomeCount == 0 {
			t.Fatalf("setup: the streak never built")
		}
		tickMetronome(&holder, struggleMove)
		if got := holder.Volatiles.MetronomeCount; got != 0 {
			t.Errorf("Struggle left the streak at %d; it is a different move and must reset it", got)
		}
		if got := metronomeMult(&holder, m); got != 1 {
			t.Errorf("the old move kept its multiplier after a Struggle: %v", got)
		}
	})

	t.Run("miss resets", func(t *testing.T) {
		// Focus Blast at 70% accuracy: sweep seeds until one misses, then check
		// the streak was broken rather than advanced.
		for seed := uint64(1); seed <= 60; seed++ {
			s, err := NewBattle(d, "b", "Holder", []int{143}, "Foe", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			holder := s.Active(0)
			holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			holder.Item = ItemMetronome
			holder.Moves = []MoveSlot{{MoveID: "focus-blast", PP: 5, MaxPP: 5}}
			holder.Volatiles.MetronomeMoveID = "focus-blast"
			holder.Volatiles.MetronomeCount = 3
			s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

			log := splashTurn(d, s)
			if !logHas(log, "attack missed") {
				continue
			}
			if got := s.Active(0).Volatiles.MetronomeCount; got != 0 {
				t.Fatalf("seed %d: a miss left the streak at %d, want 0", seed, got)
			}
			return
		}
		t.Fatal("no miss occurred across 60 seeds — the fixture stopped exercising the path")
	})
}

// TestShellBellDrainsOffTheMoveTotal: the drain used to fire per strike, so a
// multi-hit move truncated each eighth independently, and itemHealAmount's
// round-up floor healed 1 off a hit too weak to earn anything.
func TestShellBellDrainsOffTheMoveTotal(t *testing.T) {
	d := loadDex(t)

	t.Run("no heal below the threshold", func(t *testing.T) {
		s, err := NewBattle(d, "b", "Holder", []int{143}, "Wall", []int{95}, 5) // Onix: huge Def
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		holder := s.Active(0)
		holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		holder.Item = ItemShellBell
		holder.HP = holder.MaxHP / 2
		holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

		// Drive the drain directly with a damage total too small to earn a
		// point, which is the case the round-up floor got wrong.
		var log []LogLine
		before := holder.HP
		applyItemDrainOnDamageDealt(s, 0, 4, &log)
		if holder.HP != before {
			t.Errorf("healed %d off 4 damage; an eighth of 4 truncates to 0", holder.HP-before)
		}
		applyItemDrainOnDamageDealt(s, 0, 16, &log)
		if got := holder.HP - before; got != 2 {
			t.Errorf("healed %d off 16 damage, want 2", got)
		}
	})

	t.Run("multi-hit uses the total", func(t *testing.T) {
		if _, ok := d.Moves["double-kick"]; !ok {
			t.Skip("double-kick not in the curated move set")
		}
		s, err := NewBattle(d, "b", "Holder", []int{106}, "Target", []int{143}, 5) // Hitmonlee
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		holder := s.Active(0)
		holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		holder.Item = ItemShellBell
		holder.HP = holder.MaxHP / 2
		holder.Moves = []MoveSlot{{MoveID: "double-kick", PP: 30, MaxPP: 30}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		selfBefore, foeBefore := holder.HP, s.Active(1).HP

		splashTurn(d, s)

		dealt := foeBefore - s.Active(1).HP
		healed := s.Active(0).HP - selfBefore
		if dealt <= 0 {
			t.Fatalf("setup: no damage dealt")
		}
		// One drain off the total, not the sum of two truncated eighths.
		if want := dealt / 8; healed != want {
			t.Errorf("healed %d off %d total damage, want %d (an eighth of the total, "+
				"not of each strike)", healed, dealt, want)
		}
	})
}

// TestContactReactionCannotStrandAFaintedAttacker: Rocky Helmet KOs the
// attacker from inside dealDamage, and applyDamageEffects then ran the move's
// self-block unconditionally — so a drain move healed the corpse and left a
// Pokémon flagged Fainted with positive HP, stranding the side in the replace
// phase showing live HP.
func TestContactReactionCannotStrandAFaintedAttacker(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Moves["leech-life"]; !ok {
		t.Skip("leech-life not in the curated move set")
	}
	s, err := NewBattle(d, "b", "Attacker", []int{143}, "Helmet", []int{143}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	atk := s.Active(0)
	atk.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	atk.Moves = []MoveSlot{{MoveID: "leech-life", PP: 15, MaxPP: 15}}
	// One HP above the helmet's chip, so the recoil is exactly lethal.
	atk.HP = atk.MaxHP/6 - 1
	if atk.HP < 1 {
		atk.HP = 1
	}
	s.Active(1).Item = ItemRockyHelmet
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	log := splashTurn(d, s)

	p := &s.Sides[0].Team[0]
	if p.Fainted && p.HP > 0 {
		t.Errorf("attacker is Fainted with HP=%d — the drain healed a corpse; log: %v", p.HP, log)
	}
	if err := ValidateStateInvariants(s); err != nil {
		t.Errorf("state invariants broken: %v", err)
	}
}

// TestBigRootBoostsSeedAndRootHeals: canon's Big Root list is drain moves plus
// Leech Seed, Aqua Ring and Ingrain. The last three heal outside the
// declarative Effect.Drain path and were silently missing the item.
func TestBigRootBoostsSeedAndRootHeals(t *testing.T) {
	d := loadDex(t)

	t.Run("leech seed", func(t *testing.T) {
		drained := func(item ItemKind) int {
			s, err := NewBattle(d, "b", "Seeder", []int{143}, "Seeded", []int{143}, 5)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			s.Active(0).Item = item
			s.Active(0).HP = s.Active(0).MaxHP / 2
			s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			s.Active(1).Volatiles.LeechSeed = &LeechSeedState{SourceSide: 0}
			before := s.Active(0).HP
			splashTurn(d, s)
			return s.Active(0).HP - before
		}
		bare := drained(ItemNone)
		if bare <= 0 {
			t.Fatalf("setup: Leech Seed healed nothing")
		}
		if root := drained(ItemBigRoot); root <= bare {
			t.Errorf("Big Root did not boost the Leech Seed drain: %d vs %d bare", root, bare)
		}
	})

	t.Run("aqua ring", func(t *testing.T) {
		healed := func(item ItemKind) int {
			_, s := berryBattle(t, item)
			s.Active(0).HP = s.Active(0).MaxHP / 2
			s.Active(0).Volatiles.AquaRing = true
			before := s.Active(0).HP
			splashTurn(d, s)
			return s.Active(0).HP - before
		}
		bare := healed(ItemNone)
		if bare <= 0 {
			t.Fatalf("setup: Aqua Ring healed nothing")
		}
		if root := healed(ItemBigRoot); root <= bare {
			t.Errorf("Big Root did not boost the Aqua Ring heal: %d vs %d bare", root, bare)
		}
	})
}

// --- third review pass: the event-reaction batch ---

// TestWhiteHerbRestoresBeforeTheHolderActs is the timing bug. The herb was
// checked only at the damaging-move tail and in the end-of-turn sweep, so a
// foe's Growl (a status move) or an Intimidate lead lowered the holder's stats
// and the herb didn't undo it until *after* the holder had already attacked at
// the reduced stat — which is the one thing the item exists to prevent.
func TestWhiteHerbRestoresBeforeTheHolderActs(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Herb", []int{143}, "Growler", []int{135}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	holder.Item = ItemWhiteHerb
	holder.Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	// Jolteon is far faster, so Growl lands before the holder swings.
	s.Active(1).Moves = []MoveSlot{{MoveID: "growl", PP: 40, MaxPP: 40}}

	log := splashTurn(d, s)

	// The restore must precede the holder's own move in the log.
	restoreIdx, moveIdx := -1, -1
	for i, l := range log {
		if restoreIdx < 0 && logLineHas(l, "restored its lowered stats") {
			restoreIdx = i
		}
		if moveIdx < 0 && l.Type == "move" && l.Side == 0 && logLineHas(l, " used ") {
			moveIdx = i
		}
	}
	if restoreIdx < 0 {
		t.Fatalf("White Herb never fired; log: %v", log)
	}
	if moveIdx < 0 {
		t.Fatalf("the holder never moved; log: %v", log)
	}
	if restoreIdx > moveIdx {
		t.Errorf("White Herb restored at line %d, after the holder attacked at line %d — "+
			"it attacked at the lowered stat it was holding the herb to avoid; log: %v",
			restoreIdx, moveIdx, log)
	}
	if s.Active(0).Stages.Atk != 0 {
		t.Errorf("Attack left at %d", s.Active(0).Stages.Atk)
	}
}

// TestMentalHerbFreesTheHolderBeforeItsTurnIsWasted: same bug, the Taunt half.
// A Taunt landing before the holder's move meant the move was refused *and* the
// herb popped afterwards — the turn lost and the item spent for nothing.
func TestMentalHerbFreesTheHolderBeforeItsTurnIsWasted(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Herb", []int{143}, "Taunter", []int{135}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	holder.Item = ItemMentalHerb
	holder.Moves = []MoveSlot{{MoveID: "swords-dance", PP: 20, MaxPP: 20}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "taunt", PP: 20, MaxPP: 20}}

	log := splashTurn(d, s)

	if s.Active(0).Volatiles.Taunt != nil {
		t.Errorf("Mental Herb did not lift the Taunt")
	}
	if got := s.Active(0).Stages.Atk; got != 2 {
		t.Errorf("Swords Dance was refused: Atk = %d, want 2 — the herb fired too late; log: %v", got, log)
	}
}

// TestShellBellIgnoresSubstituteDamage is a regression from the fix that moved
// Shell Bell onto the move's damage total: the total accumulated the figure a
// Substitute absorbed, so the attacker healed off a hit that never reached the
// target. The old per-hit hook sat below the substitute early-return and
// couldn't do this.
func TestShellBellIgnoresSubstituteDamage(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Holder", []int{143}, "Doll", []int{143}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	holder.Item = ItemShellBell
	holder.HP = holder.MaxHP / 2
	holder.Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	def := s.Active(1)
	def.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	def.Volatiles.Substitute = &SubstituteState{HP: 10000}
	before := holder.HP

	log := splashTurn(d, s)

	if !logHas(log, "substitute took the damage") {
		t.Fatalf("setup: the doll did not absorb the hit; log: %v", log)
	}
	if got := s.Active(0).HP; got != before {
		t.Errorf("Shell Bell healed %d off damage a Substitute absorbed; log: %v", got-before, log)
	}
}

// TestBlunderPolicyIgnoresRefusals: a move refused by Safety Goggles or
// Soundproof never rolled to hit, so there was no blunder. resolveAccuracy
// returned false for all three cases and the caller couldn't tell them apart.
func TestBlunderPolicyIgnoresRefusals(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Policy", []int{45}, "Goggles", []int{143}, 5) // Vileplume
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	atk := s.Active(0)
	atk.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	atk.Item = ItemBlunderPolicy
	atk.Moves = []MoveSlot{{MoveID: "sleep-powder", PP: 15, MaxPP: 15}}
	s.Active(1).Item = ItemSafetyGoggles
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	log := splashTurn(d, s)

	if !logHas(log, "Safety Goggles") {
		t.Fatalf("setup: the goggles never refused the powder move; log: %v", log)
	}
	if s.Active(0).Stages.Spe != 0 {
		t.Errorf("Blunder Policy fired on a refusal rather than a miss; log: %v", log)
	}
	if s.Active(0).Item != ItemBlunderPolicy {
		t.Errorf("Blunder Policy consumed on a refusal")
	}
}

// TestBlunderPolicyAnswersAHundredAccuracyMiss: the old `Accuracy >= 100` gate
// excluded exactly the misses that hurt most — a sure-thing move whiffing into
// a boosted-evasion target.
func TestBlunderPolicyAnswersAHundredAccuracyMiss(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(1); seed <= 80; seed++ {
		s, err := NewBattle(d, "b", "Policy", []int{143}, "Evasive", []int{143}, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = ItemBlunderPolicy
		s.Active(0).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}} // 100 accuracy
		s.Active(1).Stages.Eva = 6
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

		log := splashTurn(d, s)
		if !logHas(log, "attack missed") {
			continue
		}
		if got := s.Active(0).Stages.Spe; got != 2 {
			t.Fatalf("seed %d: a 100-accuracy move missed and Blunder Policy gave %d, want 2; log: %v",
				seed, got, log)
		}
		return
	}
	t.Fatal("no miss across 80 seeds — the fixture stopped exercising the path")
}

// TestFlinchItemsRespectAddedEffectGuards: canon implements King's Rock as an
// added effect pushed onto the move, so Shield Dust and Covert Cloak refuse it.
func TestFlinchItemsRespectAddedEffectGuards(t *testing.T) {
	d := loadDex(t)
	flinches := func(defAbility AbilityKind, defItem ItemKind) int {
		n := 0
		for seed := uint64(1); seed <= 250; seed++ {
			s, err := NewBattle(d, "b", "Rock", []int{143}, "Target", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability = AbilityNone
			s.Active(0).Stages.Spe = 6
			s.Active(0).Item = ItemKingsRock
			s.Active(0).Moves = []MoveSlot{{MoveID: "water-gun", PP: 25, MaxPP: 25}}
			s.Active(1).Ability = defAbility
			s.Active(1).Item = defItem
			s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			if logHas(splashTurn(d, s), "flinched") {
				n++
			}
		}
		return n
	}
	if bare := flinches(AbilityNone, ItemNone); bare == 0 {
		t.Fatalf("setup: King's Rock never flinched a bare target")
	}
	if got := flinches("shield-dust", ItemNone); got != 0 {
		t.Errorf("King's Rock flinched through Shield Dust %d times", got)
	}
	if got := flinches(AbilityNone, ItemCovertCloak); got != 0 {
		t.Errorf("King's Rock flinched through a Covert Cloak %d times", got)
	}
}

// TestMetronomeStreakRestartsAfterABreak: zeroing the count but keeping the
// move ID let the next use re-match and tick straight back to x1.2, so a broken
// streak resumed instead of restarting.
func TestMetronomeStreakRestartsAfterABreak(t *testing.T) {
	d := loadDex(t)
	holder := buildPokemon(d, d.Species[143])
	holder.Ability = AbilityNone
	holder.Item = ItemMetronome
	m := d.Moves["body-slam"]

	tickMetronome(&holder, m)
	tickMetronome(&holder, m)
	if got := metronomeMult(&holder, m); got != 1.2 {
		t.Fatalf("setup: multiplier after two uses = %v, want 1.2", got)
	}
	breakMetronomeStreak(&holder)
	tickMetronome(&holder, m)
	if got := metronomeMult(&holder, m); got != 1 {
		t.Errorf("the use after a broken streak gave %v, want 1.0 — the streak resumed "+
			"instead of restarting", got)
	}
}

// TestZoomLensPaysOutAgainstASwitchingTarget: the flag was only set by the
// mover loop, so a target that switched in — and therefore will not act at all
// — read as "still to move" and the lens never paid out.
func TestZoomLensPaysOutAgainstASwitchingTarget(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Lens", []int{143}, "Switcher", []int{143, 6}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Ability = AbilityNone
	s.Active(0).Item = ItemZoomLens
	s.Active(0).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}

	var log []LogLine
	doSwitch(s, 1, 1, NewRNG(1), &log)

	if got := itemAccuracyMult(s, 0); got != 1.2 {
		t.Errorf("Zoom Lens multiplier vs a switched-in target = %v, want 1.2 — a Pokémon "+
			"that just switched in will not move this turn", got)
	}
}

// TestAccuracyItemsDoNotTouchOHKOMoves: canon bypasses the accuracy modifier
// chain for OHKO moves, the same exclusion the Micle Berry check already made.
func TestAccuracyItemsDoNotTouchOHKOMoves(t *testing.T) {
	d := loadDex(t)
	hits := func(atkItem ItemKind) int {
		n := 0
		for seed := uint64(1); seed <= 300; seed++ {
			s, err := NewBattle(d, "b", "Driller", []int{112}, "Target", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			s.Active(0).Item = atkItem
			s.Active(0).Moves = []MoveSlot{{MoveID: "horn-drill", PP: 5, MaxPP: 5}}
			s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			if !logHas(splashTurn(d, s), "attack missed") {
				n++
			}
		}
		return n
	}
	bare := hits(ItemNone)
	if lens := hits(ItemWideLens); lens != bare {
		t.Errorf("Wide Lens changed OHKO accuracy: %d vs %d bare (of 300)", lens, bare)
	}
}

// TestReactiveBoostItemsAreNotSpentOnAKO: the hooks returned true
// unconditionally, so a hit that KO'd the holder still announced the item
// immediately before the faint. Canon leaves it on the fainted Pokémon.
func TestReactiveBoostItemsAreNotSpentOnAKO(t *testing.T) {
	d := loadDex(t)
	// Venusaur (Grass/Poison) takes 2x from Charizard's Fire.
	s, err := NewBattle(d, "b", "Policy", []int{3}, "Attacker", []int{6}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	holder.Item = ItemWeaknessPolicy
	holder.HP = 1 // the super-effective hit is lethal
	holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "flamethrower", PP: 15, MaxPP: 15}}

	log := splashTurn(d, s)

	if !s.Sides[0].Team[0].Fainted {
		t.Fatalf("setup: the holder survived; log: %v", log)
	}
	if logHas(log, "used its Weakness Policy") {
		t.Errorf("Weakness Policy announced on the hit that KO'd its holder; log: %v", log)
	}
	if s.Sides[0].Team[0].Item != ItemWeaknessPolicy {
		t.Errorf("Weakness Policy consumed by a KO")
	}
}

// TestWhiteHerbAnswersIntimidateOnEntry covers the other half of the herb
// timing fix. The status-move path is handled by the check at the tail of the
// status branch; an Intimidate lead lowers Attack from applyOnSwitchIn, which
// never goes near that branch — the herb has to answer from applyStagesFromFoe
// itself, which is where canon's onUpdate effectively sits.
func TestWhiteHerbAnswersIntimidateOnEntry(t *testing.T) {
	d := loadDex(t)
	// Intimidate fires from the *Intimidator's* own switch-in, so Arcanine is
	// the one coming in — the herb holder is already on the field taking it.
	s, err := NewBattle(d, "b", "Herb", []int{143}, "Intimidator", []int{6, 59}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability = AbilityNone
	holder.Item = ItemWhiteHerb
	s.Sides[1].Team[1].Ability = AbilityIntimidate

	var log []LogLine
	doSwitch(s, 1, 1, NewRNG(1), &log)

	if !logHas(log, "Intimidate") {
		t.Fatalf("setup: Intimidate did not fire on entry; log: %v", log)
	}
	if got := s.Active(0).Stages.Atk; got != 0 {
		t.Errorf("White Herb did not undo Intimidate on entry: Atk = %d; log: %v", got, log)
	}
	if s.Active(0).Item != ItemNone {
		t.Errorf("White Herb not consumed")
	}
}
