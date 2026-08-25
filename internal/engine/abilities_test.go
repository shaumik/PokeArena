package engine

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// TestPinchAbilityBoostsMatchingType: Blaze multiplies the holder's Fire-move
// damage by 1.5 once it drops to 1/3 max HP, and leaves it untouched above
// the threshold and for off-type moves.
func TestPinchAbilityBoostsMatchingType(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[6])   // Charizard (Fire/Flying)
	def := buildPokemon(d, d.Species[143]) // Snorlax
	fire := d.Moves["flamethrower"]
	if fire.Power == 0 {
		fire = d.Moves["ember"]
	}

	atk.Ability = "blaze"

	// Healthy: no boost.
	atk.HP = atk.MaxHP
	healthy := ExpectedDamage(d, &atk, &def, fire, nil, nil, nil)

	// In pinch (≤ 1/3 HP): 1.5×.
	atk.HP = atk.MaxHP / 3
	pinched := ExpectedDamage(d, &atk, &def, fire, nil, nil, nil)

	if pinched*100 < healthy*145 || pinched*100 > healthy*155 {
		t.Errorf("Blaze in pinch: %d → %d, want ~1.5× (base*3/2 = %d)", healthy, pinched, healthy*3/2)
	}
}

// TestPinchAbilityIgnoresOffType: Blaze does nothing for a non-Fire move even
// when the holder is in the pinch HP range.
func TestPinchAbilityIgnoresOffType(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[6]) // Charizard
	def := buildPokemon(d, d.Species[143])
	normal := d.Moves["tackle"] // Normal physical

	noAbility := ExpectedDamage(d, &atk, &def, normal, nil, nil, nil)

	atk.Ability = "blaze"
	atk.HP = atk.MaxHP / 3
	withBlaze := ExpectedDamage(d, &atk, &def, normal, nil, nil, nil)

	if withBlaze != noAbility {
		t.Errorf("Blaze changed off-type damage: %d → %d, want unchanged", noAbility, withBlaze)
	}
}

// TestPinchBoostThreshold: the boost engages exactly at 1/3 max HP (HP*3 ≤
// MaxHP) and not at one point above it.
func TestPinchBoostThreshold(t *testing.T) {
	boost := pinchBoost("fire")
	m := domain.Move{Type: "fire"}
	p := &Pokemon{MaxHP: 99}

	p.HP = 33 // exactly 1/3
	if got := boost(p, m, nil, nil, 1); got != 1.5 {
		t.Errorf("at HP=33/99 (1/3): mult = %v, want 1.5", got)
	}
	p.HP = 34 // just above 1/3
	if got := boost(p, m, nil, nil, 1); got != 1 {
		t.Errorf("at HP=34/99 (>1/3): mult = %v, want 1", got)
	}
}

// TestShedSkinCuresAboutThirtyPercentOfTurns: Shed Skin gives its holder a 30%
// chance each turn-end of shrugging off a major status. Measured as a rate over
// many battles rather than pinned to a seed that rolls the way the test wants:
// the rule is the probability, and one seed can neither confirm 30% nor tell it
// from 25%.
func TestShedSkinCuresAboutThirtyPercentOfTurns(t *testing.T) {
	d := loadDex(t)
	shedTurn := func(seed uint64) bool {
		s, err := NewBattle(d, "shed", "P1", []int{143}, "P2", []int{143}, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		p := s.Active(0)
		p.Ability = "shed-skin"
		p.Status = StatusBurn
		p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Ability = AbilityNone
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

		log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		cured := s.Active(0).Status == StatusNone
		if cured != logHas(log, "shed its status with Shed Skin") {
			t.Fatalf("seed %d: the log and the status disagree about the cure", seed)
		}
		return cured
	}
	assertRate(t, "Shed Skin", 0.30, shedTurn)
}

// TestShedSkinNoStatusNoOp: with no status, Shed Skin does nothing even when
// the roll would fire.
func TestShedSkinNoStatusNoOp(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{9}, 1)
	s.Active(0).Ability = "shed-skin"
	var log []LogLine
	applyAbilityEndOfTurn(s, 0, NewRNG(2), &log)
	if len(log) != 0 {
		t.Errorf("Shed Skin logged with no status: %+v", log)
	}
}

// TestHydrationCuresInRain: Hydration cures status only while raining, and
// clears the sleep clock / toxic counter along with it.
func TestHydrationCuresInRain(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{9}, "P2", []int{6}, 1)
	p := s.Active(0)
	p.Ability = "hydration"
	p.Status = StatusSleep
	p.SleepTurns = 3

	// No rain — no cure.
	var log []LogLine
	applyAbilityEndOfTurn(s, 0, NewRNG(1), &log)
	if p.Status != StatusSleep {
		t.Errorf("Hydration cured outside rain: status = %v, want sleep", p.Status)
	}

	// Rain — cure, and the sleep clock resets.
	s.Weather = &WeatherState{Kind: WeatherRain, TurnsLeft: 5}
	applyAbilityEndOfTurn(s, 0, NewRNG(1), &log)
	if p.Status != StatusNone || p.SleepTurns != 0 {
		t.Errorf("Hydration in rain: status = %v, sleepTurns = %d, want none/0", p.Status, p.SleepTurns)
	}
}

// --- Harvest ---
//
// The rule, stated once for all four tests below: at the end of a turn, a
// Harvest holder that is carrying nothing and most recently ate a Berry gets
// that Berry back — every time in harsh sunlight, about half the time
// otherwise. It never fires while the holder is carrying something, and it
// only ever returns a Berry.
//
// Everything here drives a resolved turn rather than the end-of-turn hook, and
// the coin-flip is measured as a rate over many battles rather than pinned to
// a seed that happens to roll the way the test wants. Both choices are
// deliberate: a seed only means something to an implementation that reproduces
// this exact generator and draws from it in this exact order, which is not
// something a reimplementation should have to match before it can be told
// whether Harvest works.

// harvestTrial plays one turn: a Harvest holder at exactly half HP with a
// Sitrus Berry, which fires at that threshold, and a foe that idles. The berry
// is eaten early in the residual order and Harvest gets its chance at it
// before the turn is out. Reports whether the berry came back.
func harvestTrial(t *testing.T, d *domain.Dex, seed uint64, sun bool) bool {
	t.Helper()
	s, err := NewBattle(d, "harvest", "P1", []int{143}, "P2", []int{143}, seed)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	p := s.Active(0)
	p.Ability = "harvest"
	p.Item = ItemSitrusBerry
	p.HP = p.MaxHP / 2
	p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Ability = AbilityNone
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	if sun {
		s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 9}
	}

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !logHas(log, "ate its Sitrus Berry") {
		t.Fatalf("seed %d: the berry never fired, so the trial proves nothing", seed)
	}
	back := logHas(log, "harvested one Sitrus Berry")
	if back != (s.Active(0).Item == ItemSitrusBerry) {
		t.Errorf("seed %d: the log and the held item disagree about the regrow", seed)
	}
	return back
}

// TestHarvestAlwaysRegrowsInSun: harsh sunlight makes it certain. Measured, not
// asserted from one lucky seed — the claim is "every time", so every trial has
// to come back.
func TestHarvestAlwaysRegrowsInSun(t *testing.T) {
	d := loadDex(t)
	const trials = 200
	for seed := uint64(1); seed <= trials; seed++ {
		if !harvestTrial(t, d, seed, true) {
			t.Fatalf("seed %d: Harvest did not regrow the berry under sun", seed)
		}
	}
}

