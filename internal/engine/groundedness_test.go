package engine

import "testing"

// The groundedness predicate (terrain.go) replaced two predicates that
// disagreed: isGrounded, which terrain, the entry hazards and Arena Trap read,
// and an ad-hoc chain inside computeDamage that answered the same question for
// Ground-move immunity. Neither list was a superset of the other and Gravity
// was in neither, so the same Pokemon could be on the ground for one rule and
// airborne for another.
//
// These tests are the untagged half of the evidence. The Showdown port
// (internal/engine/showdown, behind a build tag CI does not compile) covers the
// same ground against upstream's own cases; this file keeps the invariant in
// the suite that actually runs on every commit.

// TestGroundednessIsOneAnswer walks every lift and every anchor and asserts
// that the damage path and the terrain/hazard path agree about each one. The
// table is the whole point: a future leg added to one and not the other fails
// here rather than in a game.
func TestGroundednessIsOneAnswer(t *testing.T) {
	d := loadDex(t)
	const (
		charizard = 6   // Fire/Flying — airborne by type
		gengar    = 94  // Ghost/Poison — a body for Levitate
		snorlax   = 143 // Normal — grounded, no help needed
	)
	cases := []struct {
		name    string
		dexNo   int
		setup   func(*Pokemon)
		gravity bool
		want    bool
	}{
		{"a plain Normal-type is grounded", snorlax, nil, false, true},
		{"a Flying-type floats", charizard, nil, false, false},
		{"Levitate floats", gengar, func(p *Pokemon) { p.Ability = AbilityLevitate }, false, false},
		{"an Air Balloon floats", snorlax, func(p *Pokemon) { p.Item = ItemAirBalloon }, false, false},
		{"Magnet Rise floats", snorlax, func(p *Pokemon) {
			p.Volatiles.MagnetRise = &MagnetRiseState{TurnsLeft: 5}
		}, false, false},
		{"Telekinesis floats", snorlax, func(p *Pokemon) {
			p.Volatiles.Telekinesis = &TelekinesisState{TurnsLeft: 3}
		}, false, false},

		{"an Iron Ball grounds a Flying-type", charizard, func(p *Pokemon) { p.Item = ItemIronBall }, false, true},
		{"an Iron Ball grounds Levitate", gengar, func(p *Pokemon) {
			p.Ability = AbilityLevitate
			p.Item = ItemIronBall
		}, false, true},
		{"Smack Down grounds a Flying-type", charizard, func(p *Pokemon) { p.Volatiles.SmackDown = true }, false, true},
		{"Ingrain grounds a Flying-type", charizard, func(p *Pokemon) { p.Volatiles.Ingrain = true }, false, true},
		{"Roost grounds a Flying-type for the turn", charizard, func(p *Pokemon) { p.Volatiles.Roost = true }, false, true},

		{"Gravity grounds a Flying-type", charizard, nil, true, true},
		{"Gravity grounds Levitate", gengar, func(p *Pokemon) { p.Ability = AbilityLevitate }, true, true},
		{"Gravity grounds a Magnet Rise user", snorlax, func(p *Pokemon) {
			p.Volatiles.MagnetRise = &MagnetRiseState{TurnsLeft: 5}
		}, true, true},
		{"Gravity grounds an Air Balloon holder", snorlax, func(p *Pokemon) { p.Item = ItemAirBalloon }, true, true},
	}

	rhydon := buildPokemon(d, d.Species[112]) // Ground/Rock, for Earthquake
	rhydon.Ability = AbilityNone
	eq := d.Moves["earthquake"]

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def := buildPokemon(d, d.Species[c.dexNo])
			def.Ability = AbilityNone
			if c.setup != nil {
				c.setup(&def)
			}
			var pw *PseudoWeather
			if c.gravity {
				pw = &PseudoWeather{Gravity: &PWTimer{TurnsLeft: 5}}
			}

			if got := isGrounded(&def, pw); got != c.want {
				t.Errorf("isGrounded = %v, want %v", got, c.want)
			}
			// The damage path has to give the same answer. A grounded target
			// takes Earthquake; an airborne one does not.
			res := computeDamage(d, &rhydon, &def, eq, nil, nil, nil, pw, NewRNG(1))
			if hit := res.Effectiveness != 0; hit != c.want {
				t.Errorf("Earthquake %s, but isGrounded says %v — the two predicates disagree",
					map[bool]string{true: "connected", false: "was refused"}[hit], c.want)
			}
		})
	}
}

// TestGravityGroundsForHazardsAndTerrainToo: the damage path is the loud half
// of groundedness, but the hazards and terrain read it as well, and those are
// where the old split cost real games — a Levitate pivot that dodged Earthquake
// and then ignored Spikes.
func TestGravityGroundsForHazardsAndTerrain(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Flyer", []int{143, 6}, "Setter", []int{143}, 1) // Snorlax + Charizard
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Sides[0].Conditions.Hazards.Spikes = 3

	var log []LogLine
	doSwitch(s, 0, 1, NewRNG(1), &log)
	if in := s.Active(0); in.HP != in.MaxHP {
		t.Fatalf("setup: a Flying-type took Spikes without Gravity (%d/%d)", in.HP, in.MaxHP)
	}

	// Same board, Gravity up.
	s2, err := NewBattle(d, "b", "Flyer", []int{143, 6}, "Setter", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s2.Sides[0].Conditions.Hazards.Spikes = 3
	s2.PseudoWeather.Gravity = &PWTimer{TurnsLeft: 5}
	log = nil
	doSwitch(s2, 0, 1, NewRNG(1), &log)
	in := s2.Active(0)
	if in.HP == in.MaxHP {
		t.Errorf("a Flying-type switched into three layers of Spikes under Gravity and took nothing")
	}
}

// TestIronBallFlattensGroundOnFlyingTypesUntilGravity pins the one leg of this
// that is the item's rule rather than groundedness's: upstream's Iron Ball
// zeroes the type modifier for *every* defending type when the holder is
// Flying, so Ground comes out exactly neutral — and stands down the moment the
// holder is grounded for a reason that does not need the ball.
func TestIronBallFlattensGroundOnFlyingTypesUntilGravity(t *testing.T) {
	d := loadDex(t)
	rhydon := buildPokemon(d, d.Species[112])
	rhydon.Ability = AbilityNone
	eq := d.Moves["earthquake"]

	eff := func(dexNo int, gravity bool) float64 {
		def := buildPokemon(d, d.Species[dexNo])
		def.Ability = AbilityNone
		def.Item = ItemIronBall
		var pw *PseudoWeather
		if gravity {
			pw = &PseudoWeather{Gravity: &PWTimer{TurnsLeft: 5}}
		}
		e, _ := typeEffectiveness(d, &rhydon, &def, eq, pw)
		return e
	}

	// Aerodactyl is Rock/Flying: 2× on the Rock half, immune on the Flying one.
	if got := eff(142, false); got != 1 {
		t.Errorf("Iron Ball Aerodactyl vs Earthquake = %vx, want a flat 1x", got)
	}
	if got := eff(142, true); got != 2 {
		t.Errorf("under Gravity the Rock half should decide: got %vx, want 2x", got)
	}
	// Butterfree is Bug/Flying: the Bug half resists.
	if got := eff(12, false); got != 1 {
		t.Errorf("Iron Ball Butterfree vs Earthquake = %vx, want a flat 1x", got)
	}
	if got := eff(12, true); got != 0.5 {
		t.Errorf("under Gravity the Bug half should decide: got %vx, want 0.5x", got)
	}
}
