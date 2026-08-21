package engine

import "testing"

// abilitysuppression_behavior_test.go is the whole-battle specification for
// Neutralizing Gas and Gastro Acid. Every test here builds a battle, submits
// legal actions and reads exported state or the turn log — nothing calls
// abilityOf, syncAbilitySuppression or any other internal, so a port can be
// written against these before any of this engine's decomposition exists.
//
// The mechanic earned a file of its own because it is the one the second
// battle royale actually lost a Pokémon to: The Caltrops switched Weezing in
// to shut off a foe's ability, and Neutralizing Gas was registered with no
// hooks and no suppression code anywhere in the repo. "The registry entry
// exists" was never the claim worth testing. The claim worth testing is that
// suppression is visible in a played turn, which is what each test below
// insists on.
//
// The hard half of the mechanic is not stopping abilities, it is resuming
// them. Three cases have to come out differently and they are covered by
// TestNeutralizingGasDoesNotUndoWhatAlreadyHappened and
// TestNeutralizingGasResumesAbilitiesWhenItLeaves respectively:
//
//	weather Drought already set    stays up — the gas suppresses abilities, not their effects
//	an Intimidate that already hit  stays applied — it was a one-off, and it is spent
//	Multiscale, Drought's re-entry  genuinely stop, and genuinely restart
//
// Fixture species: Weezing (110) carries Neutralizing Gas, Ninetales (38)
// Drought, Dragonite (149) Multiscale, Arcanine (59) Intimidate, Dugtrio (51)
// Arena Trap. Abilities are assigned explicitly rather than left to the dex so
// each fixture states what it is about.

// gasBattle builds Weezing-on-side-0 against a foe whose ability the test
// names, both taught a single harmless move so a turn can be played without
// anything else moving the board.
func gasBattle(t *testing.T, seed uint64, foeDex int, foeAbility AbilityKind) *BattleState {
	t.Helper()
	d := loadDex(t)
	s := neutralBattle(t, d, seed, []int{110}, []int{foeDex})
	s.Active(0).Ability = AbilityNeutralizingGas
	s.Active(1).Ability = foeAbility
	teachMoves(t, d, s.Active(0), "tackle")
	teachMoves(t, d, s.Active(1), "splash")
	return s
}

// --- suppression is visible in damage -------------------------------------

// Multiscale halves damage taken at full HP. Under Neutralizing Gas it does
// not, and the difference shows up as a bigger number on the very first hit.
//
// Asserted as a paired comparison at every seed rather than against a fixed
// damage figure: the damage roll is random, so "Dragonite took 42" pins the
// generator, while "gassed hurts more than ungassed, always" pins the rule.
// The control run is the same battle with the gas holder stripped of its
// ability, so the only thing that differs between the two is suppression.
//
// This is the test that would have told The Caltrops what they were buying.
func TestNeutralizingGasSuppressesMultiscale(t *testing.T) {
	d := loadDex(t)
	damageTaken := func(seed uint64, gas bool) int {
		s := gasBattle(t, seed, 149, "multiscale")
		if !gas {
			s.Active(0).Ability = AbilityNone
		}
		before := s.Active(1).HP
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		return before - s.Active(1).HP
	}

	assertAlwaysOver(t, "a gassed Multiscale takes more than a working one", 60,
		func(seed uint64) bool { return damageTaken(seed, true) > damageTaken(seed, false) })

	// The control has to be doing its job, or the line above passes for the
	// wrong reason (a Multiscale that never fires is also "not halving").
	// Roughly double is the shape of the halving; the band is loose because
	// each side of the comparison carries its own damage roll.
	var gassed, working int
	for seed := uint64(1); seed <= 60; seed++ {
		gassed += damageTaken(seed, true)
		working += damageTaken(seed, false)
	}
	if ratio := float64(gassed) / float64(working); ratio < 1.7 || ratio > 2.3 {
		t.Errorf("gassed/working damage ratio = %.2f over 60 seeds, want about 2.0 "+
			"(Multiscale halves) — the control is not halving, so the comparison above is empty",
			ratio)
	}
}

// --- suppression of an entry ability --------------------------------------

