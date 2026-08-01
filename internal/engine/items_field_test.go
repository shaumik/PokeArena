package engine

import (
	"testing"
)

// items_field_test.go covers the rule-changing family. These items don't move a
// damage number, they flip a decision the engine makes — does this hazard
// apply, may this Pokémon switch, is it grounded — so each test drives the
// decision itself rather than reading a multiplier back.

// --- self-inflicted status ---

// TestStatusOrbsInflictOnTheirHolder, and do it through inflictStatus so every
// existing guard still applies.
func TestStatusOrbsInflictOnTheirHolder(t *testing.T) {
	for _, tc := range []struct {
		item ItemKind
		want StatusCond
	}{
		{ItemFlameOrb, StatusBurn},
		{ItemToxicOrb, StatusToxic},
	} {
		t.Run(string(tc.item), func(t *testing.T) {
			d, s := berryBattle(t, tc.item)
			splashTurn(d, s)
			if got := s.Active(0).Status; got != tc.want {
				t.Errorf("status after one turn = %q, want %q", got, tc.want)
			}
			// Not consumed — an orb keeps trying.
			if s.Active(0).Item != tc.item {
				t.Errorf("%s was consumed", tc.item)
			}
		})
	}
}

// TestStatusOrbCostsNoDamageOnTheTurnItFires is the reason the orbs sit in the
// late residual slot: the free turn is the whole point of running one. Ticking
// them with the other residuals would charge the holder immediately.
func TestStatusOrbCostsNoDamageOnTheTurnItFires(t *testing.T) {
	d, s := berryBattle(t, ItemToxicOrb)
	holder := s.Active(0)
	before := holder.HP

	splashTurn(d, s)

	if s.Active(0).Status != StatusToxic {
		t.Fatalf("Toxic Orb did not fire")
	}
	if got := s.Active(0).HP; got != before {
		t.Errorf("holder took %d damage on the turn the orb fired; the late slot exists to avoid exactly that",
			before-got)
	}
	// The turn after, the poison does tick.
	splashTurn(d, s)
	if s.Active(0).HP >= before {
		t.Errorf("toxic never chipped on the following turn")
	}
}

// TestStatusOrbRespectsTypeImmunity: the orb goes through inflictStatus, so a
// Fire-type simply can't be burned by a Flame Orb. A bespoke assignment would
// have skipped that.
func TestStatusOrbRespectsTypeImmunity(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Holder", []int{6}, "Foe", []int{143}, 1) // Charizard is Fire
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	s.Active(0).Item = ItemFlameOrb
	s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	splashTurn(d, s)

	if got := s.Active(0).Status; got != StatusNone {
		t.Errorf("a Fire-type was burned by its Flame Orb: status = %q", got)
	}
}

// --- typing-keyed residuals ---

// TestBlackSludgeHealsPoisonAndHurtsEverythingElse: one item, two behaviors,
// keyed on typing — so both halves need their own case.
func TestBlackSludgeHealsPoisonAndHurtsEverythingElse(t *testing.T) {
	d := loadDex(t)
	run := func(dexNo int) (before, after, maxHP int) {
		s, err := NewBattle(d, "b", "Holder", []int{dexNo}, "Foe", []int{143}, 1)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = ItemBlackSludge
		s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(0).HP = s.Active(0).MaxHP / 2
		before = s.Active(0).HP
		splashTurn(d, s)
		return before, s.Active(0).HP, s.Active(0).MaxHP
	}

	// Muk is Poison — it heals 1/16.
	before, after, maxHP := run(89)
	if want := before + maxHP/16; after != want {
		t.Errorf("Poison-type holder: %d → %d, want %d (+1/16)", before, after, want)
	}
	// Snorlax is not — it loses 1/8.
	before, after, maxHP = run(143)
	if want := before - maxHP/8; after != want {
		t.Errorf("non-Poison holder: %d → %d, want %d (-1/8)", before, after, want)
	}
}

