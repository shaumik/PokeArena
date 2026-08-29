package engine

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

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

// TestHighCritFlagRaisesCritRate: the "high-crit" flag adds one crit stage,
// taking the rate from 1/24 to 1/8. Guards the flag against going dead again
// — it spent a while being read by damage.go while no move in data/ carried
// it, which quietly cost Stone Edge its whole edge over Rock Slide (#130 §4).
func TestHighCritFlagRaisesCritRate(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[144]) // Articuno
	def := buildPokemon(d, d.Species[143]) // Snorlax

	edge, ok := d.Moves["stone-edge"]
	if !ok {
		t.Fatal("stone-edge missing from the dex")
	}
	if !edge.HasFlag("high-crit") {
		t.Fatal("stone-edge should carry the high-crit flag; data-sync must emit it from critRatio")
	}
	slide := d.Moves["rock-slide"]
	if slide.HasFlag("high-crit") {
		t.Fatal("rock-slide is a normal-rate move; test needs an unflagged control")
	}

	const trials = 4000
	count := func(m domain.Move) int {
		n := 0
		for i := 0; i < trials; i++ {
			rng := NewRNG(uint64(i*2654435761 + 11))
			if computeDamage(d, &atk, &def, m, nil, nil, nil, nil, rng).Crit {
				n++
			}
		}
		return n
	}
	high, normal := count(edge), count(slide)

	// 1/8 vs 1/24 over 4000 rolls — the gap is far wider than sampling noise,
	// so a plain "meaningfully more" check stays stable without a seed pin.
	if high <= normal*2 {
		t.Errorf("stone-edge crit %d/%d, rock-slide %d/%d: high-crit should roughly triple the rate",
			high, trials, normal, trials)
	}
	if normal == 0 {
		t.Error("rock-slide never crit across 4000 rolls; the control is broken")
	}
}

// TestHighCritDatasetCoverage: every move Showdown gives a boosted crit rate
// must arrive carrying the flag. A data-sync regression that drops critRatio
// would otherwise be invisible — the moves keep working, just weaker.
func TestHighCritDatasetCoverage(t *testing.T) {
	d := loadDex(t)
	want := []string{
		"stone-edge", "slash", "cross-chop", "crabhammer", "leaf-blade",
		"night-slash", "shadow-claw", "psycho-cut", "razor-leaf",
		"air-cutter", "drill-run", "blaze-kick", "karate-chop", "sky-attack",
		"cross-poison", "poison-tail", "razor-wind",
	}
	for _, id := range want {
		m, ok := d.Moves[id]
		if !ok {
			continue // not every move survives the curation filter
		}
		if !m.HasFlag("high-crit") {
			t.Errorf("%s should carry high-crit (flags %v)", id, m.Flags)
		}
	}
}