// A Drought holder that enters a gassed field does not set the sun. The
// ordering is the mechanic here: both leads arrive at the same moment on turn
// 1, and the gas has to be filling the field before the opposing lead's entry
// hook is asked to run. Canon reaches this by giving Neutralizing Gas an
// onPreStart that precedes every onStart.
//
// The sun is read off the field rather than off the log, because "no weather
// line was printed" would also pass for a Drought that set the sun silently.
func TestNeutralizingGasSuppressesAnEntryAbility(t *testing.T) {
	d := loadDex(t)
	s := gasBattle(t, 7, 38, "drought")
	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})

	if s.Weather != nil {
		t.Errorf("Drought set %s through the gas; want no weather at all", s.Weather.Kind)
	}
	if !logHas(log, "Neutralizing Gas filled the area") {
		t.Errorf("the gas never announced itself; log: %v", logTexts(log))
	}

	// Control on the same fixture: without the gas the sun goes up on turn 1,
	// so the assertion above is a suppression and not a broken Drought.
	c := gasBattle(t, 7, 38, "drought")
	c.Active(0).Ability = AbilityNone
	ResolveTurn(d, c, [2]Action{moveAt(0), moveAt(0)})
	if c.Weather == nil || c.Weather.Kind != WeatherSun {
		t.Fatal("control: Drought did not set the sun with no gas on the field — " +
			"the entry hook is not running at all, and the suppression test above proves nothing")
	}
}

// A Drought holder that switches into a gas already standing on the field is
// suppressed on arrival — not a turn later.
//
// A different path from the turn-1 lead case above and worth its own test:
// there the suppression was established before either Pokémon existed on the
// field, here it has to be applied to a Pokémon walking into it, in the window
// between the switch landing and that Pokémon's own entry hook running. Get the
// order wrong by one step and the sun goes up, then never comes down — the
// suppression looks correct from every later turn's point of view.
func TestNeutralizingGasSuppressesAnAbilitySwitchingIntoIt(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 21, []int{110}, []int{143, 38})
	s.Active(0).Ability = AbilityNeutralizingGas
	s.Sides[1].Team[1].Ability = "drought"
	teachMoves(t, d, s.Active(0), "splash")
	teachMoves(t, d, s.Active(1), "splash")
	teachMoves(t, d, &s.Sides[1].Team[1], "splash")

	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	log := ResolveTurn(d, s, [2]Action{moveAt(0), switchTo(1)})

	if s.Active(1).Ability != "drought" {
		t.Fatalf("precondition: the Drought holder never came in (active is %s)", s.Active(1).Name)
	}
	if s.Weather != nil {
		t.Errorf("Drought set %s on the way into a standing gas; want no weather. log: %v",
			s.Weather.Kind, logTexts(log))
	}
	// Suppression starting is not suppression ending. The wear-off line belongs
	// to one direction only, and a resume announced on arrival would both read
	// as nonsense and — because log text is inside the golden fingerprint —
	// re-record 147 fixtures for an event that did not happen.
	if logHas(log, "wore off") {
		t.Errorf("switching into the gas announced a wear-off; log: %v", logTexts(log))
	}
}

// --- resume ---------------------------------------------------------------

// When the gas leaves the field, the abilities it was holding down come back —
// and canon does more than un-set a flag: it re-runs the switch-in ability of
// everything still out (Showdown's neutralizinggas.onEnd calls singleEvent
// 'Start'). So a Drought holder that entered *into* the gas gets its sun at
// the moment the gas clears, rather than never.
//
// Without the re-run, this Ninetales would stand in a permanently sunless
// battle after one Weezing pivot, which is neither canon nor anything a player
// could reason about.
func TestNeutralizingGasResumesAbilitiesWhenItLeaves(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 11, []int{110, 143}, []int{38})
	s.Active(0).Ability = AbilityNeutralizingGas
	s.Active(1).Ability = "drought"
	teachMoves(t, d, s.Active(0), "tackle")
	teachMoves(t, d, &s.Sides[0].Team[1], "tackle")
	teachMoves(t, d, s.Active(1), "splash")

	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if s.Weather != nil {
		t.Fatalf("precondition: the sun is up under the gas (%s)", s.Weather.Kind)
	}

	// Weezing pivots out. The gas goes with it.
	log := ResolveTurn(d, s, [2]Action{switchTo(1), moveAt(0)})

	if s.Weather == nil || s.Weather.Kind != WeatherSun {
		t.Errorf("weather after the gas left = %v, want sun — Drought did not resume; log: %v",
			s.Weather, logTexts(log))
	}
	if !logHas(log, "Neutralizing Gas wore off") {
		t.Errorf("the field never said the gas had lifted; log: %v", logTexts(log))
	}
}