// TestStickyBarbTransfersToABareContactAttacker: the transfer is the item's
// defining behavior, and it has two gates — contact, and the attacker holding
// nothing.
func TestStickyBarbTransfersToABareContactAttacker(t *testing.T) {
	d := loadDex(t)
	setup := func(atkItem ItemKind, moveID string) *BattleState {
		s, err := NewBattle(d, "b", "Attacker", []int{143}, "Barbed", []int{143}, 5)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = atkItem
		s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 20, MaxPP: 20}}
		s.Active(1).Item = ItemStickyBarb
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		return s
	}

	// Bare attacker, contact move: the barb changes hands.
	s := setup(ItemNone, "body-slam")
	splashTurn(d, s)
	if s.Active(0).Item != ItemStickyBarb {
		t.Errorf("barb did not transfer to a bare contact attacker (attacker holds %q)", s.Active(0).Item)
	}
	if s.Active(1).Item != ItemNone {
		t.Errorf("barb stayed on its original holder as well as transferring")
	}

	// Non-contact move: no transfer.
	s = setup(ItemNone, "surf")
	splashTurn(d, s)
	if s.Active(0).Item != ItemNone {
		t.Errorf("barb transferred off a non-contact move")
	}

	// Attacker already holding something: keeps its own item.
	s = setup(ItemLeftovers, "body-slam")
	splashTurn(d, s)
	if s.Active(0).Item != ItemLeftovers {
		t.Errorf("barb displaced the attacker's own item: now %q", s.Active(0).Item)
	}
	if s.Active(1).Item != ItemStickyBarb {
		t.Errorf("barb left its holder without landing anywhere")
	}
}

// --- field-duration extenders ---

// TestFieldExtendersLengthenWhatTheHolderSets covers all six through their real
// setters, since the whole mechanic is "read the setter's item at creation".
func TestFieldExtendersLengthenWhatTheHolderSets(t *testing.T) {
	d := loadDex(t)
	newBattle := func(item ItemKind) *BattleState {
		s, err := NewBattle(d, "b", "Setter", []int{143}, "Foe", []int{143}, 1)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = item
		return s
	}

	t.Run("light-clay", func(t *testing.T) {
		bare := newBattle(ItemNone)
		var log []LogLine
		applyScreenSetter(bare, 0, ScreenReflect, &log)
		if got := bare.Sides[0].Conditions.Reflect.TurnsLeft; got != defaultScreenTurns {
			t.Fatalf("bare screen lasts %d, want %d", got, defaultScreenTurns)
		}
		clay := newBattle(ItemLightClay)
		applyScreenSetter(clay, 0, ScreenReflect, &log)
		if got := clay.Sides[0].Conditions.Reflect.TurnsLeft; got != extendedFieldTurns {
			t.Errorf("Light Clay screen lasts %d, want %d", got, extendedFieldTurns)
		}
	})

	t.Run("terrain-extender", func(t *testing.T) {
		var log []LogLine
		bare := newBattle(ItemNone)
		applyTerrainSetter(bare, 0, TerrainElectric, &log)
		baseTurns := bare.Terrain.TurnsLeft
		ext := newBattle(ItemTerrainExtender)
		applyTerrainSetter(ext, 0, TerrainElectric, &log)
		if ext.Terrain.TurnsLeft != extendedFieldTurns || baseTurns == extendedFieldTurns {
			t.Errorf("Terrain Extender: %d turns (bare %d), want %d", ext.Terrain.TurnsLeft, baseTurns, extendedFieldTurns)
		}
	})

	for _, tc := range []struct {
		item    ItemKind
		weather WeatherKind
	}{
		{ItemDampRock, WeatherRain},
		{ItemHeatRock, WeatherSun},
		{ItemSmoothRock, WeatherSandstorm},
		{ItemIcyRock, WeatherSnow},
	} {
		t.Run(string(tc.item), func(t *testing.T) {
			var log []LogLine
			ext := newBattle(tc.item)
			applyWeatherSetter(ext, 0, tc.weather, &log)
			if got := ext.Weather.TurnsLeft; got != extendedFieldTurns {
				t.Errorf("%s on %s: %d turns, want %d", tc.item, tc.weather, got, extendedFieldTurns)
			}
			// A rock must not lengthen a weather it doesn't cover.
			other := WeatherRain
			if tc.weather == WeatherRain {
				other = WeatherSun
			}
			mismatch := newBattle(tc.item)
			applyWeatherSetter(mismatch, 0, other, &log)
			if got := mismatch.Weather.TurnsLeft; got != defaultWeatherTurns {
				t.Errorf("%s lengthened %s, which it does not cover: %d turns", tc.item, other, got)
			}
		})
	}
}

