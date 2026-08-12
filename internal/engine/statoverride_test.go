package engine

import "testing"

// TestPsystrikeReadsPhysicalDefense: Psystrike and Psyshock are special
// moves dealt against the target's Defense, which is the whole point of
// carrying them — they answer special walls that an ordinary special
// Psychic bounces off. Before the override shipped they were plain special
// moves, which made Psystrike a strictly better Choice-of-Psychic rather
// than a tool, and both agents in issue #130 exploited exactly that.
//
// Chansey (base Def 5 / SpD 105) makes the difference unmissable.
func TestPsystrikeReadsPhysicalDefense(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Species[113]; !ok {
		t.Skip("Chansey (#113) not in dataset")
	}
	atk := buildPokemon(d, d.Species[150]) // Mewtwo
	def := buildPokemon(d, d.Species[113]) // Chansey

	for _, id := range []string{"psystrike", "psyshock"} {
		m, ok := d.Moves[id]
		if !ok {
			t.Fatalf("%s missing from the dex", id)
		}
		if m.OverrideDefensiveStat != "defense" {
			t.Fatalf("%s should carry override_defensive_stat=defense, got %q", id, m.OverrideDefensiveStat)
		}
		// Same base power, same type, same category — only the stat read
		// differs, so the paper-thin Def has to make this hurt far more.
		plain := m
		plain.OverrideDefensiveStat = ""
		via, without := computeDamage(d, &atk, &def, m, nil, nil, nil, nil, NewRNG(9)),
			computeDamage(d, &atk, &def, plain, nil, nil, nil, nil, NewRNG(9))
		if via.Damage <= without.Damage {
			t.Errorf("%s vs Chansey: through Def %d, through SpD %d — the override should hit much harder",
				id, via.Damage, without.Damage)
		}
	}
}

// TestBodyPressAttacksOffDefense: the mirror case on the offensive side.
// Body Press is physical but swings the user's Defense, so a Defense boost
// has to raise its damage and an Attack boost must not.
func TestBodyPressAttacksOffDefense(t *testing.T) {
	d := loadDex(t)
	m, ok := d.Moves["body-press"]
	if !ok {
		t.Skip("body-press not in dataset")
	}
	if m.OverrideOffensiveStat != "defense" {
		t.Fatalf("body-press should carry override_offensive_stat=defense, got %q", m.OverrideOffensiveStat)
	}
	def := buildPokemon(d, d.Species[143]) // Snorlax

	base := buildPokemon(d, d.Species[112]) // Rhydon — big Def
	flat := computeDamage(d, &base, &def, m, nil, nil, nil, nil, NewRNG(4)).Damage

	withDef := base
	withDef.Stages.Def = 2
	if got := computeDamage(d, &withDef, &def, m, nil, nil, nil, nil, NewRNG(4)).Damage; got <= flat {
		t.Errorf("+2 Defense should raise Body Press damage: %d -> %d", flat, got)
	}

	withAtk := base
	withAtk.Stages.Atk = 2
	if got := computeDamage(d, &withAtk, &def, m, nil, nil, nil, nil, NewRNG(4)).Damage; got != flat {
		t.Errorf("+2 Attack should not touch Body Press damage: %d -> %d", flat, got)
	}
}

// TestBurnStillHalvesBodyPress: burn keys off the move's category, not off
// which stat the formula happens to read. A burned Body Press user is
// halved even though the number being halved is its Defense — the easy bug
// here is to move the burn check onto the stat and lose that.
func TestBurnStillHalvesBodyPress(t *testing.T) {
	d := loadDex(t)
	m, ok := d.Moves["body-press"]
	if !ok {
		t.Skip("body-press not in dataset")
	}
	def := buildPokemon(d, d.Species[143])
	healthy := buildPokemon(d, d.Species[112])
	burned := healthy
	burned.Status = StatusBurn

	clean := computeDamage(d, &healthy, &def, m, nil, nil, nil, nil, NewRNG(6)).Damage
	sick := computeDamage(d, &burned, &def, m, nil, nil, nil, nil, NewRNG(6)).Damage
	if sick >= clean {
		t.Errorf("burn should halve Body Press: healthy %d, burned %d", clean, sick)
	}
}