// The gas ends the moment its holder faints, not at the end of the turn and
// not when the replacement arrives. A Pokémon that KOs the Weezing in front of
// it has its ability back for the rest of that same turn.
//
// Worth pinning separately from the switch-out case because it runs through a
// different path entirely: nothing switches, and the fainted holder stays the
// active until the replace phase — so a suppression keyed only on "who is in
// the slot" would keep gassing the field from beyond the grave.
func TestNeutralizingGasResumesWhenTheHolderFaints(t *testing.T) {
	d := loadDex(t)
	sunAfterTheKO := func(seed uint64) bool {
		s := neutralBattle(t, d, seed, []int{110, 143}, []int{38})
		s.Active(0).Ability = AbilityNeutralizingGas
		s.Active(1).Ability = "drought"
		teachMoves(t, d, s.Active(0), "splash")
		teachMoves(t, d, s.Active(1), "tackle")
		// One HP, so the hit lands the KO whatever the damage roll says. The
		// roll is left alone: the test is about what happens after the faint,
		// not about how big the hit was.
		s.Active(0).HP = 1
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		return s.Active(0).Fainted && s.Weather != nil && s.Weather.Kind == WeatherSun
	}
	assertAlwaysOver(t, "Drought resumes the moment the gas holder faints", 40, sunAfterTheKO)
}

// Multiscale is the other half of the resume: unlike Drought it has no entry
// hook to re-run, it is simply read on every hit, so it has to start working
// again on its own the moment the gas is gone. Stopping and restarting is the
// whole difference between a suppression and a one-way disable.
func TestNeutralizingGasResumesMultiscale(t *testing.T) {
	d := loadDex(t)
	// Weezing gasses turn 1, pivots out on turn 2, and the Persian behind it
	// swings on turn 3 into a Multiscale that should be working again.
	damageOnTurn3 := func(seed uint64, pivot bool) int {
		s := neutralBattle(t, d, seed, []int{110, 53}, []int{149})
		s.Active(0).Ability = AbilityNeutralizingGas
		s.Active(1).Ability = "multiscale"
		teachMoves(t, d, s.Active(0), "tackle")
		teachMoves(t, d, &s.Sides[0].Team[1], "tackle")
		teachMoves(t, d, s.Active(1), "splash")

		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		if pivot {
			ResolveTurn(d, s, [2]Action{switchTo(1), moveAt(0)})
		} else {
			ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		}
		// Multiscale reads full HP, so the turn-3 target is topped back up.
		// Restoring HP through the exported field is fixture arrangement; the
		// hit itself still goes through ResolveTurn.
		s.Active(1).HP = s.Active(1).MaxHP
		before := s.Active(1).HP
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		return before - s.Active(1).HP
	}

	// Not a paired comparison this time — the two runs use different attackers
	// (Weezing stays in, or Persian comes in), so their damage is not
	// comparable seed by seed. Totals over a sweep are, once each is scaled by
	// nothing more than whether Multiscale fired.
	var withGas, gasGone int
	for seed := uint64(1); seed <= 60; seed++ {
		withGas += damageOnTurn3(seed, false)
		gasGone += damageOnTurn3(seed, true)
	}
	if gasGone == 0 || withGas == 0 {
		t.Fatalf("fixture never landed a hit: withGas=%d gasGone=%d", withGas, gasGone)
	}
	// Both attackers are frail Normal-types hitting the same Dragonite, so a
	// working Multiscale should roughly halve the second column. Anything at
	// or above parity means it never came back on.
	if float64(gasGone) >= 0.8*float64(withGas) {
		t.Errorf("damage with the gas gone = %d vs %d under it — Multiscale did not resume "+
			"(a resumed Multiscale should cut its column roughly in half)", gasGone, withGas)
	}
}

// --- what suppression must NOT do -----------------------------------------

