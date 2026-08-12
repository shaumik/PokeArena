package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// powderLands runs one powder move from side 0 into side 1 and reports
// whether it took. Accuracy is forced to auto-hit so the result is about the
// immunity and never about a roll.
func powderLands(t *testing.T, d *domain.Dex, atkNum, defNum int, moveID string, setup func(def *Pokemon)) (bool, []LogLine) {
	t.Helper()
	s, err := NewBattle(d, "b", "P1", []int{atkNum}, "P2", []int{defNum}, 1)
	if err != nil {
		t.Fatalf("NewBattle: %v", err)
	}
	if setup != nil {
		setup(s.Active(1))
	}
	m := d.Moves[moveID]
	if m.ID == "" {
		t.Fatalf("%s not in dataset", moveID)
	}
	if !m.HasFlag("powder") {
		t.Fatalf("%s should carry the powder flag", moveID)
	}
	m.Accuracy = 0 // unmissable, so a failure here is the immunity
	var log []LogLine
	landed, _ := resolveAccuracy(s, 0, m, NewRNG(1), &log)
	return landed, log
}

// TestGrassTypesRefusePowderMoves is the fix: powder moves simply don't
// affect Grass-types. Without it Spore was a 100%-accurate unresisted sleep
// against the entire roster, which is most of what made the missing Sleep
// Clause matter.
func TestGrassTypesRefusePowderMoves(t *testing.T) {
	d := loadDex(t)
	for _, move := range []string{"spore", "sleep-powder", "stun-spore", "poison-powder"} {
		t.Run(move, func(t *testing.T) {
			// Venusaur is Grass/Poison — Grass in slot 1.
			if landed, log := powderLands(t, d, 143, 3, move, nil); landed {
				t.Errorf("%s should not affect a Grass-type; log %v", move, log)
			}
			// Parasect is Bug/Grass — the immunity has to read both slots.
			if landed, _ := powderLands(t, d, 143, 47, move, nil); landed {
				t.Errorf("%s should not affect a Grass-type in slot 2", move)
			}
			// Snorlax is Normal: the same move must still land.
			if landed, log := powderLands(t, d, 3, 143, move, nil); !landed {
				t.Errorf("%s should affect a non-Grass target; log %v", move, log)
			}
		})
	}
}

// TestOvercoatRefusesPowderMoves: Cloyster is Water/Ice, so only the ability
// can be doing the work here.
func TestOvercoatRefusesPowderMoves(t *testing.T) {
	d := loadDex(t)
	landed, log := powderLands(t, d, 143, 91, "spore", func(def *Pokemon) {
		def.Ability = "overcoat"
	})
	if landed {
		t.Errorf("Overcoat should refuse Spore; log %v", log)
	}
	if !logHas(log, "Overcoat") {
		t.Errorf("the refusal should name Overcoat; log %v", log)
	}
	// Same Pokémon without the ability: Spore lands.
	if landed, _ := powderLands(t, d, 143, 91, "spore", func(def *Pokemon) {
		def.Ability = "shell-armor"
	}); !landed {
		t.Error("a Water/Ice target without Overcoat should be hit by Spore")
	}
}

// TestMoldBreakerPiercesOvercoatButNotGrass: Overcoat is an ability, so Mold
// Breaker goes through it. A Grass-type's immunity is its typing and no
// ability-piercer touches it.
func TestMoldBreakerPiercesOvercoatButNotGrass(t *testing.T) {
	d := loadDex(t)

	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{91}, 1)
	s.Active(0).Ability = AbilityMoldBreaker
	s.Active(1).Ability = "overcoat"
	m := d.Moves["spore"]
	m.Accuracy = 0
	var log []LogLine
	if landed, _ := resolveAccuracy(s, 0, m, NewRNG(1), &log); !landed {
		t.Errorf("Mold Breaker should punch through Overcoat; log %v", log)
	}

	s2, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{3}, 1)
	s2.Active(0).Ability = AbilityMoldBreaker
	var log2 []LogLine
	if landed, _ := resolveAccuracy(s2, 0, m, NewRNG(1), &log2); landed {
		t.Error("Mold Breaker should not beat a Grass-type's powder immunity — it isn't an ability")
	}
}