// --- partial-trap modifiers ---

// TestBindingBandAndGripClawTuneTheTrap: both live on the trapper but act on
// the target, and both are snapshotted at application time — so the assertion
// is on the trap the target ends up carrying.
func TestBindingBandAndGripClawTuneTheTrap(t *testing.T) {
	d := loadDex(t)
	trap := func(item ItemKind) *PartialTrapState {
		s, err := NewBattle(d, "b", "Trapper", []int{143}, "Target", []int{143}, 5)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Item = item
		var log []LogLine
		applyPartialTrapVolatile(s.Active(1), 1, d.Moves["bind"], s, NewRNG(3), &log)
		return s.Active(1).Volatiles.PartialTrap
	}

	bare := trap(ItemNone)
	if got := bare.Chip(240); got != 240/partialTrapDenom {
		t.Errorf("bare trap chip = %d, want %d", got, 240/partialTrapDenom)
	}
	band := trap(ItemBindingBand)
	if got := band.Chip(240); got != 240/bindingBandDenom {
		t.Errorf("Binding Band chip = %d, want %d", got, 240/bindingBandDenom)
	}
	if band.Turns != bare.Turns {
		t.Errorf("Binding Band changed the duration: %d vs %d", band.Turns, bare.Turns)
	}
	claw := trap(ItemGripClaw)
	if claw.Turns != gripClawTurns {
		t.Errorf("Grip Claw trap lasts %d turns, want %d", claw.Turns, gripClawTurns)
	}
	if got := claw.Chip(240); got != 240/partialTrapDenom {
		t.Errorf("Grip Claw changed the chip: %d", got)
	}
}

// TestPartialTrapChipDefaultsWhenUnset: a state deserialized from before the
// field existed carries ChipDenom 0, which must read as the default rather than
// dividing by zero.
func TestPartialTrapChipDefaultsWhenUnset(t *testing.T) {
	pt := &PartialTrapState{Turns: 3, MoveName: "Bind"}
	if got := pt.Chip(240); got != 240/partialTrapDenom {
		t.Errorf("unset ChipDenom gave %d, want the default %d", got, 240/partialTrapDenom)
	}
	if got := (&PartialTrapState{}).Chip(4); got != 1 {
		t.Errorf("a tiny max HP must still chip at least 1, got %d", got)
	}
}

// --- immunity grants and removals ---

// TestHeavyDutyBootsSkipEveryHazardLayer.
func TestHeavyDutyBootsSkipEveryHazardLayer(t *testing.T) {
	d := loadDex(t)
	entry := func(item ItemKind) (hpLost int, status StatusCond) {
		s, err := NewBattle(d, "b", "P1", []int{143, 76}, "P2", []int{143}, 1)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		h := &s.Sides[0].Conditions.Hazards
		h.StealthRock, h.Spikes, h.ToxicSpikes = true, 3, 2
		in := &s.Sides[0].Team[1] // Golem: grounded, so every layer applies
		in.Ability = AbilityNone
		in.Item = item
		before := in.HP
		var log []LogLine
		doSwitch(s, 0, 1, NewRNG(1), &log)
		return before - s.Active(0).HP, s.Active(0).Status
	}
	bareLost, bareStatus := entry(ItemNone)
	if bareLost <= 0 {
		t.Fatalf("setup: a bare switch-in took no hazard damage")
	}
	if bareStatus == StatusNone {
		t.Fatalf("setup: Toxic Spikes did not poison a bare grounded switch-in")
	}
	bootLost, bootStatus := entry(ItemHeavyDutyBoots)
	if bootLost != 0 {
		t.Errorf("Heavy-Duty Boots holder took %d hazard damage", bootLost)
	}
	if bootStatus != StatusNone {
		t.Errorf("Heavy-Duty Boots holder was poisoned by Toxic Spikes: %q", bootStatus)
	}
}