// Suppressing an ability does not reach back and undo what it already did.
// Weather that Drought put up stays up, and an Intimidate that already cut an
// Attack stays cut — the gas stops abilities from acting, it does not rewind
// their effects.
//
// This is the pair of cases that makes "resume" a design question rather than
// a flag, and getting either backwards is invisible until a real game turns on
// it: a gas switch-in that cleared the sun would silently hand the sun-setter's
// opponent a free Solar Beam nerf every time.
func TestNeutralizingGasDoesNotUndoWhatAlreadyHappened(t *testing.T) {
	d := loadDex(t)
	// Ninetales leads into Arcanine; behind Arcanine sits the Weezing.
	s := neutralBattle(t, d, 3, []int{38}, []int{59, 110})
	s.Active(0).Ability = "drought"
	s.Active(1).Ability = "intimidate"
	s.Sides[1].Team[1].Ability = AbilityNeutralizingGas
	teachMoves(t, d, s.Active(0), "splash")
	teachMoves(t, d, s.Active(1), "splash")
	teachMoves(t, d, &s.Sides[1].Team[1], "splash")

	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if s.Weather == nil || s.Weather.Kind != WeatherSun {
		t.Fatalf("precondition: Drought did not set the sun (weather %v)", s.Weather)
	}
	if s.Active(0).Stages.Atk != -1 {
		t.Fatalf("precondition: Intimidate did not cut Attack (stage %d)", s.Active(0).Stages.Atk)
	}

	// The gas arrives.
	ResolveTurn(d, s, [2]Action{moveAt(0), switchTo(1)})

	if s.Active(1).Ability != AbilityNeutralizingGas {
		t.Fatalf("precondition: the gas never came in (active is %s)", s.Active(1).Name)
	}
	if s.Weather == nil || s.Weather.Kind != WeatherSun {
		t.Errorf("weather after the gas arrived = %v, want the sun still up — "+
			"the gas suppresses abilities, not the weather they set", s.Weather)
	}
	if got := s.Active(0).Stages.Atk; got != -1 {
		t.Errorf("Attack stage after the gas arrived = %d, want -1 still — "+
			"an Intimidate that already fired does not un-fire", got)
	}
}

// Neutralizing Gas does not suppress Neutralizing Gas. Both holders keep it,
// which matters because the alternative — each canceling the other — would
// make a gas mirror match silently ability-free for everything else too.
//
// Read off the announcements: a suppressed ability has no hook to run, so an
// entry line from both sides is the front-door evidence that neither was
// switched off.
func TestNeutralizingGasDoesNotSuppressItself(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 5, []int{110}, []int{110})
	for side := 0; side < 2; side++ {
		s.Active(side).Ability = AbilityNeutralizingGas
		teachMoves(t, d, s.Active(side), "splash")
	}
	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})

	announcements := 0
	for _, l := range log {
		if l.Type == "ability" && l.Side >= 0 &&
			logHas([]LogLine{l}, "Neutralizing Gas filled the area") {
			announcements++
		}
	}
	if announcements != 2 {
		t.Errorf("%d gas announcements over a mirror match, want 2 — "+
			"a gas holder is suppressing the other; log: %v", announcements, logTexts(log))
	}
}

// Arena Trap is an ability like any other, so the gas frees whoever it was
// holding. Checked through LegalActions because that is where a player meets
// the rule: under the gas the switch option is simply there again.
func TestNeutralizingGasFreesATrappedPokemon(t *testing.T) {
	d := loadDex(t)
	// Weezing is a pure Poison-type here (Levitate would make it ungrounded
	// and untrappable for reasons that have nothing to do with the gas), and
	// Dugtrio across from it holds the trap.
	build := func(gas bool) *BattleState {
		s := neutralBattle(t, d, 9, []int{110, 143}, []int{51})
		if gas {
			s.Active(0).Ability = AbilityNeutralizingGas
		}
		s.Active(1).Ability = "arena-trap"
		teachMoves(t, d, s.Active(0), "splash")
		teachMoves(t, d, s.Active(1), "splash")
		return s
	}
	// Asked at the choice point after a played turn, which is where a
	// controller actually asks it — and, for this fixture, the first moment
	// the board is settled, since neutralBattle assigns abilities after the
	// battle is built.
	canSwitchAfterATurn := func(s *BattleState) bool {
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		for _, a := range LegalActionsDex(d, s, 0) {
			if a.Kind == ActionSwitch {
				return true
			}
		}
		return false
	}

	if canSwitchAfterATurn(build(false)) {
		t.Fatal("control: Arena Trap did not trap at all, so the gas test below proves nothing")
	}
	if !canSwitchAfterATurn(build(true)) {
		t.Error("Arena Trap still holds the Weezing that is gassing it")
	}
}

// --- Gastro Acid ----------------------------------------------------------