// TestHarvestRegrowsAboutHalfTheTimeOutsideSun: the rate is the rule, so the
// rate is what is checked. The band is wide enough that a fair coin cannot
// realistically fall outside it over this many trials, and narrow enough to
// catch an implementation that made the regrow certain, impossible, or keyed
// to something other than a coin flip.
func TestHarvestRegrowsAboutHalfTheTimeOutsideSun(t *testing.T) {
	d := loadDex(t)
	const trials = 400
	got := 0
	for seed := uint64(1); seed <= trials; seed++ {
		if harvestTrial(t, d, seed, false) {
			got++
		}
	}
	rate := float64(got) / float64(trials)
	if rate < 0.35 || rate > 0.65 {
		t.Errorf("Harvest regrew %d/%d (%.0f%%) outside sun, want roughly half",
			got, trials, 100*rate)
	}
}

// TestHarvestRefusesWhileHolding: the ability restocks an empty slot, so once
// the berry is back the holder keeps it and no second berry appears. Played as
// two turns rather than by handing the ability a full slot, because "it does
// not fire twice" is the property a roster actually depends on.
func TestHarvestRefusesWhileHolding(t *testing.T) {
	d, s := berryBattle(t, ItemSitrusBerry)
	p := s.Active(0)
	p.Ability = "harvest"
	p.HP = p.MaxHP / 2
	s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 9}

	if log := splashTurn(d, s); !logHas(log, "harvested one Sitrus Berry") {
		t.Fatalf("the berry did not come back on turn one: %v", log)
	}
	// The holder is above the berry's threshold now, so nothing is eaten and
	// the slot stays full: Harvest has nothing to do.
	s.Active(0).HP = s.Active(0).MaxHP
	log := splashTurn(d, s)
	if logHas(log, "harvested one") {
		t.Errorf("Harvest fired while the holder was already carrying the berry: %v", log)
	}
	if got := s.Active(0).Item; got != ItemSitrusBerry {
		t.Errorf("item = %q, want the berry still held", got)
	}
}

// TestHarvestOnlyRegrowsBerries: a spent White Herb is not a Berry and stays
// spent. Driven through the item that actually consumes itself — the foe drops
// a stat, the Herb undoes it and is gone — so the test says what the rule is
// about rather than writing a slug into the holder's memory by hand.
func TestHarvestOnlyRegrowsBerries(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "herb", "P1", []int{143}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	p := s.Active(0)
	p.Ability = "harvest"
	p.Item = ItemWhiteHerb
	p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	foe := s.Active(1)
	foe.Ability = AbilityNone
	foe.Moves = []MoveSlot{{MoveID: "growl", PP: 40, MaxPP: 40}}
	s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 9}

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !logHas(log, "used its White Herb") {
		t.Fatalf("the White Herb never fired, so the trial proves nothing: %v", log)
	}
	if logHas(log, "harvested one") {
		t.Errorf("Harvest regrew a non-Berry: %v", log)
	}
	if got := s.Active(0).Item; got != ItemNone {
		t.Errorf("item = %q, want the holder still bare", got)
	}
}

// TestHarvestRegrowsAcrossRealTurns: the unit tests drive the hook directly;
// this one runs a whole turn, because the failure the tournament actually hit
// was "nothing happens in a match". Sitrus fires at half HP earlier in the
// residual order, and Harvest's tick hands it back before the turn is out —
// same-turn regrowth, which is where canon puts the ability's residual too.
func TestHarvestRegrowsAcrossRealTurns(t *testing.T) {
	d, s := berryBattle(t, ItemSitrusBerry)
	p := s.Active(0)
	p.Ability = "harvest"
	p.HP = p.MaxHP / 2
	s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 9}

	log := splashTurn(d, s)
	if !logHas(log, "ate its Sitrus Berry") {
		t.Fatalf("Sitrus never fired: %v", log)
	}
	if !logHas(log, "harvested one Sitrus Berry") {
		t.Errorf("Harvest did not regrow the berry in a real turn: %v", log)
	}
	if got := s.Active(0).Item; got != ItemSitrusBerry {
		t.Errorf("item after Harvest = %q, want sitrus-berry", got)
	}

	// And it keeps working: the regrown berry is eaten again next time the
	// holder drops into range, then regrown again.
	s.Active(0).HP = s.Active(0).MaxHP / 2
	log = splashTurn(d, s)
	if !logHas(log, "harvested one Sitrus Berry") {
		t.Errorf("Harvest fired once and stopped: %v", log)
	}
}

// TestOwnTempoRefusesConfusion: an Own Tempo holder cannot be confused. The
// slug was registered with a comment saying the guard lived "elsewhere" — it
// did not, nothing in the package read it, and a Slowbro with Own Tempo was
// confused exactly as often as one without.
//
// Driven by the foe actually using Confuse Ray, with a control that confuses
// without the ability, rather than by calling the volatile applier: the rule
// is about what a move can do to a Pokémon, and that is the form of it another
// implementation has to satisfy. The refusal is silent — the engine announces
// nothing, so the foe learns nothing — which is why the log is checked too.
func TestOwnTempoRefusesConfusion(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	target, caster := s.Active(0), s.Active(1)
	target.Ability = "own-tempo"
	caster.Ability = AbilityNone
	target.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	caster.Moves = []MoveSlot{{MoveID: "confuse-ray", PP: 10, MaxPP: 10}}

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !logHas(log, "used Confuse Ray") {
		t.Fatalf("Confuse Ray never resolved: %v", log)
	}
	if s.Active(0).Volatiles.Confusion != nil {
		t.Errorf("Own Tempo holder was confused by Confuse Ray")
	}
	if logHas(log, "became confused") {
		t.Errorf("the log says the holder was confused: %v", log)
	}

	// Control: the same turn without Own Tempo does confuse.
	s2, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	s2.Active(0).Ability = AbilityNone
	s2.Active(1).Ability = AbilityNone
	s2.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s2.Active(1).Moves = []MoveSlot{{MoveID: "confuse-ray", PP: 10, MaxPP: 10}}
	log = ResolveTurn(d, s2, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s2.Active(0).Volatiles.Confusion == nil {
		t.Errorf("Confuse Ray no longer confuses without Own Tempo: %v", log)
	}
}

// TestSandForceBoostsInSand: Sand Force adds 1.3× to a Ground move only while
// a sandstorm is active, and never to off-type moves.
func TestSandForceBoostsInSand(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[143]) // Snorlax (stat stick)
	def := buildPokemon(d, d.Species[143])
	atk.Ability = "sand-force"
	ground := d.Moves["earthquake"]
	normal := d.Moves["tackle"]
	sand := &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}

	clear := ExpectedDamage(d, &atk, &def, ground, nil, nil, nil)
	boosted := ExpectedDamage(d, &atk, &def, ground, sand, nil, nil)
	if boosted*100 < clear*125 || boosted*100 > clear*135 {
		t.Errorf("Sand Force Ground move in sand: %d → %d, want ~1.3× (base*13/10 = %d)", clear, boosted, clear*13/10)
	}

	// Off-type move: untouched even in sand.
	normalClear := ExpectedDamage(d, &atk, &def, normal, nil, nil, nil)
	normalSand := ExpectedDamage(d, &atk, &def, normal, sand, nil, nil)
	if normalSand != normalClear {
		t.Errorf("Sand Force changed off-type damage in sand: %d → %d, want unchanged", normalClear, normalSand)
	}
}

// TestDownloadPicksWeakerDefense: Download raises Attack against a foe whose
// Defense is the lower defensive stat, and Sp. Atk otherwise.
func TestDownloadPicksWeakerDefense(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{9}, 1)
	p := s.Active(0)
	p.Ability = "download"
	foe := s.Active(1)

	// Foe weaker on Defense → +1 Attack.
	foe.Stats.Def, foe.Stats.SpD = 50, 100
	p.Stages.Atk, p.Stages.SpA = 0, 0
	var log []LogLine
	applyOnSwitchIn(s, 0, &log)
	if p.Stages.Atk != 1 || p.Stages.SpA != 0 {
		t.Errorf("Download vs low Def: Atk=%d SpA=%d, want +1/0", p.Stages.Atk, p.Stages.SpA)
	}

	// Foe weaker on Sp. Def → +1 Sp. Atk.
	foe.Stats.Def, foe.Stats.SpD = 100, 50
	p.Stages.Atk, p.Stages.SpA = 0, 0
	applyOnSwitchIn(s, 0, &log)
	if p.Stages.SpA != 1 || p.Stages.Atk != 0 {
		t.Errorf("Download vs low SpD: Atk=%d SpA=%d, want 0/+1", p.Stages.Atk, p.Stages.SpA)
	}
}