// TestSafetyGogglesStillRefusePowder: the item path predates this change and
// has to keep working, including against Mold Breaker — Mold Breaker ignores
// abilities, not items.
func TestSafetyGogglesStillRefusePowder(t *testing.T) {
	d := loadDex(t)
	landed, log := powderLands(t, d, 143, 91, "spore", func(def *Pokemon) {
		def.Ability = "shell-armor"
		def.Item = ItemSafetyGoggles
	})
	if landed {
		t.Errorf("Safety Goggles should refuse Spore; log %v", log)
	}
	if !logHas(log, "Safety Goggles") {
		t.Errorf("the refusal should name the item; log %v", log)
	}

	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{91}, 1)
	s.Active(0).Ability = AbilityMoldBreaker
	s.Active(1).Ability = "shell-armor"
	s.Active(1).Item = ItemSafetyGoggles
	m := d.Moves["spore"]
	m.Accuracy = 0
	var log2 []LogLine
	if landed, _ := resolveAccuracy(s, 0, m, NewRNG(1), &log2); landed {
		t.Error("Mold Breaker should not beat Safety Goggles — it ignores abilities, not items")
	}
}

// TestNonPowderMovesUnaffected: a Grass-type is only immune to powder. Sleep
// from any other source still lands, and the refusal must not leak onto
// ordinary moves.
func TestNonPowderMovesUnaffected(t *testing.T) {
	d := loadDex(t)
	for _, id := range []string{"hypnosis", "thunder-wave", "tackle"} {
		m, ok := d.Moves[id]
		if !ok {
			continue
		}
		s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{3}, 1)
		m.Accuracy = 0
		var log []LogLine
		if landed, _ := resolveAccuracy(s, 0, m, NewRNG(1), &log); !landed {
			t.Errorf("%s carries no powder flag and should land on a Grass-type; log %v", id, log)
		}
	}
}

// TestPowderRefusalIsNotAMiss: a refused move never rolled, so Blunder Policy
// has nothing to answer. Same distinction Soundproof and Safety Goggles
// already relied on.
func TestPowderRefusalIsNotAMiss(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{3}, 1)
	m := d.Moves["spore"]
	var log []LogLine
	landed, missed := resolveAccuracy(s, 0, m, NewRNG(1), &log)
	if landed {
		t.Fatal("Spore should be refused by a Grass-type")
	}
	if missed {
		t.Error("a refused powder move is an immunity, not a miss")
	}
}

// TestSoundproofBeatsAutoHitMoves: Roar, Confide and Disarming Voice are
// sound moves that skip the accuracy roll, and the auto-hit early return used
// to fire before the Soundproof check — so an Electrode was being roared out
// of the field by a move it is supposed to be immune to. An immunity is not
// an accuracy problem, and nothing that makes a move unmissable may reach
// past one.
func TestSoundproofBeatsAutoHitMoves(t *testing.T) {
	d := loadDex(t)
	for _, id := range []string{"roar", "confide", "disarming-voice", "perish-song"} {
		m, ok := d.Moves[id]
		if !ok {
			continue
		}
		if !m.HasFlag("sound") || (!m.HasFlag("bypass-acc") && m.Accuracy != 0) {
			t.Fatalf("%s should be a sound move that skips the accuracy roll", id)
		}
		t.Run(id, func(t *testing.T) {
			// Electrode (#101) has Soundproof.
			s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{101}, 1)
			s.Active(1).Ability = "soundproof"
			var log []LogLine
			landed, missed := resolveAccuracy(s, 0, m, NewRNG(1), &log)
			if landed {
				t.Errorf("Soundproof should refuse %s even though it never rolls; log %v", id, log)
			}
			if missed {
				t.Errorf("%s was refused, not missed", id)
			}
		})
	}
}

// TestNoGuardDoesNotBeatAnImmunity: No Guard makes moves land, but it is an
// accuracy effect — it has nothing to say about a target that the move
// cannot affect at all.
func TestNoGuardDoesNotBeatAnImmunity(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "P1", []int{143}, "P2", []int{3}, 1) // vs Venusaur (Grass)
	s.Active(0).Ability = "no-guard"
	m := d.Moves["spore"]
	var log []LogLine
	if landed, _ := resolveAccuracy(s, 0, m, NewRNG(1), &log); landed {
		t.Errorf("No Guard should not push Spore through a Grass-type; log %v", log)
	}
}
