package engine

import (
	"encoding/json"
	"testing"

	"pokearena/internal/domain"
)

func loadDex(t *testing.T) *domain.Dex {
	t.Helper()
	d, err := domain.LoadDex("../../data", "test")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	return d
}

func TestStatDerivation(t *testing.T) {
	// The historical fixed spread — IV 31, EV 0, neutral — must still give
	// the numbers it always gave. These two assertions predate spreads and
	// are deliberately unchanged apart from the explicit arguments: they are
	// the proof that adding EVs/IVs/natures moved nothing at the defaults.

	// Charizard base HP 78 at L50: (2*78+31)*50/100 + 50 + 10 = 153.
	if got := calcHP(78, 31, 0); got != 153 {
		t.Errorf("calcHP(78, 31, 0) = %d, want 153", got)
	}
	// Charizard base Sp.Atk 109 at L50: (2*109+31)*50/100 + 5 = 129.
	if got := calcStat(109, 31, 0, 1, 1); got != 129 {
		t.Errorf("calcStat(109, 31, 0, neutral) = %d, want 129", got)
	}
}

func TestStageMultiplier(t *testing.T) {
	cases := []struct {
		stage int
		want  float64
	}{
		{0, 1.0}, {2, 2.0}, {6, 4.0}, {-2, 0.5}, {-6, 0.25},
	}
	for _, c := range cases {
		if got := stageMultiplier(c.stage); got != c.want {
			t.Errorf("stageMultiplier(%d) = %v, want %v", c.stage, got, c.want)
		}
	}
}

func TestTypeEffectiveness(t *testing.T) {
	d := loadDex(t)
	cases := []struct {
		atk, def1, def2 domain.Type
		want            float64
	}{
		{"water", "fire", "", 2},          // water > fire
		{"fire", "grass", "poison", 2},    // fire > Venusaur (grass/poison)
		{"electric", "ground", "rock", 0}, // electric immune vs Rhydon (ground/rock)
		{"ground", "fire", "flying", 0},   // ground can't hit a flyer
		{"fire", "fire", "flying", 0.5},   // resisted by Charizard
		{"ice", "dragon", "flying", 4},    // doubly super-effective
	}
	for _, c := range cases {
		if got := d.Effectiveness(c.atk, c.def1, c.def2); got != c.want {
			t.Errorf("Effectiveness(%s vs %s/%s) = %v, want %v", c.atk, c.def1, c.def2, got, c.want)
		}
	}
}

func TestComputeDamage(t *testing.T) {
	d := loadDex(t)
	charizard := buildPokemon(d, d.Species[6]) // fire/flying
	venusaur := buildPokemon(d, d.Species[3])  // grass/poison
	flamethrower := d.Moves["flamethrower"]

	seen := map[int]bool{}
	for i := 0; i < 300; i++ {
		rng := NewRNG(uint64(i*2654435761 + 1))
		res := computeDamage(d, &charizard, &venusaur, flamethrower, nil, nil, nil, nil, rng)
		if res.Effectiveness != 2 {
			t.Fatalf("flamethrower vs Venusaur effectiveness = %v, want 2", res.Effectiveness)
		}
		if res.Damage < 1 || res.Damage > 400 {
			t.Fatalf("damage %d out of sane range", res.Damage)
		}
		seen[res.Damage] = true
	}
	if len(seen) < 5 {
		t.Errorf("expected damage variation from the random spread, saw %d distinct values", len(seen))
	}

	// Immunity short-circuits to zero damage.
	rhydon := buildPokemon(d, d.Species[112]) // ground/rock
	raichu := buildPokemon(d, d.Species[26])  // electric
	res := computeDamage(d, &raichu, &rhydon, d.Moves["thunderbolt"], nil, nil, nil, nil, NewRNG(1))
	if res.Damage != 0 || res.Effectiveness != 0 {
		t.Errorf("thunderbolt vs Rhydon = %+v, want zero damage", res)
	}
}

// drive plays a battle to completion with a fixed policy (always the first
// legal action), so battle outcomes are deterministic per seed.
func drive(d *domain.Dex, s *BattleState) int {
	guard := 0
	for !s.Ended() {
		guard++
		if guard > maxTurns*4 {
			panic("battle failed to terminate")
		}
		switch s.Phase {
		case PhaseChoosing:
			a := [2]Action{LegalActions(s, 0)[0], LegalActions(s, 1)[0]}
			ResolveTurn(d, s, a)
		case PhaseReplace:
			var sw [2]*Action
			for i := 0; i < 2; i++ {
				if s.Replace[i] {
					act := LegalActions(s, i)[0]
					sw[i] = &act
				}
			}
			ResolveReplace(s, sw)
		}
	}
	return s.Winner
}

func TestBattleTerminates(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(1); seed <= 25; seed++ {
		s, err := NewBattle(d, "b", "Red", []int{6, 9, 26}, "Blue", []int{3, 65, 143}, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		winner := drive(d, s)
		if winner < 0 || winner > 2 {
			t.Fatalf("seed %d: invalid winner %d", seed, winner)
		}
		if s.Turn > maxTurns {
			t.Fatalf("seed %d: ran %d turns, over cap", seed, s.Turn)
		}
	}
}

func TestBattleDeterminism(t *testing.T) {
	d := loadDex(t)
	run := func() string {
		s, err := NewBattle(d, "b", "Red", []int{149, 91, 38}, "Blue", []int{150, 130, 45}, 0xC0FFEE)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		drive(d, s)
		out, _ := json.Marshal(s)
		return string(out)
	}
	// Two independent runs from the same seed must serialize identically.
	first, second := run(), run()
	if first != second {
		t.Fatal("same seed produced divergent battles — engine is not deterministic")
	}
}