// Gastro Acid suppresses the ability of the Pokémon it hits, for as long as
// that Pokémon stays on the field. It shares the mechanism with Neutralizing
// Gas — the same one gate on the same lookup — and it needs its own test
// because its flag used to be set and read by nothing: the move printed "its
// ability was suppressed!" and then nothing was.
func TestGastroAcidSuppressesTheTargetsAbility(t *testing.T) {
	d := loadDex(t)
	damageAfter := func(seed uint64, douse bool) int {
		s := neutralBattle(t, d, seed, []int{53}, []int{149})
		s.Active(1).Ability = "multiscale"
		teachMoves(t, d, s.Active(0), "gastro-acid", "tackle")
		teachMoves(t, d, s.Active(1), "splash")

		open := 1 // tackle, the no-op opener for the control run
		if douse {
			open = 0
		}
		ResolveTurn(d, s, [2]Action{moveAt(open), moveAt(0)})
		// Multiscale only reads full HP, so the opener's chip is undone before
		// the measured hit — otherwise the control would lose Multiscale for a
		// reason that has nothing to do with Gastro Acid.
		s.Active(1).HP = s.Active(1).MaxHP
		before := s.Active(1).HP
		ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})
		return before - s.Active(1).HP
	}

	assertAlwaysOver(t, "a doused Multiscale takes more than an untouched one", 60,
		func(seed uint64) bool { return damageAfter(seed, true) > damageAfter(seed, false) })
}

// Gastro Acid landed on the gas holder ends the gas: Neutralizing Gas is
// itself an ability, and nothing exempts it from being switched off. Canon
// runs the checks in exactly this order — Showdown's Pokemon#ignoringAbility
// tests the gastroacid volatile before it reaches the gas exemption — so a
// doused Weezing is just a Weezing, and everything it was holding down comes
// back.
//
// The order is the whole content of the test. Reversed, the gas would be
// immune to the one move designed to answer it.
func TestGastroAcidOnTheGasHolderEndsTheGas(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 13, []int{38}, []int{110})
	s.Active(0).Ability = "drought"
	s.Active(1).Ability = AbilityNeutralizingGas
	teachMoves(t, d, s.Active(0), "splash", "gastro-acid")
	teachMoves(t, d, s.Active(1), "splash")

	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if s.Weather != nil {
		t.Fatalf("precondition: the sun is already up under the gas (%s)", s.Weather.Kind)
	}

	log := ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})
	if s.Weather == nil || s.Weather.Kind != WeatherSun {
		t.Errorf("weather after dousing the gas = %v, want sun — Gastro Acid did not "+
			"switch off Neutralizing Gas; log: %v", s.Weather, logTexts(log))
	}
}

// Gastro Acid's suppression lasts exactly as long as the target stays out:
// the volatile goes with the rest of the bag on switch-out, so the same
// Pokémon coming back has its ability again.
//
// Multiscale rather than an entry ability, deliberately. Multiscale is re-read
// on every incoming hit and has no switch-in hook at all, so what it measures
// is the suppression itself still being consulted turn after turn — an entry
// ability can only ever report on the one instant it fired.
func TestGastroAcidWearsOffOnSwitchOut(t *testing.T) {
	d := loadDex(t)
	// Douse the Dragonite, optionally pivot it out and back, then measure one
	// hit against it at full HP.
	damageAfter := func(seed uint64, pivot bool) int {
		s := neutralBattle(t, d, seed, []int{53}, []int{149, 143})
		s.Sides[1].Team[0].Ability = "multiscale"
		teachMoves(t, d, s.Active(0), "gastro-acid", "tackle")
		teachMoves(t, d, s.Active(1), "splash")
		teachMoves(t, d, &s.Sides[1].Team[1], "splash")

		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		if pivot {
			ResolveTurn(d, s, [2]Action{moveAt(1), switchTo(1)})
			ResolveTurn(d, s, [2]Action{moveAt(1), switchTo(0)})
		}
		// Multiscale only reads full HP, so the chip from the turns above is
		// undone before the measured hit. Fixture arrangement through an
		// exported field; the hit itself still goes through ResolveTurn.
		target := &s.Sides[1].Team[0]
		target.HP = target.MaxHP
		before := target.HP
		ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})
		return before - target.HP
	}

	assertAlwaysOver(t, "a doused Pokémon that pivoted out takes less than one still doused", 60,
		func(seed uint64) bool { return damageAfter(seed, true) < damageAfter(seed, false) })
}

