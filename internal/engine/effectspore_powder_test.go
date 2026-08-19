package engine

import "testing"

// Effect Spore has been a powder effect since Gen VI: Grass types, Overcoat
// and Safety Goggles are immune to it exactly as they are to Spore itself.
//
// The engine had the immunity and could not reach it. powderRefusedBy opens
// with `if !m.HasFlag("powder")`, and the move that triggers Effect Spore is
// the *attacker's* contact move — Close Combat, Flare Blitz — which carries no
// powder flag, so the guard returned "not refused" every time. The rider went
// straight to inflictStatusFrom with no filter of its own. A Round 1 referee
// found it statically; it never fired live because the only contact hit on
// that Parasect was Fire-type.
//
// The immunity now lives in powderImmuneBy, which asks about a Pokémon rather
// than about a move aimed at one, and both callers share it.
func TestEffectSporeRespectsPowderImmunity(t *testing.T) {
	d := loadDex(t)
	contact := d.Moves["vine-whip"] // Grass, physical, contact
	if !moveMakesContact(contact, nil) {
		t.Fatalf("test needs a contact move; vine-whip has flags %v", contact.Flags)
	}

	cases := []struct {
		name    string
		dexNo   int
		ability AbilityKind
		item    ItemKind
		immune  bool
	}{
		// Venusaur is Grass/Poison: immune by typing, and poison-immune too,
		// so it also proves the guard is not just the status immunity talking.
		{"grass type", 3, AbilityNone, ItemNone, true},
		{"safety goggles", 143, AbilityNone, "safety-goggles", true},
		{"overcoat", 91, "overcoat", ItemNone, true},
		// Snorlax with neither: the control. It must still be catchable, or
		// the test would pass on an ability that simply stopped working.
		{"no immunity", 143, AbilityNone, ItemNone, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statused := false
			// The rider is a 30% roll and then a 1-in-3 outcome, so a single
			// seed proves nothing either way. Sweep enough of them that the
			// control is certain to catch something.
			for seed := uint64(0); seed < 300; seed++ {
				s, err := NewBattle(d, "b", "A", []int{tc.dexNo, 143}, "B", []int{47, 143}, seed)
				if err != nil {
					t.Fatal(err)
				}
				atk := s.Active(0)
				atk.Ability, atk.Item = tc.ability, tc.item
				def := s.Active(1)
				def.Ability = "effect-spore"

				rng := NewRNG(seed)
				log := []LogLine{}
				applyOnHit(s, 1, contact, false, rng, &log)
				if s.Active(0).Status != StatusNone {
					statused = true
					if tc.immune {
						t.Fatalf("seed %d: %s took %v from Effect Spore, want immunity",
							seed, s.Active(0).Name, s.Active(0).Status)
					}
					break
				}
			}
			if !tc.immune && !statused {
				t.Errorf("Effect Spore never landed in 300 seeds against a legal target — "+
					"the guard is refusing something it should not (%s)", tc.name)
			}
		})
	}
}

// The immunity check must sit *after* both of Effect Spore's rolls, so adding
// it cannot shift the RNG stream for anything downstream in the same turn.
// This is the same discipline the faint-window fix used, and it is what keeps
// old replays valid across the change.
func TestEffectSporeImmunityDoesNotShiftTheRNGStream(t *testing.T) {
	d := loadDex(t)
	contact := d.Moves["vine-whip"]

	draw := func(dexNo int) []int {
		s, err := NewBattle(d, "b", "A", []int{dexNo, 143}, "B", []int{47, 143}, 7)
		if err != nil {
			t.Fatal(err)
		}
		s.Active(1).Ability = "effect-spore"
		rng := NewRNG(7)
		log := []LogLine{}
		applyOnHit(s, 1, contact, false, rng, &log)
		// Whatever the rider consumed, the next few draws must line up.
		out := make([]int, 5)
		for i := range out {
			out[i] = rng.IntN(1000)
		}
		return out
	}

	// Venusaur is immune, Snorlax is not. Both must leave the stream in the
	// same place.
	immune, catchable := draw(3), draw(143)
	for i := range immune {
		if immune[i] != catchable[i] {
			t.Fatalf("RNG diverged at draw %d: immune target %v, catchable target %v — "+
				"the immunity check is consuming (or skipping) a roll", i, immune, catchable)
		}
	}
}
