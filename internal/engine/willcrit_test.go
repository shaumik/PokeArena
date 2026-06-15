package engine

import "testing"

// TestAlwaysCritLandsEveryTime: Frost Breath and Storm Throw always deal a
// critical hit. Across many RNG seeds the crit flag must never come back false.
func TestAlwaysCritLandsEveryTime(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[144]) // Articuno
	def := buildPokemon(d, d.Species[143]) // Snorlax
	for _, id := range []string{"frost-breath", "storm-throw"} {
		m := d.Moves[id]
		for i := 0; i < 300; i++ {
			rng := NewRNG(uint64(i*2654435761 + 1))
			if res := computeDamage(d, &atk, &def, m, nil, nil, nil, nil, rng); !res.Crit {
				t.Fatalf("%s should always crit, seed %d returned Crit=false", id, i)
			}
		}
	}
}

// TestNormalMoveSometimesDoesNotCrit guards against the always-crit hook
// leaking onto ordinary moves: a regular 1/24 move must produce non-crits.
func TestNormalMoveSometimesDoesNotCrit(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[144])
	def := buildPokemon(d, d.Species[143])
	ice := d.Moves["ice-beam"]
	sawNonCrit := false
	for i := 0; i < 300 && !sawNonCrit; i++ {
		rng := NewRNG(uint64(i*40503 + 7))
		if !computeDamage(d, &atk, &def, ice, nil, nil, nil, nil, rng).Crit {
			sawNonCrit = true
		}
	}
	if !sawNonCrit {
		t.Error("ice-beam should produce at least one non-crit across 300 rolls")
	}
}

// TestAlwaysCritBlockedByArmor: Battle Armor / Shell Armor still veto the
// guaranteed crit (the absolute block runs after the always-crit override).
func TestAlwaysCritBlockedByArmor(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[144]) // Articuno
	def := buildPokemon(d, d.Species[91])  // Cloyster — Shell Armor (slot 0)
	fb := d.Moves["frost-breath"]
	for i := 0; i < 50; i++ {
		rng := NewRNG(uint64(i*2654435761 + 3))
		if res := computeDamage(d, &atk, &def, fb, nil, nil, nil, nil, rng); res.Crit {
			t.Fatalf("Shell Armor should block Frost Breath's crit, seed %d returned Crit=true", i)
		}
	}
}