// TestSafetyGogglesBlockSandAndPowder covers both halves of the item.
func TestSafetyGogglesBlockSandAndPowder(t *testing.T) {
	d := loadDex(t)

	// Sandstorm chip.
	sand := func(item ItemKind) int {
		s, err := NewBattle(d, "b", "Holder", []int{143}, "Foe", []int{143}, 1)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = item
		s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Weather = &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}
		before := s.Active(0).HP
		splashTurn(d, s)
		return before - s.Active(0).HP
	}
	if sand(ItemNone) <= 0 {
		t.Fatalf("setup: a bare holder took no sandstorm chip")
	}
	if got := sand(ItemSafetyGoggles); got != 0 {
		t.Errorf("Safety Goggles holder took %d sandstorm damage", got)
	}

	// Powder move.
	powder := func(item ItemKind) StatusCond {
		s, err := NewBattle(d, "b", "Holder", []int{143}, "Foe", []int{45}, 3) // Vileplume
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = item
		s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "stun-spore", PP: 30, MaxPP: 30}}
		splashTurn(d, s)
		return s.Active(0).Status
	}
	if got := powder(ItemNone); got != StatusParalysis {
		t.Skipf("setup: Stun Spore did not land on a bare holder (got %q)", got)
	}
	if got := powder(ItemSafetyGoggles); got != StatusNone {
		t.Errorf("Safety Goggles holder was hit by a powder move: %q", got)
	}
}

// TestShedShellEscapesEveryTrap.
func TestShedShellEscapesEveryTrap(t *testing.T) {
	d := loadDex(t)
	canSwitch := func(item ItemKind, mut func(*Pokemon)) bool {
		s, err := NewBattle(d, "b", "P1", []int{143, 6}, "P2", []int{143}, 1)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		act := s.Active(0)
		act.Item = item
		mut(act)
		for _, a := range LegalActionsDex(d, s, 0) {
			if a.Kind == ActionSwitch {
				return true
			}
		}
		return false
	}
	for _, tc := range []struct {
		name string
		mut  func(*Pokemon)
	}{
		{"partial trap", func(p *Pokemon) {
			p.Volatiles.PartialTrap = &PartialTrapState{Turns: 3, MoveName: "Bind"}
		}},
		{"ingrain", func(p *Pokemon) { p.Volatiles.Ingrain = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if canSwitch(ItemNone, tc.mut) {
				t.Fatalf("setup: a bare Pokémon could switch out of %s", tc.name)
			}
			if !canSwitch(ItemShedShell, tc.mut) {
				t.Errorf("Shed Shell did not escape %s", tc.name)
			}
		})
	}
}

