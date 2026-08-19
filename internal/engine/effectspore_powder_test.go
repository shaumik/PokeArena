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

// The immunity check must sit *after* both of Effect Spore's rolls. A guard
// placed before them would skip draws the unfixed engine made, which would
// shift the RNG stream for every battle that ever hit a Parasect — including
// the ones where nothing was immune and no behaviour changed at all. That is
// the difference between a fix that invalidates recorded replays and one that
// does not, and it is why the faint-window fix rolled first and checked after.
//
// Scope, stated honestly: this does not claim the stream is untouched for a
// target that is *now* immune. It cannot be — the fix stops a status being
// applied, and applying a status draws (sleep rolls its own duration). Where
// behaviour changes, the stream changes; that is what a behaviour fix means.
// What is pinned here is that the guard itself spends nothing and defers
// nothing, so a target with no immunity runs bit-identically to before.
//
// splitmix64 advances its state by one fixed constant per draw, so the exact
// number of draws a call consumed is recoverable arithmetic rather than an
// inference.
func TestEffectSporeImmunityCheckSitsAfterBothRolls(t *testing.T) {
	d := loadDex(t)
	contact := d.Moves["vine-whip"]

	// splitmix64 adds one fixed constant to its state per draw, so the draw
	// count is recoverable — but only by stepping forward and comparing, never
	// by dividing the difference: the state wraps at 2^64 and integer division
	// of a wrapped difference silently reports nonsense (two draws read as
	// zero, which is how the first draft of this helper "passed").
	const step = 0x9E3779B97F4A7C15
	drawsTaken := func(seed, end uint64) (int, bool) {
		st := seed
		for n := 0; n <= 16; n++ {
			if st == end {
				return n, true
			}
			st += step
		}
		return -1, false
	}

	// Three ways to be immune: by typing, by item, by ability. All three must
	// spend the rider's rolls before refusing.
	targets := []struct {
		name    string
		dexNo   int
		ability AbilityKind
		item    ItemKind
	}{
		{"grass type", 3, AbilityNone, ItemNone},
		{"safety goggles", 143, AbilityNone, "safety-goggles"},
		{"overcoat", 91, "overcoat", ItemNone},
	}

	for _, tc := range targets {
		t.Run(tc.name, func(t *testing.T) {
			var triggered, skipped int
			for seed := uint64(0); seed < 200; seed++ {
				// The rider's first draw is its 30% trigger roll, so the same
				// draw off a fresh generator tells us independently whether
				// this seed triggers — no need to infer it from the outcome.
				probe := NewRNG(seed)
				fires := probe.Chance(30)

				s, err := NewBattle(d, "b", "A", []int{tc.dexNo, 143}, "B", []int{47, 143}, seed)
				if err != nil {
					t.Fatal(err)
				}
				atk := s.Active(0)
				atk.Ability, atk.Item = tc.ability, tc.item
				s.Active(1).Ability = "effect-spore"

				rng := NewRNG(seed)
				log := []LogLine{}
				applyOnHit(s, 1, contact, false, rng, &log)

				// One draw for the trigger roll; a second for the outcome roll
				// the guard must not have skipped.
				want := 1
				if fires {
					want = 2
					triggered++
				} else {
					skipped++
				}
				got, ok := drawsTaken(seed, rng.State())
				if !ok {
					t.Fatalf("seed %d: could not account for the generator state after the rider", seed)
				}
				if got != want {
					t.Fatalf("seed %d (trigger fired=%v): rider consumed %d draws, want %d — "+
						"the immunity guard is sitting before a roll instead of after it",
						seed, fires, got, want)
				}
				if s.Active(0).Status != StatusNone {
					t.Fatalf("seed %d: %s was statused despite powder immunity", seed, s.Active(0).Name)
				}
			}
			// Neither branch may be empty, or the assertion above is only
			// testing one half of the behaviour and the test has gone quietly
			// vacuous — which is exactly how its first version passed against
			// a guard placed on the wrong side of the rolls.
			if triggered == 0 || skipped == 0 {
				t.Fatalf("only one branch exercised (%d triggered, %d skipped); test is vacuous",
					triggered, skipped)
			}
		})
	}
}

// powderRefusedBy now delegates its three immunities to powderImmuneBy, so the
// *move* path — Sleep Powder, Stun Spore, Spore — has to keep behaving exactly
// as it did before that refactor. Mold Breaker is the interesting axis: it
// ignores the target's ability, so it beats Overcoat, and it is not an item or
// a typing, so it must not touch the other two.
//
// This is also the honest answer to a mutation the Effect Spore tests do not
// catch. Passing the spore holder as the breaker instead of nil is not a
// behaviour change and cannot be: a Pokémon has exactly one Ability field, so
// nothing can hold Effect Spore and Mold Breaker at once, and the argument is
// dead either way. The nil at that call site documents intent — Mold Breaker
// ignores abilities for its holder's own *moves*, and a contact rider is not a
// move — while this test pins the behaviour that is actually reachable.
func TestPowderImmunityAndMoldBreaker(t *testing.T) {
	d := loadDex(t)
	sleepPowder := d.Moves["sleep-powder"]
	if !sleepPowder.HasFlag("powder") {
		t.Fatal("sleep-powder lost its powder flag; this test is aimed at the wrong move")
	}

	mk := func(dexNo int, ability AbilityKind, item ItemKind) *Pokemon {
		p := buildPokemon(d, d.Species[dexNo])
		p.Ability, p.Item = ability, item
		return &p
	}
	plain := mk(143, AbilityNone, ItemNone)      // Snorlax, no immunity, no mold breaking
	breaker := mk(127, "mold-breaker", ItemNone) // Pinsir

	cases := []struct {
		name       string
		atk, def   *Pokemon
		wantRefuse bool
		wantReason string
	}{
		{"overcoat refuses a powder move", plain, mk(91, "overcoat", ItemNone), true, "Overcoat"},
		{"mold breaker beats overcoat", breaker, mk(91, "overcoat", ItemNone), false, ""},
		{"grass typing refuses, and mold breaker cannot touch a typing",
			breaker, mk(3, AbilityNone, ItemNone), true, ""},
		{"safety goggles refuse, and mold breaker cannot touch an item",
			breaker, mk(143, AbilityNone, "safety-goggles"), true, "Safety Goggles"},
		{"no immunity at all", plain, mk(143, AbilityNone, ItemNone), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, refused := powderRefusedBy(tc.atk, tc.def, sleepPowder)
			if refused != tc.wantRefuse {
				t.Fatalf("refused = %v, want %v (reason %q)", refused, tc.wantRefuse, reason)
			}
			if refused && reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}

	// And the flag gate itself: a move with no powder flag is never refused,
	// which is the property that made the Effect Spore rider unreachable in the
	// first place and must stay true for moves.
	if _, refused := powderRefusedBy(plain, mk(3, AbilityNone, ItemNone), d.Moves["tackle"]); refused {
		t.Error("a non-powder move was refused by powder immunity")
	}
}