// TestLeafGuardBlocksStatusInSun: Leaf Guard refuses a status while the sun is
// up and permits it once the sun is gone.
func TestLeafGuardBlocksStatusInSun(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{9}, "P2", []int{6}, 1)
	p := s.Active(0)
	p.Ability = "leaf-guard"

	// Sun up — burn refused.
	s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 5}
	var log []LogLine
	if inflictStatus(p, 0, StatusBurn, s, NewRNG(1), &log) {
		t.Errorf("Leaf Guard let a burn through under sun")
	}
	if p.Status != StatusNone {
		t.Errorf("status applied under Leaf Guard: %v", p.Status)
	}

	// No weather — burn lands.
	s.Weather = nil
	if !inflictStatus(p, 0, StatusBurn, s, NewRNG(1), &log) {
		t.Errorf("Leaf Guard blocked a burn with no sun")
	}
	if p.Status != StatusBurn {
		t.Errorf("burn should have landed without sun: %v", p.Status)
	}
}

// TestLiquidOozeBackfiresDrain: draining a Liquid Ooze holder damages the
// drainer for the would-be-healed amount instead of restoring its HP.
func TestLiquidOozeBackfiresDrain(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{143}, 1) // Charizard vs Snorlax
	atk := s.Active(0)
	foe := s.Active(1)
	foe.Ability = "liquid-ooze"
	atk.Moves = []MoveSlot{{MoveID: "drain-punch", PP: 10, MaxPP: 10}}
	foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	atk.HP = atk.MaxHP / 2 // leave headroom so a normal drain would visibly heal
	before := atk.HP

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Active(0).HP >= before {
		t.Errorf("Liquid Ooze: drainer HP %d → %d, want a net loss (drain should hurt)", before, s.Active(0).HP)
	}
}

// TestUnawareIgnoresDefenderBoost: an Unaware attacker ignores the defender's
// Defense boost, so a +2 Def wall takes the same damage as an unboosted one.
func TestUnawareIgnoresDefenderBoost(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[143])
	def := buildPokemon(d, d.Species[143])
	move := d.Moves["tackle"]

	def.Stages.Def = 2
	boostedVsNormal := ExpectedDamage(d, &atk, &def, move, nil, nil, nil)

	atk.Ability = "unaware"
	boostedVsUnaware := ExpectedDamage(d, &atk, &def, move, nil, nil, nil)

	def.Stages.Def = 0
	atk.Ability = ""
	unboosted := ExpectedDamage(d, &atk, &def, move, nil, nil, nil)

	if boostedVsUnaware != unboosted {
		t.Errorf("Unaware attacker vs +2 Def: %d, want unboosted %d", boostedVsUnaware, unboosted)
	}
	if boostedVsNormal >= unboosted {
		t.Errorf("sanity: +2 Def should reduce damage for a normal attacker (%d vs %d)", boostedVsNormal, unboosted)
	}
}

// TestUnawareIgnoresAttackerBoost: an Unaware defender ignores the attacker's
// Attack boost, taking the same damage from a +2 Atk foe as from an unboosted
// one.
func TestUnawareIgnoresAttackerBoost(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[143])
	def := buildPokemon(d, d.Species[143])
	move := d.Moves["tackle"]

	atk.Stages.Atk = 2
	def.Ability = "unaware"
	boostedVsUnaware := ExpectedDamage(d, &atk, &def, move, nil, nil, nil)

	atk.Stages.Atk = 0
	def.Ability = ""
	unboosted := ExpectedDamage(d, &atk, &def, move, nil, nil, nil)

	if boostedVsUnaware != unboosted {
		t.Errorf("Unaware defender vs +2 Atk: %d, want unboosted %d", boostedVsUnaware, unboosted)
	}
}

// TestRockHeadNegatesRecoil: a Rock Head attacker takes no recoil from a
// recoil move, while a normal attacker does.
func TestRockHeadNegatesRecoil(t *testing.T) {
	d := loadDex(t)
	runRecoil := func(ability AbilityKind) (start, end int) {
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
		atk := s.Active(0)
		atk.Ability = ability
		atk.Moves = []MoveSlot{{MoveID: "double-edge", PP: 15, MaxPP: 15}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		start = atk.HP
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		return start, s.Active(0).HP
	}

	start, end := runRecoil("rock-head")
	if end != start {
		t.Errorf("Rock Head should negate recoil: HP %d → %d", start, end)
	}
	start, end = runRecoil("")
	if end >= start {
		t.Errorf("sanity: a normal attacker should take recoil from double-edge: HP %d → %d", start, end)
	}
}

// TestJustifiedRaisesAttackOnDarkHit: taking a Dark-type move raises the
// holder's Attack by one stage; a non-Dark hit leaves it alone.
func TestJustifiedRaisesAttackOnDarkHit(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1) // Snorlax mirror
	p := s.Active(0)
	p.Ability = "justified"
	s.Active(1).Moves = []MoveSlot{{MoveID: "knock-off", PP: 20, MaxPP: 20}} // Dark, physical
	p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if p.Stages.Atk != 1 {
		t.Errorf("Justified after a Dark hit: Atk stage = %d, want +1", p.Stages.Atk)
	}

	// A non-Dark hit does nothing.
	p.Stages.Atk = 0
	var log []LogLine
	applyOnHit(s, 0, d.Moves["tackle"], false, NewRNG(1), &log)
	if p.Stages.Atk != 0 {
		t.Errorf("Justified fired on a Normal move: Atk stage = %d, want 0", p.Stages.Atk)
	}
}

// TestWeakArmorShiftsOnPhysicalHit: a physical hit drops Defense by one and
// raises Speed by two; a special hit does neither.
func TestWeakArmorShiftsOnPhysicalHit(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	p := s.Active(0)
	p.Ability = "weak-armor"
	s.Active(1).Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}} // Normal, physical
	p.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if p.Stages.Def != -1 || p.Stages.Spe != 2 {
		t.Errorf("Weak Armor after a physical hit: Def=%d Spe=%d, want -1/+2", p.Stages.Def, p.Stages.Spe)
	}

	// A special hit leaves both stages untouched.
	p.Stages.Def, p.Stages.Spe = 0, 0
	var log []LogLine
	applyOnHit(s, 0, d.Moves["psychic"], false, NewRNG(1), &log) // special
	if p.Stages.Def != 0 || p.Stages.Spe != 0 {
		t.Errorf("Weak Armor fired on a special move: Def=%d Spe=%d, want 0/0", p.Stages.Def, p.Stages.Spe)
	}
}

// TestReactiveDefenseIgnoresSubstituteHit: Justified and Weak Armor don't
// trigger when a substitute absorbed the blow (hitSub == true).
func TestReactiveDefenseIgnoresSubstituteHit(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	p := s.Active(0)

	p.Ability = "justified"
	var log []LogLine
	applyOnHit(s, 0, d.Moves["knock-off"], true, NewRNG(1), &log) // sub ate it
	if p.Stages.Atk != 0 {
		t.Errorf("Justified fired through a substitute: Atk stage = %d, want 0", p.Stages.Atk)
	}
	applyOnHit(s, 0, d.Moves["knock-off"], false, NewRNG(1), &log) // direct hit
	if p.Stages.Atk != 1 {
		t.Errorf("Justified failed on a direct hit: Atk stage = %d, want +1", p.Stages.Atk)
	}

	p.Ability = "weak-armor"
	p.Stages.Def, p.Stages.Spe = 0, 0
	applyOnHit(s, 0, d.Moves["tackle"], true, NewRNG(1), &log) // sub ate it
	if p.Stages.Def != 0 || p.Stages.Spe != 0 {
		t.Errorf("Weak Armor fired through a substitute: Def=%d Spe=%d, want 0/0", p.Stages.Def, p.Stages.Spe)
	}
}

