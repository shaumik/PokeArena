package engine

import (
	"math"
	"testing"
)

// TestRoostSuppressesFlyingDefense: a Roosting Pokémon loses its Flying type
// for the turn, so a Ground move that's normally immune against a Fire/Flying
// Charizard connects for Fire's 2× weakness instead.
func TestRoostSuppressesFlyingDefense(t *testing.T) {
	d := loadDex(t)
	attacker := buildPokemon(d, d.Species[112]) // Rhydon (Ground/Rock)
	charizard := buildPokemon(d, d.Species[6])  // Fire/Flying
	eq := d.Moves["earthquake"]                 // Ground

	if res := computeDamage(d, &attacker, &charizard, eq, nil, nil, nil, nil, NewRNG(1)); res.Effectiveness != 0 {
		t.Fatalf("Earthquake vs Fire/Flying Charizard should be immune, got %v", res.Effectiveness)
	}

	charizard.Volatiles.Roost = true
	res := computeDamage(d, &attacker, &charizard, eq, nil, nil, nil, nil, NewRNG(1))
	if res.Effectiveness != 2 {
		t.Errorf("Earthquake vs Roosting Charizard should be 2× (Fire weak to Ground), got %v", res.Effectiveness)
	}
}

// TestRoostHealsSetsAndClearsVolatile: Roost still heals 50% (the declarative
// Primary block), arms the Flying-suppression volatile during the turn, and
// clears it in the end-of-turn sweep so the type loss doesn't leak.
func TestRoostHealsSetsAndClearsVolatile(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{6}, "P2", []int{143}, 1) // Charizard vs Snorlax
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "roost", PP: 5, MaxPP: 5}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(0).HP = 1
	max := s.Active(0).MaxHP

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	want := 1 + int(math.Round(float64(max)*0.5))
	if want > max {
		want = max
	}
	if got := s.Active(0).HP; got != want {
		t.Errorf("Roost healed to %d, want %d (max %d)", got, want, max)
	}
	if s.Active(0).Volatiles.Roost {
		t.Error("Roost volatile should clear at end of turn")
	}
}
