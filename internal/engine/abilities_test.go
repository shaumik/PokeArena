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