// TestCuteCharmInfatuatesOnContact: a 30% chance to infatuate whoever makes
// contact with the holder, and only if the two could be attracted at all. The
// chance is measured; the two refusals — a non-contact move, a same-gender
// attacker — are required to hold on every single attempt, which is a stronger
// claim than the one seed each used to get.
func TestCuteCharmInfatuatesOnContact(t *testing.T) {
	d := loadDex(t)
	// The attacker strikes the holder and may fall in love with it.
	contact := func(move, atkGender string) func(uint64) bool {
		return func(seed uint64) bool {
			s, err := NewBattle(d, "charm", "P1", []int{143}, "P2", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			holder := s.Active(0)
			holder.Ability = "cute-charm"
			holder.Gender = "female"
			holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			atk := s.Active(1)
			atk.Ability = AbilityNone
			atk.Gender = atkGender
			atk.Moves = []MoveSlot{{MoveID: move, PP: 30, MaxPP: 30}}

			ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
			return s.Active(1).Volatiles.Attract
		}
	}

	assertRate(t, "Cute Charm on contact", 0.30, contact("tackle", "male"))
	assertNever(t, "Cute Charm off a non-contact move", contact("water-gun", "male"))
	assertNever(t, "Cute Charm between same genders", contact("tackle", "female"))
}

// TestPoisonTouchPoisonsOnContact: the holder's contact moves poison what they
// hit, 30% of the time. The rate is measured; the non-contact refusal has to
// hold every time.
func TestPoisonTouchPoisonsOnContact(t *testing.T) {
	d := loadDex(t)
	strike := func(move string) func(uint64) bool {
		return func(seed uint64) bool {
			s, err := NewBattle(d, "touch", "P1", []int{143}, "P2", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			atk := s.Active(0)
			atk.Ability = "poison-touch"
			atk.Moves = []MoveSlot{{MoveID: move, PP: 30, MaxPP: 30}}
			def := s.Active(1)
			// Snorlax defaults to Immunity, which would refuse the poison and
			// measure the wrong thing.
			def.Ability = AbilityNone
			def.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

			ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
			return s.Active(1).Status == StatusPoison
		}
	}

	assertRate(t, "Poison Touch on contact", 0.30, strike("tackle"))
	assertNever(t, "Poison Touch off a non-contact move", strike("water-gun"))
}

// TestPoisonTouchBouncesOffSynchronize: Synchronize reflects a status back at
// whoever inflicted it, and Poison Touch is an infliction like any other — so
// a Poison Touch holder that poisons a Synchronize target poisons itself.
//
// Stated as an implication over many attempts rather than off a seed that
// happens to make the 30% roll fire: on every attempt where the target is
// poisoned, the attacker must be poisoned too. That also covers the direction
// a single lucky seed cannot — the bounce never firing on its own.
func TestPoisonTouchBouncesOffSynchronize(t *testing.T) {
	d := loadDex(t)
	var poisoned, bounced int
	for seed := uint64(1); seed <= trials; seed++ {
		s, err := NewBattle(d, "sync", "P1", []int{143}, "P2", []int{143}, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		atk := s.Active(0)
		atk.Ability = "poison-touch"
		atk.Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}}
		def := s.Active(1)
		def.Ability = "synchronize" // and not Immunity, which would refuse it
		def.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

		switch {
		case s.Active(1).Status == StatusPoison:
			poisoned++
			if s.Active(0).Status != StatusPoison {
				t.Fatalf("seed %d: Synchronize did not bounce the poison back onto the "+
					"Poison Touch holder (attacker status %q)", seed, s.Active(0).Status)
			}
			bounced++
		case s.Active(0).Status != StatusNone:
			t.Fatalf("seed %d: the attacker was poisoned without poisoning anything", seed)
		}
	}
	if poisoned == 0 {
		t.Fatalf("Poison Touch never poisoned across %d attempts, so the bounce was "+
			"never tested", trials)
	}
	t.Logf("Synchronize bounced %d of %d poisonings", bounced, poisoned)
}

// TestSynchronizeReflectsStatus: a foe-inflicted burn/poison/toxic/paralysis on
// a Synchronize holder bounces back onto the source; sleep and self-inflicted
// status do not, and a non-holder never reflects.
func TestSynchronizeReflectsStatus(t *testing.T) {
	d := loadDex(t)
	newState := func() *BattleState {
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
		s.Active(0).Ability = "synchronize"
		s.Active(1).Ability = "" // Snorlax defaults to Immunity, which would refuse the bounced poison
		return s
	}
	var log []LogLine

	// Foe-caused paralysis bounces back onto the source.
	s := newState()
	if !inflictStatusFrom(s.Active(0), 0, 1, StatusParalysis, s, NewRNG(1), &log) {
		t.Fatal("Synchronize holder failed to receive paralysis")
	}
	if s.Active(1).Status != StatusParalysis {
		t.Errorf("Synchronize did not reflect paralysis: source status=%q", s.Active(1).Status)
	}

	// A self-inflicted status (source side == target side) never bounces.
	s = newState()
	inflictStatusFrom(s.Active(0), 0, 0, StatusPoison, s, NewRNG(1), &log)
	if s.Active(1).Status != StatusNone {
		t.Errorf("self-inflicted status must not reflect: source status=%q", s.Active(1).Status)
	}

	// Sleep is outside the reflect set.
	s = newState()
	inflictStatusFrom(s.Active(0), 0, 1, StatusSleep, s, NewRNG(1), &log)
	if s.Active(1).Status != StatusNone {
		t.Errorf("sleep must not reflect: source status=%q", s.Active(1).Status)
	}

	// Without the ability, nothing bounces.
	s = newState()
	s.Active(0).Ability = ""
	inflictStatusFrom(s.Active(0), 0, 1, StatusParalysis, s, NewRNG(1), &log)
	if s.Active(1).Status != StatusNone {
		t.Errorf("non-holder must not reflect: source status=%q", s.Active(1).Status)
	}
}

// TestPressureDrainsExtraPP: a foe move aimed at a Pressure holder costs two
// PP, not one; a self-targeted move still costs only one.
func TestPressureDrainsExtraPP(t *testing.T) {
	d := loadDex(t)
	run := func(foeAbility AbilityKind, moverMove string) int {
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
		atk := s.Active(0)
		atk.Moves = []MoveSlot{{MoveID: moverMove, PP: 20, MaxPP: 20}}
		foe := s.Active(1)
		foe.Ability = foeAbility
		foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		return atk.Moves[0].PP
	}

	// Foe-targeting move vs Pressure: 20 → 18 (one normal + one Pressure PP).
	if pp := run("pressure", "tackle"); pp != 18 {
		t.Errorf("Pressure vs foe-targeting move: PP = %d, want 18", pp)
	}
	// Same move without Pressure: only the normal PP is paid.
	if pp := run("", "tackle"); pp != 19 {
		t.Errorf("no Pressure: PP = %d, want 19", pp)
	}
	// Self-targeted move is never in Pressure's range: only the normal PP.
	if pp := run("pressure", "swords-dance"); pp != 19 {
		t.Errorf("Pressure vs self-targeted move: PP = %d, want 19", pp)
	}
}

// TestMoxieBoostsOnKO: scoring a KO with a damaging move raises Moxie's
// Attack; a hit that leaves the foe standing does not.
//
// The foe's team is two Pokémon deep, and that is load-bearing rather than
// incidental. With a one-Pokémon foe the KO *ends the battle*, and canon does
// not pay out for the last KO of a sweep — faintMessages returns on checkWin
// before ever reaching the AfterFaint event Moxie hangs off. This fixture used
// to have a single foe, so it was quietly asserting the boost in exactly the
// case that should not produce one. See TestMoxieSkipsTheKOThatEndsTheBattle.
func TestMoxieBoostsOnKO(t *testing.T) {
	d := loadDex(t)
	run := func(foeHP int) *Pokemon {
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143, 65}, 1)
		atk := s.Active(0)
		atk.Ability = "moxie"
		atk.Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}}
		foe := s.Active(1)
		foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		if foeHP > 0 {
			foe.HP = foeHP // 0 means "leave it at full"
		}
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		return atk
	}

	// foe at 1 HP → KO → +1 Attack.
	if atk := run(1); atk.Stages.Atk != 1 {
		t.Errorf("Moxie after a KO: Atk stage = %d, want +1", atk.Stages.Atk)
	}
	// healthy foe survives the tackle → no boost. Full HP, not an arbitrary
	// large number: 999 on a 235-HP Snorlax is a state the engine cannot
	// produce, and the automatic invariant check in TestMain rejects it.
	if atk := run(0); atk.Stages.Atk != 0 {
		t.Errorf("Moxie without a KO: Atk stage = %d, want 0", atk.Stages.Atk)
	}
}