// TestIronBallHalvesSpeedAndGrounds.
func TestIronBallHalvesSpeedAndGrounds(t *testing.T) {
	d := loadDex(t)
	// Speed.
	p := buildPokemon(d, d.Species[135]) // Jolteon
	p.Ability = AbilityNone
	base := effectiveSpeed(&p, nil)
	p.Item = ItemIronBall
	if got, want := effectiveSpeed(&p, nil), base/2; got != want {
		t.Errorf("Iron Ball speed = %d, want %d (half of %d)", got, want, base)
	}
	// Grounding: Charizard is Flying, so it normally floats.
	flyer := buildPokemon(d, d.Species[6])
	flyer.Ability = AbilityNone
	if isGrounded(&flyer) {
		t.Fatalf("setup: a Flying-type reads as grounded already")
	}
	flyer.Item = ItemIronBall
	if !isGrounded(&flyer) {
		t.Errorf("Iron Ball did not ground a Flying-type")
	}
	// It beats Levitate too.
	lev := buildPokemon(d, d.Species[94]) // Gengar
	lev.Ability = AbilityLevitate
	if isGrounded(&lev) {
		t.Fatalf("setup: a Levitate holder reads as grounded already")
	}
	lev.Item = ItemIronBall
	if !isGrounded(&lev) {
		t.Errorf("Iron Ball did not beat Levitate")
	}
}

// TestAirBalloonFloatsThenPops: the immunity and the lifecycle are one item, so
// both are asserted in sequence — the balloon must stop working after it pops.
func TestAirBalloonFloatsThenPops(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Balloon", []int{143}, "Digger", []int{51}, 5) // Dugtrio
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	holder.Item = ItemAirBalloon
	holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "earthquake", PP: 10, MaxPP: 10}}

	before := holder.HP
	splashTurn(d, s)
	if s.Active(0).HP != before {
		t.Errorf("Air Balloon holder took Ground damage: %d → %d", before, s.Active(0).HP)
	}
	if s.Active(0).Item != ItemAirBalloon {
		t.Errorf("balloon popped on a move it made the holder immune to")
	}

	// A non-Ground hit pops it, and then Ground connects.
	s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	log := splashTurn(d, s)
	if s.Active(0).Item != ItemNone {
		t.Fatalf("balloon did not pop when hit; log: %v", log)
	}
	if !logHas(log, "Air Balloon popped") {
		t.Errorf("no pop line; log: %v", log)
	}
	s.Active(1).Moves = []MoveSlot{{MoveID: "earthquake", PP: 10, MaxPP: 10}}
	before = s.Active(0).HP
	splashTurn(d, s)
	if s.Active(0).HP == before {
		t.Errorf("Ground still missed after the balloon popped")
	}
}

// TestRingTargetGivesUpEveryImmunity, including the ability-granted ones.
func TestRingTargetGivesUpEveryImmunity(t *testing.T) {
	d := loadDex(t)
	hit := func(defDex int, ability AbilityKind, item ItemKind, moveID string) int {
		atk := buildPokemon(d, d.Species[143])
		def := buildPokemon(d, d.Species[defDex])
		atk.Ability = AbilityNone
		def.Ability = ability
		def.Item = item
		return ExpectedDamage(d, &atk, &def, d.Moves[moveID], nil, nil, nil)
	}
	// Type-chart immunity: Normal can't touch a Ghost.
	if got := hit(94, AbilityNone, ItemNone, "body-slam"); got != 0 {
		t.Fatalf("setup: Normal already damages a Ghost for %d", got)
	}
	if got := hit(94, AbilityNone, ItemRingTarget, "body-slam"); got <= 0 {
		t.Errorf("Ring Target did not lift the Ghost type immunity")
	}
	// Ability immunity: Levitate vs Ground.
	if got := hit(94, AbilityLevitate, ItemNone, "earthquake"); got != 0 {
		t.Fatalf("setup: Levitate already takes Ground damage (%d)", got)
	}
	if got := hit(94, AbilityLevitate, ItemRingTarget, "earthquake"); got <= 0 {
		t.Errorf("Ring Target did not lift Levitate")
	}
}