// Abilities resume in time for the same turn's residual damage, not merely by
// the next turn. A Magic Guard holder that KOs the Weezing gassing it takes no
// sandstorm chip at the end of that turn — the gas ended when the holder went
// down, and the weather residual comes after that.
//
// This is the narrowest window the mechanic has, and it is the one an
// end-of-turn-only re-derive gets wrong: the residual block runs before any
// tidy-up at the turn boundary, so a suppression refreshed only at the edges of
// a turn is still up while the chip lands. Worth a Pokémon's life in a real
// game, and invisible to every other test here.
func TestNeutralizingGasResumesInTimeForTheSameTurnsResidual(t *testing.T) {
	d := loadDex(t)
	// Alakazam (Magic Guard) in a sandstorm, across from a Weezing on its last
	// HP. koTheGas decides whether this turn's hit actually takes the gas down.
	sandChipTaken := func(seed uint64, koTheGas bool) int {
		s := neutralBattle(t, d, seed, []int{110}, []int{65})
		s.Active(0).Ability = AbilityNeutralizingGas
		s.Active(1).Ability = "magic-guard"
		teachMoves(t, d, s.Active(0), "splash")
		teachMoves(t, d, s.Active(1), "tackle")
		s.Weather = &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}
		if koTheGas {
			s.Active(0).HP = 1
		}
		before := s.Active(1).HP
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		return before - s.Active(1).HP
	}

	// Control first: while the gas is up, Magic Guard is off and the sand bites.
	assertAlwaysOver(t, "a gassed Magic Guard holder still takes sandstorm chip", 40,
		func(seed uint64) bool { return sandChipTaken(seed, false) > 0 })

	assertAlwaysOver(t, "Magic Guard is back in time for the sand chip that same turn", 40,
		func(seed uint64) bool { return sandChipTaken(seed, true) == 0 })
}

// An ability freed by a residual kill still gets its own end-of-turn tick that
// same turn. Weezing chipped to death by the sandstorm at the top of the
// residual block does not get to hold Speed Boost down through the bottom of
// it.
//
// The residual block is a long ordered sequence — weather chip first, ability
// ticks near the end — and the gas can die partway along it. Nothing else in
// this file crosses that boundary: every other resume is triggered by a move,
// a switch or a faint that happens while moves are still resolving.
func TestNeutralizingGasResumesBetweenResidualSteps(t *testing.T) {
	d := loadDex(t)
	// Golem is Rock/Ground, so the sandstorm that kills the Weezing does not
	// touch it and the only thing that can move its Speed stage is the ability.
	speedStageAfterTheTurn := func(seed uint64, killTheGas bool) int {
		s := neutralBattle(t, d, seed, []int{110}, []int{76})
		s.Active(0).Ability = AbilityNeutralizingGas
		s.Active(1).Ability = "speed-boost"
		teachMoves(t, d, s.Active(0), "splash")
		teachMoves(t, d, s.Active(1), "splash")
		s.Weather = &WeatherState{Kind: WeatherSandstorm, TurnsLeft: 5}
		if killTheGas {
			s.Active(0).HP = 1
		}
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		return s.Active(1).Stages.Spe
	}

	assertAlwaysOver(t, "a gassed Speed Boost does not tick", 40,
		func(seed uint64) bool { return speedStageAfterTheTurn(seed, false) == 0 })

	assertAlwaysOver(t, "Speed Boost ticks on the turn the sand kills the gas", 40,
		func(seed uint64) bool { return speedStageAfterTheTurn(seed, true) == 1 })
}

// The very first legal-action list a controller asks for already knows about
// the gas — before any turn has resolved.
//
// Built through NewBattleFromPicks with the abilities named in the picks,
// because that is how a real match starts and because it is the only way to
// reach the question honestly: the helpers elsewhere in this file assign
// abilities after the battle exists, which no real caller does. A controller
// asks what it may do before it does anything, and "may I switch out" is
// answered from state alone.
func TestNeutralizingGasFreesATrappedPokemonOnTheFirstChoice(t *testing.T) {
	d := loadDex(t)
	build := func(gasAbility string) *BattleState {
		s, err := NewBattleFromPicks(d, "behavior",
			"P1", []TeamPick{
				{DexNo: 110, Ability: gasAbility, MoveIDs: []string{"sludge-bomb"}},
				{DexNo: 143, MoveIDs: []string{"body-slam"}},
			},
			"P2", []TeamPick{{DexNo: 51, Ability: "arena-trap", MoveIDs: []string{"earthquake"}}}, 4)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		return s
	}
	canSwitch := func(s *BattleState) bool {
		for _, a := range LegalActionsDex(d, s, 0) {
			if a.Kind == ActionSwitch {
				return true
			}
		}
		return false
	}

	// "stench" is Weezing's other legal ability and does nothing here — the
	// control differs from the fixture in exactly the gas and nothing else.
	if canSwitch(build("stench")) {
		t.Fatal("control: Arena Trap did not trap on the first choice, so the test below proves nothing")
	}
	if !canSwitch(build("neutralizing-gas")) {
		t.Error("the first legal-action list still traps a Weezing that is gassing the trapper")
	}
}