// TestWonderRoomSwapsPsystrikeOntoSpD: the two defensive-stat rules compose.
// Psystrike normally reads Def; Wonder Room swaps whatever the formula was
// about to read, so under it Psystrike goes through the target's SpD.
func TestWonderRoomSwapsPsystrikeOntoSpD(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Species[113]; !ok {
		t.Skip("Chansey (#113) not in dataset")
	}
	m, ok := d.Moves["psystrike"]
	if !ok {
		t.Skip("psystrike not in dataset")
	}
	atk := buildPokemon(d, d.Species[150])
	def := buildPokemon(d, d.Species[113]) // Def 5 / SpD 105

	plain := computeDamage(d, &atk, &def, m, nil, nil, nil, nil, NewRNG(3)).Damage
	pw := &PseudoWeather{WonderRoom: &PWTimer{TurnsLeft: 5}}
	swapped := computeDamage(d, &atk, &def, m, nil, nil, nil, pw, NewRNG(3)).Damage
	if swapped >= plain {
		t.Errorf("Wonder Room should push Psystrike onto Chansey's 105 SpD: without=%d with=%d", plain, swapped)
	}
}

// TestStatOverrideStagesFollowTheStat: Psystrike reads the target's Defense
// *stage*, not its Sp. Def stage. Stages travel with the stat — the same
// rule Wonder Room follows.
func TestStatOverrideStagesFollowTheStat(t *testing.T) {
	d := loadDex(t)
	m, ok := d.Moves["psystrike"]
	if !ok {
		t.Skip("psystrike not in dataset")
	}
	atk := buildPokemon(d, d.Species[150])
	base := buildPokemon(d, d.Species[143])

	flat := computeDamage(d, &atk, &base, m, nil, nil, nil, nil, NewRNG(8)).Damage

	defUp := base
	defUp.Stages.Def = 2
	if got := computeDamage(d, &atk, &defUp, m, nil, nil, nil, nil, NewRNG(8)).Damage; got >= flat {
		t.Errorf("+2 Defense should blunt Psystrike: %d -> %d", flat, got)
	}
	spdUp := base
	spdUp.Stages.SpD = 2
	if got := computeDamage(d, &atk, &spdUp, m, nil, nil, nil, nil, NewRNG(8)).Damage; got != flat {
		t.Errorf("+2 Sp. Def should not touch Psystrike: %d -> %d", flat, got)
	}
}

// TestCategoryDefaultsUnchanged: the rewrite of offensiveDefensiveStats has
// to leave every ordinary move exactly where it was. An override-free
// physical move reads Atk vs Def, a special one SpA vs SpD.
func TestCategoryDefaultsUnchanged(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[150])
	base := buildPokemon(d, d.Species[143])

	cases := []struct {
		move        string
		raise       func(*Pokemon)
		wantBlunted bool
	}{
		{"tackle", func(p *Pokemon) { p.Stages.Def = 2 }, true},
		{"tackle", func(p *Pokemon) { p.Stages.SpD = 2 }, false},
		{"psychic", func(p *Pokemon) { p.Stages.SpD = 2 }, true},
		{"psychic", func(p *Pokemon) { p.Stages.Def = 2 }, false},
	}
	for _, c := range cases {
		m, ok := d.Moves[c.move]
		if !ok {
			continue
		}
		flat := computeDamage(d, &atk, &base, m, nil, nil, nil, nil, NewRNG(2)).Damage
		boosted := base
		c.raise(&boosted)
		got := computeDamage(d, &atk, &boosted, m, nil, nil, nil, nil, NewRNG(2)).Damage
		if c.wantBlunted && got >= flat {
			t.Errorf("%s: the boosted stat should have blunted it (%d -> %d)", c.move, flat, got)
		}
		if !c.wantBlunted && got != flat {
			t.Errorf("%s: the unrelated stat should not matter (%d -> %d)", c.move, flat, got)
		}
	}
}

// TestOverrideDoesNotMoveTheScreen: only the stat read changes. Psystrike is
// still a special move, so Light Screen covers it and Reflect does not —
// a plausible-looking "make it behave physically" fix breaks this.
func TestOverrideDoesNotMoveTheScreen(t *testing.T) {
	d := loadDex(t)
	m, ok := d.Moves["psystrike"]
	if !ok {
		t.Skip("psystrike not in dataset")
	}
	atk := buildPokemon(d, d.Species[150])
	def := buildPokemon(d, d.Species[143])

	bare := computeDamage(d, &atk, &def, m, nil, nil, nil, nil, NewRNG(5)).Damage
	lightUp := &SideConditions{LightScreen: &ScreenState{TurnsLeft: 5}}
	reflectUp := &SideConditions{Reflect: &ScreenState{TurnsLeft: 5}}
	light := computeDamage(d, &atk, &def, m, nil, nil, lightUp, nil, NewRNG(5)).Damage
	reflect := computeDamage(d, &atk, &def, m, nil, nil, reflectUp, nil, NewRNG(5)).Damage
	if light >= bare {
		t.Errorf("Light Screen should still cover Psystrike: bare %d, screened %d", bare, light)
	}
	if reflect != bare {
		t.Errorf("Reflect should not cover Psystrike: bare %d, screened %d", bare, reflect)
	}
}