// TestMoxieSkipsFaintedAttacker: an attacker that fainted in the same exchange
// (e.g. Destiny Bond, recoil) collects no Moxie boost.
func TestMoxieSkipsFaintedAttacker(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	atk := s.Active(0)
	atk.Ability = "moxie"
	atk.HP = 0
	atk.Fainted = true

	var log []LogLine
	applyOnKO(s, 0, &log)
	if atk.Stages.Atk != 0 || len(log) != 0 {
		t.Errorf("Moxie fired for a fainted attacker: Atk=%d log=%+v", atk.Stages.Atk, log)
	}
}

// TestAftermathChipsContactKiller: fainting to a contact move costs the
// attacker 1/4 of its max HP.
func TestAftermathChipsContactKiller(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	atk := s.Active(0)
	atk.Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}} // contact
	foe := s.Active(1)
	foe.Ability = "aftermath"
	foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	foe.HP = 1
	before := atk.HP

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !s.Active(1).Fainted {
		t.Fatalf("foe should have fainted")
	}
	want := atk.MaxHP / 4
	got := before - s.Active(0).HP
	if got < want-1 || got > want+1 {
		t.Errorf("Aftermath chip: attacker lost %d HP, want ~%d (1/4 max)", got, want)
	}
}

// TestAftermathRespectsContactAndMagicGuard: Aftermath fires only for a
// contact KO and is blocked by the attacker's Magic Guard.
func TestAftermathRespectsContactAndMagicGuard(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	s.Active(1).Ability = "aftermath" // the (notionally fainted) defender
	atk := s.Active(0)
	var log []LogLine

	// Non-contact finisher: no chip.
	before := atk.HP
	applyOnFaint(s, 1, 0, d.Moves["water-gun"], &log)
	if atk.HP != before {
		t.Errorf("Aftermath fired on a non-contact KO: HP %d → %d", before, atk.HP)
	}

	// Contact, but the attacker has Magic Guard: no chip.
	atk.Ability = "magic-guard"
	applyOnFaint(s, 1, 0, d.Moves["tackle"], &log)
	if atk.HP != before {
		t.Errorf("Magic Guard failed to block Aftermath: HP %d → %d", before, atk.HP)
	}

	// Contact, ordinary attacker: chip lands.
	atk.Ability = ""
	applyOnFaint(s, 1, 0, d.Moves["tackle"], &log)
	if atk.HP >= before {
		t.Errorf("Aftermath failed to chip a contact attacker: HP %d → %d", before, atk.HP)
	}
}

// TestAngerPointMaxesAttack: a crit maxes Attack from any stage, no-ops at +6,
// and skips a defender that the hit knocked out.
func TestAngerPointMaxesAttack(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	p := s.Active(0)
	p.Ability = "anger-point"
	p.Stages.Atk = -2

	var log []LogLine
	applyOnCrit(s, 0, &log)
	if p.Stages.Atk != 6 {
		t.Errorf("Anger Point on a crit: Atk stage = %d, want 6", p.Stages.Atk)
	}

	// Already maxed: nothing changes and nothing is logged.
	n := len(log)
	applyOnCrit(s, 0, &log)
	if p.Stages.Atk != 6 || len(log) != n {
		t.Errorf("Anger Point re-fired at +6: Atk=%d, logΔ=%d", p.Stages.Atk, len(log)-n)
	}

	// A defender knocked out by the crit collects nothing.
	dead := s.Active(1)
	dead.Ability = "anger-point"
	dead.HP = 0
	applyOnCrit(s, 1, &log)
	if dead.Stages.Atk != 0 {
		t.Errorf("Anger Point fired on a 0-HP defender: Atk stage = %d, want 0", dead.Stages.Atk)
	}
}

// TestAngerPointFiresOnRealCrit: the OnCrit wiring triggers off a genuine
// critical hit landed in a resolved turn (Frost Breath always crits).
func TestAngerPointFiresOnRealCrit(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	atk := s.Active(0)
	atk.Moves = []MoveSlot{{MoveID: "frost-breath", PP: 10, MaxPP: 10}} // always crits
	def := s.Active(1)
	def.Ability = "anger-point"
	def.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if def.Fainted {
		t.Fatalf("defender should survive a 60 BP crit")
	}
	if def.Stages.Atk != 6 {
		t.Errorf("Anger Point off a real crit: Atk stage = %d, want 6", def.Stages.Atk)
	}
}

// TestSkillLinkMaxesMultihit: a Skill Link holder always lands the top of a
// multi-strike move's range, across every seed; without it the [2,5] roll
// varies. Fixed-count moves are unaffected (already max).
func TestSkillLinkMaxesMultihit(t *testing.T) {
	rangeM := domain.Move{MinHits: 2, MaxHits: 5}
	linker := &Pokemon{Ability: "skill-link"}
	for seed := uint64(1); seed <= 50; seed++ {
		if n := multihitCount(rangeM, linker, NewRNG(seed)); n != 5 {
			t.Errorf("Skill Link [2,5] seed %d returned %d, want 5", seed, n)
		}
	}

	// A non-Skill-Link attacker still varies (not always 5).
	plain := &Pokemon{Ability: ""}
	allFive := true
	for seed := uint64(1); seed <= 50; seed++ {
		if multihitCount(rangeM, plain, NewRNG(seed)) != 5 {
			allFive = false
			break
		}
	}
	if allFive {
		t.Errorf("sanity: a plain attacker should not roll 5 hits on every seed")
	}
}

// TestSereneGraceDoublesSecondaryChance: the multiplier is 2 for Serene Grace
// (clamped at 100% per-secondary) and 1 for everything else.
func TestSereneGraceDoublesSecondaryChance(t *testing.T) {
	if got := abilitySecondaryChanceMult(&Pokemon{Ability: "serene-grace"}); got != 2 {
		t.Errorf("Serene Grace multiplier = %v, want 2", got)
	}
	if got := abilitySecondaryChanceMult(&Pokemon{Ability: ""}); got != 1 {
		t.Errorf("default secondary multiplier = %v, want 1", got)
	}
	// A 60% secondary doubled clamps to 100, not 120.
	chance := int(float64(60) * abilitySecondaryChanceMult(&Pokemon{Ability: "serene-grace"}))
	if chance > 100 {
		chance = 100
	}
	if chance != 100 {
		t.Errorf("doubled 60%% secondary = %d, want clamped 100", chance)
	}
}