// --- the state contract at the turn and replace boundaries -----------------
//
// The two tests below are about the mirror agreeing with the field at the two
// moments the engine promises it does, rather than about a damage number. They
// exist because both windows are real and neither is reachable by any of the
// gameplay tests above: mutation testing removed each of the two sync points
// they cover and nothing else in this file went red.

// The gas holder can die in the *late* residuals — after the ability ticks,
// at the very bottom of the turn — and the field has to be consistent by the
// time the turn hands back.
//
// What this pins is the state contract, not a visible play: everything that
// would read an ability next re-derives suppression on the way in, so a stale
// mirror here shows up as a malformed state rather than as a wrong turn. That
// is exactly what ValidateStateInvariants is for, and it is asserted directly
// because no sequence of actions makes the difference visible any other way.
func TestNeutralizingGasLeavesAConsistentStateAfterALateResidualKO(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 23, []int{110}, []int{38})
	s.Active(0).Ability = AbilityNeutralizingGas
	s.Active(1).Ability = "drought"
	teachMoves(t, d, s.Active(0), "splash")
	teachMoves(t, d, s.Active(1), "splash")
	// Perish Song counts at the very end of the residual order, below every
	// heal, chip and ability tick. At zero the holder faints there.
	s.Active(0).Volatiles.PerishSong = &PerishState{TurnsLeft: 0}

	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})

	if !s.Active(0).Fainted {
		t.Fatalf("precondition: the perish count did not take the gas holder (HP %d)", s.Active(0).HP)
	}
	if err := ValidateStateInvariants(s); err != nil {
		t.Errorf("the turn ended with the field and the mirror disagreeing: %v", err)
	}
}

// A replacement can die on the way in — Stealth Rock is waiting — and if that
// replacement was the gas, the field must not be left thinking the gas arrived.
//
// The switch itself installs suppression before entry hazards run, which is the
// right order (a Pokémon that survives the rocks really is gassing the field
// from the moment it lands). It also means the hazard KO happens with the
// mirror already set, so the replace phase is the last chance to put it back.
func TestNeutralizingGasReplacementKilledByHazardsLeavesNoGas(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 29, []int{38}, []int{143, 110})
	s.Active(0).Ability = "drought"
	s.Sides[1].Team[1].Ability = AbilityNeutralizingGas
	teachMoves(t, d, s.Active(0), "stealth-rock", "tackle")
	teachMoves(t, d, s.Active(1), "splash")
	teachMoves(t, d, &s.Sides[1].Team[1], "splash")

	// Rocks down on side 1, then its lead is taken.
	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if !s.Sides[1].Conditions.Hazards.StealthRock {
		t.Fatal("precondition: Stealth Rock never landed on side 1")
	}
	s.Active(1).HP = 1
	ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})
	if s.Phase != PhaseReplace {
		t.Fatalf("precondition: side 1 did not need to replace (phase %s)", s.Phase)
	}

	// The Weezing behind it walks into the rocks on one HP and does not survive.
	s.Sides[1].Team[1].HP = 1
	log := ResolveReplace(s, [2]*Action{nil, {Kind: ActionSwitch, Index: 1}})

	if !s.Sides[1].Team[1].Fainted {
		t.Fatalf("precondition: the rocks did not take the Weezing (HP %d)", s.Sides[1].Team[1].HP)
	}
	if err := ValidateStateInvariants(s); err != nil {
		t.Errorf("a gas that died on entry left the field inconsistent: %v; log: %v",
			err, logTexts(log))
	}
	if s.Active(0).Volatiles.AbilitySuppressed {
		t.Error("Drought is still suppressed by a Weezing that never finished switching in")
	}
}
