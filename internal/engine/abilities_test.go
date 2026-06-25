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