// TestObliviousBlocksInfatuationAndTaunt: Oblivious refuses both the Attract
// and Taunt volatiles; a Pokémon without it takes them normally.
func TestObliviousBlocksInfatuationAndTaunt(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	p := s.Active(0)
	p.Ability = "oblivious"

	var log []LogLine
	s.Active(1).Gender = domain.GenderFemale
	p.Gender = domain.GenderMale
	applyAttractVolatile(p, 0, domain.Move{}, s, NewRNG(1), &log)
	if p.Volatiles.Attract {
		t.Errorf("Oblivious was infatuated")
	}
	applyTauntVolatile(p, 0, domain.Move{}, s, NewRNG(1), &log)
	if p.Volatiles.Taunt != nil {
		t.Errorf("Oblivious was taunted")
	}

	// Without Oblivious both land.
	p.Ability = ""
	applyAttractVolatile(p, 0, domain.Move{}, s, NewRNG(1), &log)
	applyTauntVolatile(p, 0, domain.Move{}, s, NewRNG(1), &log)
	if !p.Volatiles.Attract || p.Volatiles.Taunt == nil {
		t.Errorf("non-Oblivious dodged infatuation/taunt: attract=%v taunt=%v", p.Volatiles.Attract, p.Volatiles.Taunt)
	}
}

// TestStenchFlinchesOnHit: every damaging move the holder lands carries a 10%
// flinch. Landed with a priority move so the holder always strikes first and
// the flinch has something to interrupt; the rate is measured, and Inner Focus
// has to refuse it every single time.
func TestStenchFlinchesOnHit(t *testing.T) {
	d := loadDex(t)
	hit := func(defAbility AbilityKind) func(uint64) bool {
		return func(seed uint64) bool {
			s, err := NewBattle(d, "stench", "P1", []int{143}, "P2", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			atk := s.Active(0)
			atk.Ability = "stench"
			atk.Moves = []MoveSlot{{MoveID: "quick-attack", PP: 30, MaxPP: 30}}
			def := s.Active(1)
			def.Ability = defAbility
			def.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

			log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
			return logHas(log, "flinched and couldn't move")
		}
	}

	assertRate(t, "Stench", 0.10, hit(AbilityNone))
	assertNever(t, "Stench against Inner Focus", hit("inner-focus"))
}

// TestEarlyBirdHalvesSleep: Early Bird ticks the sleep counter down twice per
// turn, so a 4-turn sleep drops to 2 after one turn instead of 3.
func TestEarlyBirdHalvesSleep(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	p := s.Active(0)
	p.Status = StatusSleep
	p.SleepTurns = 4
	p.Ability = "early-bird"

	var log []LogLine
	canAct(p, 0, d.Moves["splash"], NewRNG(1), &log)
	if p.SleepTurns != 2 {
		t.Errorf("Early Bird sleep tick: SleepTurns=%d, want 2", p.SleepTurns)
	}

	// A normal sleeper only loses one turn.
	p.SleepTurns = 4
	p.Ability = ""
	canAct(p, 0, d.Moves["splash"], NewRNG(1), &log)
	if p.SleepTurns != 3 {
		t.Errorf("normal sleep tick: SleepTurns=%d, want 3", p.SleepTurns)
	}
}

// TestNoGuardAlwaysHits: No Guard on either the attacker or the defender makes
// an otherwise-missing move land; without it the 1%-accuracy move misses.
func TestNoGuardAlwaysHits(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	m := domain.Move{ID: "longshot", Name: "Longshot", Accuracy: 1}

	var log []LogLine
	// Baseline: seed 1 rolls 65 >= 1, so the move misses.
	if firstOf2(resolveAccuracy(s, 0, m, NewRNG(1), &log)) {
		t.Fatalf("baseline: 1%%-accuracy move should have missed")
	}

	// No Guard on the attacker: always hits.
	s.Active(0).Ability = "no-guard"
	if !firstOf2(resolveAccuracy(s, 0, m, NewRNG(1), &log)) {
		t.Errorf("No Guard attacker: move missed")
	}

	// No Guard on the defender: moves aimed at it also always hit.
	s.Active(0).Ability = ""
	s.Active(1).Ability = "no-guard"
	if !firstOf2(resolveAccuracy(s, 0, m, NewRNG(1), &log)) {
		t.Errorf("No Guard defender: move missed")
	}
}

// TestEvasionAbilitiesLowerAccuracy: Sand Veil, Snow Cloak and Tangled Feet
// each cut an incoming move's accuracy by a fifth while their condition holds,
// and do nothing at all outside it.
//
// Accuracy is a probability, so it is measured: a hundred-accuracy move lands
// every time against a holder outside its condition, and at the ability's own
// rate inside it. The old form picked a seed that missed and one that hit,
// which proved a hit and a miss were both reachable and nothing about the size
// of the effect — writing this the measured way immediately caught the author
// assuming Tangled Feet was another 20% shave when it halves accuracy.
func TestEvasionAbilitiesLowerAccuracy(t *testing.T) {
	d := loadDex(t)
	if tackle := d.Moves["tackle"]; tackle.Accuracy != 100 {
		t.Fatalf("this test needs a never-missing move; tackle is %d", tackle.Accuracy)
	}

	// One turn of Tackle into the holder. Reports whether it landed.
	swing := func(defAbility AbilityKind, arrange func(*BattleState)) func(uint64) bool {
		return func(seed uint64) bool {
			s, err := NewBattle(d, "evade", "P1", []int{143}, "P2", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability = AbilityNone
			s.Active(0).Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}}
			def := s.Active(1)
			def.Ability = defAbility
			def.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			if arrange != nil {
				arrange(s)
			}
			log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
			return !logHas(log, "attack missed")
		}
	}
	inWeather := func(k WeatherKind) func(*BattleState) {
		return func(s *BattleState) { s.Weather = &WeatherState{Kind: k, TurnsLeft: 9} }
	}
	confused := func(s *BattleState) {
		s.Active(1).Volatiles.Confusion = &ConfusionState{Turns: 3}
	}

	// The multiplier is part of the rule, so each case names its own: the two
	// weather cloaks shave a fifth off, while Tangled Feet doubles evasion
	// outright and halves what lands.
	cases := []struct {
		name     string
		ability  AbilityKind
		active   func(*BattleState)
		landRate float64
	}{
		{"Sand Veil", "sand-veil", inWeather(WeatherSandstorm), 0.80},
		{"Snow Cloak", "snow-cloak", inWeather(WeatherSnow), 0.80},
		{"Tangled Feet", "tangled-feet", confused, 0.50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertRate(t, c.name+" (condition up)", c.landRate, swing(c.ability, c.active))
			assertAlways(t, c.name+" (condition down)", swing(c.ability, nil))
		})
	}
}

// TestSandVeilImmuneToSandstorm: a Sand Veil holder takes no sandstorm chip
// even though its typing (Normal) would otherwise be buffeted.
func TestSandVeilImmuneToSandstorm(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[143]) // Snorlax, Normal
	sand := &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}
	if got := weatherResidual(sand, &p); got == 0 {
		t.Fatalf("baseline Snorlax should take sand chip, got 0")
	}
	p.Ability = "sand-veil"
	if got := weatherResidual(sand, &p); got != 0 {
		t.Errorf("Sand Veil sand chip = %d, want 0", got)
	}
}

