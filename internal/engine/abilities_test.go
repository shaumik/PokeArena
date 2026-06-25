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
