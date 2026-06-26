package engine

import (
	"testing"

	"pokearena/internal/domain"
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

// TestShedSkinCuresOnRoll: Shed Skin clears a major status when its 30% roll
// fires (seed 2), and leaves it when the roll fails (seed 1).
func TestShedSkinCuresOnRoll(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{6}, "P2", []int{9}, 1)
	p := s.Active(0)
	p.Ability = "shed-skin"

	// Roll fails (seed 1 → Chance(30) false): status persists.
	p.Status = StatusBurn
	var log []LogLine
	applyAbilityEndOfTurn(s, 0, NewRNG(1), &log)
	if p.Status != StatusBurn {
		t.Errorf("Shed Skin cured on a failed roll: status = %v, want burn", p.Status)
	}

	// Roll fires (seed 2 → Chance(30) true): status clears.
	applyAbilityEndOfTurn(s, 0, NewRNG(2), &log)
	if p.Status != StatusNone {
		t.Errorf("Shed Skin failed to cure on a passing roll: status = %v, want none", p.Status)
	}
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

// TestCuteCharmInfatuatesOnContact: a contact hit infatuates the attacker on
// the 30% roll (seed 2), does nothing on a failed roll (seed 1), and never
// fires for a non-contact move.
func TestCuteCharmInfatuatesOnContact(t *testing.T) {
	d := loadDex(t)
	setup := func() (*BattleState, *Pokemon) {
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
		s.Active(0).Ability = "cute-charm"
		return s, s.Active(1) // the attacker
	}

	// Roll fires (seed 2): attacker falls in love.
	s, foe := setup()
	var log []LogLine
	applyOnHit(s, 0, d.Moves["tackle"], false, NewRNG(2), &log)
	if !foe.Volatiles.Attract {
		t.Errorf("Cute Charm failed to infatuate on a passing contact roll")
	}

	// Roll fails (seed 1): no infatuation.
	s, foe = setup()
	applyOnHit(s, 0, d.Moves["tackle"], false, NewRNG(1), &log)
	if foe.Volatiles.Attract {
		t.Errorf("Cute Charm infatuated on a failed roll")
	}

	// Non-contact move never triggers, even on the passing seed.
	s, foe = setup()
	applyOnHit(s, 0, d.Moves["water-gun"], false, NewRNG(2), &log)
	if foe.Volatiles.Attract {
		t.Errorf("Cute Charm infatuated from a non-contact move")
	}
}

// TestMoxieBoostsOnKO: scoring a KO with a damaging move raises Moxie's
// Attack; a hit that leaves the foe standing does not.
func TestMoxieBoostsOnKO(t *testing.T) {
	d := loadDex(t)
	run := func(foeHP int) *Pokemon {
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
		atk := s.Active(0)
		atk.Ability = "moxie"
		atk.Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}}
		foe := s.Active(1)
		foe.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		foe.HP = foeHP
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		return atk
	}

	// foe at 1 HP → KO → +1 Attack.
	if atk := run(1); atk.Stages.Atk != 1 {
		t.Errorf("Moxie after a KO: Atk stage = %d, want +1", atk.Stages.Atk)
	}
	// healthy foe survives the tackle → no boost.
	if atk := run(999); atk.Stages.Atk != 0 {
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