// TestWonderSkinHalvesStatusAccuracy: Wonder Skin drops a status move's
// accuracy by half (Thunder Wave 90 → 45, so seed 1's roll of 65 now misses)
// while leaving damaging moves alone.
func TestWonderSkinHalvesStatusAccuracy(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	tw := d.Moves["thunder-wave"] // status, 90 accuracy
	var log []LogLine

	// Baseline: 90 accuracy, roll 65 < 90 → lands.
	if !firstOf2(resolveAccuracy(s, 0, tw, NewRNG(1), &log)) {
		t.Fatalf("baseline Thunder Wave should have hit")
	}
	// Wonder Skin halves it to 45, roll 65 >= 45 → misses.
	s.Active(1).Ability = "wonder-skin"
	if firstOf2(resolveAccuracy(s, 0, tw, NewRNG(1), &log)) {
		t.Errorf("Wonder Skin: status move should have missed")
	}
	// A damaging move is unaffected.
	if !firstOf2(resolveAccuracy(s, 0, d.Moves["tackle"], NewRNG(1), &log)) {
		t.Errorf("Wonder Skin should not touch a damaging move")
	}
}

// TestOvercoatImmuneToSandstorm: Overcoat blocks sandstorm chip damage.
func TestOvercoatImmuneToSandstorm(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[143]) // Snorlax, Normal
	sand := &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}
	p.Ability = "overcoat"
	if got := weatherResidual(sand, &p); got != 0 {
		t.Errorf("Overcoat sand chip = %d, want 0", got)
	}
}

// TestDampBlocksExplosion: an Explosion user fizzles (keeps its HP, deals no
// damage) when the foe has Damp, and blows up normally otherwise.
func TestDampBlocksExplosion(t *testing.T) {
	d := loadDex(t)
	run := func(foeAbility AbilityKind) (atkHP, foeHP, foeMax int) {
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
		atk := s.Active(0)
		atk.Moves = []MoveSlot{{MoveID: "explosion", PP: 5, MaxPP: 5}}
		foe := s.Active(1)
		foe.Ability = foeAbility
		var log []LogLine
		executeMove(d, s, 0, Action{Kind: ActionMove, Index: 0}, Action{}, false, false, NewRNG(1), &log)
		return atk.HP, foe.HP, foe.MaxHP
	}

	// Damp on the foe: attacker survives and the foe takes no damage.
	atkHP, foeHP, foeMax := run("damp")
	if atkHP == 0 {
		t.Errorf("Damp: attacker blew itself up anyway")
	}
	if foeHP != foeMax {
		t.Errorf("Damp: foe took damage (%d/%d), want full HP", foeHP, foeMax)
	}

	// No Damp: the user faints from Self-Destruct.
	atkHP2, _, _ := run("")
	if atkHP2 != 0 {
		t.Errorf("without Damp: Explosion should drop the user to 0 HP, got %d", atkHP2)
	}
}

// TestTraceCopiesFoeAbility: Trace copies the foe's ability on entry and fires
// its switch-in effect (tracing Intimidate cuts the foe's Attack). It stays
// inert against an untraceable ability.
func TestTraceCopiesFoeAbility(t *testing.T) {
	d := loadDex(t)

	// Trace a passive ability: the holder just adopts it.
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	p := s.Active(0)
	p.Ability = "trace"
	s.Active(1).Ability = "levitate"
	var log []LogLine
	applyOnSwitchIn(s, 0, &log)
	if p.Ability != "levitate" {
		t.Errorf("Trace: copied ability = %q, want levitate", p.Ability)
	}

	// Trace Intimidate: the copied entry effect drops the foe's Attack.
	s, _ = NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	p = s.Active(0)
	p.Ability = "trace"
	s.Active(1).Ability = "intimidate"
	applyOnSwitchIn(s, 0, &log)
	if p.Ability != "intimidate" {
		t.Errorf("Trace: copied ability = %q, want intimidate", p.Ability)
	}
	if s.Active(1).Stages.Atk != -1 {
		t.Errorf("Traced Intimidate: foe Atk stage = %d, want -1", s.Active(1).Stages.Atk)
	}

	// Untraceable ability (Trace mirror): the holder keeps Trace.
	s, _ = NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	p = s.Active(0)
	p.Ability = "trace"
	s.Active(1).Ability = "trace"
	applyOnSwitchIn(s, 0, &log)
	if p.Ability != "trace" {
		t.Errorf("Trace vs Trace: ability = %q, want trace (inert)", p.Ability)
	}
}

// TestScrappyHitsGhost: Scrappy lets a Normal move connect with a Ghost-type
// for neutral damage, where it normally does nothing.
func TestScrappyHitsGhost(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{94}, 1) // Snorlax vs Gengar (Ghost/Poison)
	atk := s.Active(0)
	def := s.Active(1)
	tackle := d.Moves["tackle"] // Normal

	if base := ExpectedDamage(d, atk, def, tackle, nil, nil, nil); base != 0 {
		t.Fatalf("baseline Normal vs Ghost: %d, want 0 (immune)", base)
	}
	atk.Ability = "scrappy"
	if got := ExpectedDamage(d, atk, def, tackle, nil, nil, nil); got <= 0 {
		t.Errorf("Scrappy Normal vs Ghost: %d, want > 0", got)
	}
}

// TestCursedBodyDisablesOnHit: being struck by a damaging move gives the
// holder a 30% chance to disable that move on its attacker. The rate is
// measured; the substitute refusal — the holder was not really struck — has to
// hold on every attempt.
func TestCursedBodyDisablesOnHit(t *testing.T) {
	d := loadDex(t)
	struck := func(behindSub bool) func(uint64) bool {
		return func(seed uint64) bool {
			s, err := NewBattle(d, "cursed", "P1", []int{143}, "P2", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			holder := s.Active(0)
			holder.Ability = "cursed-body"
			holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			if behindSub {
				holder.Volatiles.Substitute = &SubstituteState{HP: 60}
			}
			atk := s.Active(1)
			atk.Ability = AbilityNone
			atk.Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}}

			ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
			dis := s.Active(1).Volatiles.Disable
			return dis != nil && dis.MoveID == "tackle"
		}
	}

	assertRate(t, "Cursed Body on a direct hit", 0.30, struck(false))
	assertNever(t, "Cursed Body through a substitute", struck(true))
}

// TestArenaTrapAndMagnetPullTrapSwitches: Arena Trap bars a grounded foe from
// switching; Magnet Pull bars a Steel foe; Ghost-types ignore both.
func TestArenaTrapAndMagnetPullTrapSwitches(t *testing.T) {
	d := loadDex(t)
	hasSwitch := func(acts []Action) bool {
		for _, a := range acts {
			if a.Kind == ActionSwitch {
				return true
			}
		}
		return false
	}

	// Arena Trap vs a grounded foe (Snorlax): no switch offered.
	s, _ := NewBattle(d, "b", "P1", []int{143, 9}, "P2", []int{143}, 1)
	s.Active(1).Ability = "arena-trap"
	if hasSwitch(LegalActions(s, 0)) {
		t.Errorf("Arena Trap: grounded foe should not be able to switch")
	}
	// Remove the ability → switching returns.
	s.Active(1).Ability = ""
	if !hasSwitch(LegalActions(s, 0)) {
		t.Errorf("no trapper: foe should be able to switch")
	}

	// Ghost-types escape Arena Trap.
	s.Active(1).Ability = "arena-trap"
	s.Active(0).Type1 = "ghost"
	if !hasSwitch(LegalActions(s, 0)) {
		t.Errorf("Arena Trap should not hold a Ghost-type")
	}

	// Magnet Pull holds only Steel foes.
	s, _ = NewBattle(d, "b", "P1", []int{143, 9}, "P2", []int{143}, 1)
	s.Active(1).Ability = "magnet-pull"
	if !hasSwitch(LegalActions(s, 0)) {
		t.Errorf("Magnet Pull should not hold a non-Steel foe")
	}
	s.Active(0).Type1 = "steel"
	if hasSwitch(LegalActions(s, 0)) {
		t.Errorf("Magnet Pull: Steel foe should be trapped")
	}
}