// TestUtilityUmbrellaIgnoresRainAndSunOnly: it keeps rain and sun off, not grit
// and cold, so sandstorm must be untouched.
func TestUtilityUmbrellaIgnoresRainAndSunOnly(t *testing.T) {
	d := loadDex(t)
	dmg := func(item ItemKind, w *WeatherState) int {
		atk := buildPokemon(d, d.Species[9]) // Blastoise
		def := buildPokemon(d, d.Species[143])
		atk.Ability, def.Ability = AbilityNone, AbilityNone
		def.Item = item
		return ExpectedDamage(d, &atk, &def, d.Moves["surf"], w, nil, nil)
	}
	rain := &WeatherState{Kind: WeatherRain, TurnsLeft: 5}
	clear := dmg(ItemNone, nil)
	boosted := dmg(ItemNone, rain)
	if boosted <= clear {
		t.Fatalf("setup: rain did not boost Water damage (%d vs %d)", boosted, clear)
	}
	if got := dmg(ItemUtilityUmbrella, rain); got != clear {
		t.Errorf("umbrella holder took rain-boosted damage: %d, want the clear-weather %d", got, clear)
	}

	// Sandstorm chip still applies — the umbrella covers rain and sun only.
	p := buildPokemon(d, d.Species[143])
	p.Ability = AbilityNone
	p.Item = ItemUtilityUmbrella
	if got := weatherResidual(&WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}, &p); got == 0 {
		t.Errorf("Utility Umbrella blocked sandstorm chip; it covers rain and sun only")
	}
}

// TestProtectivePadsBlockEveryContactEffect — unlike Punching Glove, the pads
// cover the whole move list.
func TestProtectivePadsBlockEveryContactEffect(t *testing.T) {
	d := loadDex(t)
	chip := func(item ItemKind) int {
		s, err := NewBattle(d, "b", "Padded", []int{143}, "Helmet", []int{143}, 5)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = item
		s.Active(0).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
		s.Active(1).Item = ItemRockyHelmet
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		before := s.Active(0).HP
		splashTurn(d, s)
		return before - s.Active(0).HP
	}
	if chip(ItemNone) <= 0 {
		t.Fatalf("setup: Rocky Helmet did not chip a bare attacker")
	}
	if got := chip(ItemProtectivePads); got != 0 {
		t.Errorf("Protective Pads holder still took %d from Rocky Helmet", got)
	}
}

// TestCovertCloakBlocksAddedEffects but not the damage itself.
func TestCovertCloakBlocksAddedEffects(t *testing.T) {
	d := loadDex(t)
	// Body Slam's 30% paralysis is the added effect under test; over a seed
	// sweep a bare holder gets paralyzed and a cloaked one never does.
	paralyzed := func(item ItemKind) int {
		n := 0
		for seed := uint64(1); seed <= 60; seed++ {
			s, err := NewBattle(d, "b", "Cloak", []int{143}, "Foe", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			s.Active(0).Item = item
			s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
			splashTurn(d, s)
			if s.Active(0).Status == StatusParalysis {
				n++
			}
		}
		return n
	}
	if paralyzed(ItemNone) == 0 {
		t.Fatalf("setup: Body Slam never paralyzed a bare holder across 60 seeds")
	}
	if got := paralyzed(ItemCovertCloak); got != 0 {
		t.Errorf("Covert Cloak holder was paralyzed %d times by an added effect", got)
	}
}

// TestClearAmuletRefusesFoeDropsButNotSelfDrops: the asymmetry is the point —
// Clear Body has the same one, and an item that blocked the holder's own Close
// Combat drops would be strictly better than canon.
func TestClearAmuletRefusesFoeDropsButNotSelfDrops(t *testing.T) {
	d, s := berryBattle(t, ItemClearAmulet)
	holder := s.Active(0)

	var log []LogLine
	applyStagesFromFoe(holder, 0, "attack", -1, s, &log)
	if holder.Stages.Atk != 0 {
		t.Errorf("Clear Amulet allowed a foe-induced drop: Atk = %d", holder.Stages.Atk)
	}
	if !logHas(log, "prevented the stat drop") {
		t.Errorf("no refusal line; log: %v", log)
	}

	applyStages(holder, 0, "defense", -1, &log)
	if holder.Stages.Def != -1 {
		t.Errorf("Clear Amulet blocked a self-inflicted drop: Def = %d", holder.Stages.Def)
	}
	_ = d
}