// TestInfiltratorIgnoresScreensAndSub: Infiltrator's damage ignores the
// defender's Reflect, and its moves strike through a substitute.
func TestInfiltratorIgnoresScreensAndSub(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[143]) // Snorlax
	def := buildPokemon(d, d.Species[143])
	tackle := d.Moves["tackle"] // physical

	screens := &SideConditions{Reflect: &ScreenState{TurnsLeft: 5}}
	behind := ExpectedDamage(d, &atk, &def, tackle, nil, nil, screens)
	atk.Ability = "infiltrator"
	through := ExpectedDamage(d, &atk, &def, tackle, nil, nil, screens)
	if through <= behind {
		t.Errorf("Infiltrator vs Reflect: %d (behind) → %d (through), want higher", behind, through)
	}
	// Should match the no-screen number.
	atk.Ability = ""
	noScreen := ExpectedDamage(d, &atk, &def, tackle, nil, nil, nil)
	if through != noScreen {
		t.Errorf("Infiltrator damage %d != no-screen damage %d", through, noScreen)
	}

	// Substitute transparency.
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	s.Active(1).Volatiles.Substitute = &SubstituteState{HP: 50}
	if bypassesSubstitute(tackle, s.Active(0)) {
		t.Fatalf("baseline: Tackle should not bypass a substitute")
	}
	s.Active(0).Ability = "infiltrator"
	if !bypassesSubstitute(tackle, s.Active(0)) {
		t.Errorf("Infiltrator: Tackle should pass through the substitute")
	}
}

// TestHookFreeAbilitiesStaySilent: every registry entry that carries only
// Kind is registered (so the roster is explicitly complete) and fires no
// hooks — a switch-in produces no announcement.
//
// Two different reasons put an ability here, and the list deliberately mixes
// them: some have no modelable effect yet (rivalry, forewarn),
// while others are fully functional through a layer that reads the slug
// directly — Gluttony via pinchThresholdFor, Sticky Hold via itemIsRemovable,
// Klutz via itemSuppressed. What is asserted is the same either way: no hook,
// no switch-in noise.
func TestHookFreeAbilitiesStaySilent(t *testing.T) {
	d := loadDex(t)
	inert := []AbilityKind{
		"gluttony", "rivalry", "sticky-hold", "klutz",
		"forewarn", "illuminate", "run-away", "healer",
	}
	for _, ab := range inert {
		a := abilityRegistry[ab]
		if a == nil {
			t.Errorf("%s: not registered — roster incomplete", ab)
			continue
		}
		if a.Kind != ab {
			t.Errorf("%s: registered under mismatched Kind %q", ab, a.Kind)
		}
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
		s.Active(0).Ability = ab
		var log []LogLine
		applyOnSwitchIn(s, 0, &log)
		if len(log) != 0 {
			t.Errorf("%s: expected an inert switch-in, got log %v", ab, log)
		}
	}
}

// TestFriskRevealsFoeItem: Frisk announces the foe's held item on entry and
// stays silent when the foe holds nothing.
func TestFriskRevealsFoeItem(t *testing.T) {
	d := loadDex(t)

	// Foe holding an item: entry log names it.
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	s.Active(0).Ability = "frisk"
	s.Active(1).Item = ItemChoiceBand
	var log []LogLine
	applyOnSwitchIn(s, 0, &log)
	if !logHas(log, "found its Choice Band") {
		t.Errorf("Frisk should reveal the foe's Choice Band; log=%v", log)
	}

	// Itemless foe: nothing is logged.
	s2, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	s2.Active(0).Ability = "frisk"
	s2.Active(1).Item = ItemNone
	var log2 []LogLine
	applyOnSwitchIn(s2, 0, &log2)
	if logHas(log2, "frisked") {
		t.Errorf("Frisk should stay silent against an itemless foe; log=%v", log2)
	}
}

// TestMoldBreakerPiercesDefensiveAbilities: a Mold Breaker attacker ignores
// the target's Levitate immunity, Thick Fat damage reduction, and Sturdy
// OHKO survival — each of which stops or blunts the hit for a normal attacker.
func TestMoldBreakerPiercesDefensiveAbilities(t *testing.T) {
	d := loadDex(t)
	earthquake := d.Moves["earthquake"] // Ground
	flamethrower := d.Moves["flamethrower"]

	// Levitate: Ground immunity is lifted by Mold Breaker.
	atk := buildPokemon(d, d.Species[143])
	def := buildPokemon(d, d.Species[143]) // Snorlax — neutral to Ground
	def.Ability = "levitate"
	if r := computeDamage(d, &atk, &def, earthquake, nil, nil, nil, nil, NewRNG(1)); r.Damage != 0 || r.Effectiveness != 0 {
		t.Fatalf("baseline: Levitate should block Ground, got dmg=%d eff=%v", r.Damage, r.Effectiveness)
	}
	atk.Ability = "mold-breaker"
	if r := computeDamage(d, &atk, &def, earthquake, nil, nil, nil, nil, NewRNG(1)); r.Damage == 0 {
		t.Errorf("Mold Breaker should ignore Levitate; earthquake dealt 0")
	}

	// Thick Fat: the ×0.5 on Fire is removed, so damage is higher. Same seed
	// keeps the random roll fixed, so the comparison is deterministic.
	tf := buildPokemon(d, d.Species[143])
	tf.Ability = "thick-fat"
	normalATK := buildPokemon(d, d.Species[143])
	blunted := computeDamage(d, &normalATK, &tf, flamethrower, nil, nil, nil, nil, NewRNG(3))
	normalATK.Ability = "mold-breaker"
	full := computeDamage(d, &normalATK, &tf, flamethrower, nil, nil, nil, nil, NewRNG(3))
	if full.Damage <= blunted.Damage {
		t.Errorf("Mold Breaker vs Thick Fat: full=%d should exceed blunted=%d", full.Damage, blunted.Damage)
	}

	// Sturdy: a full-HP lethal hit is survived normally, but not against Mold
	// Breaker. Read through a battle rather than off DamageResult, because
	// Sturdy is no longer decided in computeDamage — it is a survival effect and
	// lives in dealDamage's chain beside Endure and Focus Sash, which is where
	// canon resolves all three. The observable is the same and is what the
	// mechanic is actually about: does the Pokemon live.
	survives := func(atkAbility AbilityKind) bool {
		s, err := NewBattle(d, "b", "P1", []int{112}, "P2", []int{95}, 1) // Rhydon vs Onix
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability = atkAbility
		s.Active(1).Ability = AbilitySturdy
		s.Active(0).Moves = []MoveSlot{{MoveID: "earthquake", PP: 10, MaxPP: 10}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(0).Stats.Atk = 999
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		return !s.Sides[1].Team[0].Fainted
	}
	if !survives(AbilityNone) {
		t.Fatalf("baseline: Sturdy should survive the lethal hit")
	}
	if survives(AbilityMoldBreaker) {
		t.Errorf("Mold Breaker should ignore Sturdy's OHKO survival")
	}
}

// TestMoxieSkipsTheKOThatEndsTheBattle: the last KO of a sweep pays nothing.
//
// Canon reaches Moxie through onSourceAfterFaint, and faintMessages runs
// `if (checkWin && this.checkWin()) return true` on the line before it fires
// that event — so a battle-ending faint returns early and the boost never
// happens. Nothing observable depends on the difference, which is exactly why
// it is worth a test: an engine that pays out here looks right in every log a
// player would read, and is wrong.
func TestMoxieSkipsTheKOThatEndsTheBattle(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	atk := s.Active(0)
	atk.Ability = "moxie"
	atk.Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}}
	foe := s.Active(1)
	foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	foe.HP = 1

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !foe.Fainted {
		t.Fatalf("setup: the tackle should have KO'd a 1 HP foe")
	}
	if s.Winner != 0 {
		t.Fatalf("setup: that KO should have won the battle, winner = %d", s.Winner)
	}
	if got := atk.Stages.Atk; got != 0 {
		t.Errorf("the KO that ends the battle should not boost Moxie, Atk stage = %d", got)
	}
}
