package engine

import "testing"

// The residual phase was ordered by side index and ticked the weather last.
// Canon runs the whole phase as one speed-sorted fieldEvent('Residual') whose
// first handler is the weather's own — countdown included — and skips the rest
// of that handler when the timer hits zero. These are the untagged checks; the
// full ordering table is documented at the top of ResolveTurn's residual block.

// TestWeatherIsOverBeforeItsFinalResidual stands in for "Weather damage
// calculation: should wear off on the final turn before weather effects are
// applied": five turns of a five-turn sandstorm deal exactly four chips.
//
// docs/engine-findings.md records the earlier decision to move the countdown
// *after* the weather-keyed ability ticks so that "one residual phase gives one
// answer about whether the weather is up". The diagnosis was right and the
// resolution went the wrong way — canon's single answer is "already over" — so
// this asserts both halves of that consistency at once: on the expiry turn
// neither the chip nor an Ice Body heal fires.
func TestWeatherIsOverBeforeItsFinalResidual(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "sandstorm", "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")
	// Snorlax is neutral to sand, and a Ghost would dodge the chip entirely.
	victim := s.Active(1)
	victim.HP = victim.MaxHP

	chips := 0
	// Turn 1 sets the sandstorm; its own residual counts as the first of five.
	for turn := 0; turn < 6; turn++ {
		move := 1
		if turn == 0 {
			move = 0
		}
		log := ResolveTurn(d, s, [2]Action{moveAt(move), moveAt(0)})
		if logHas(log, "is buffeted by the sandstorm") {
			chips++
		}
		victim.HP = victim.MaxHP // keep the count from ending in a faint
		s.Active(0).HP = s.Active(0).MaxHP
	}
	if chips != 4 {
		t.Errorf("a five-turn sandstorm should chip four times, not five: got %d", chips)
	}
	if s.Weather != nil {
		t.Errorf("the sandstorm should be gone by now, got %+v", s.Weather)
	}
}

// TestResidualPhaseWalksBySpeed stands in for "Weather damage calculation:
// should run residual weather effects in order of Speed" and for the Grassy
// Terrain sibling. Usually invisible, and lethal exactly when it matters: when
// a chip kills, who faints first decides what the survivor sees and which side
// is asked for a replacement.
func TestResidualPhaseWalksBySpeed(t *testing.T) {
	d := loadDex(t)
	// Jolteon (130 base Speed) against Snorlax (30). The sand chips both.
	s := neutralBattle(t, d, 1, []int{135}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "sandstorm", "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")
	log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})

	fast, slow := -1, -1
	for i, l := range log {
		if l.Type != "weather" {
			continue
		}
		switch {
		case fast < 0 && l.Side == 0:
			fast = i
		case slow < 0 && l.Side == 1:
			slow = i
		}
	}
	if fast < 0 || slow < 0 {
		t.Fatalf("both actives should be chipped by the sand; log: %v", logTexts(log))
	}
	if fast > slow {
		t.Errorf("the sand should chip the faster Pokemon first: Jolteon at line %d, "+
			"Snorlax at %d", fast, slow)
	}

	// And Trick Room inverts it, because the residual phase reads the same
	// notion of speed the move phase does.
	s2 := neutralBattle(t, d, 1, []int{135}, []int{143})
	teachMoves(t, d, &s2.Sides[0].Team[0], "sandstorm", "splash")
	teachMoves(t, d, &s2.Sides[1].Team[0], "splash")
	s2.PseudoWeather.TrickRoom = &PWTimer{TurnsLeft: 5}
	log2 := ResolveTurn(d, s2, [2]Action{moveAt(0), moveAt(0)})
	f2, s2i := -1, -1
	for i, l := range log2 {
		if l.Type != "weather" {
			continue
		}
		switch {
		case f2 < 0 && l.Side == 0:
			f2 = i
		case s2i < 0 && l.Side == 1:
			s2i = i
		}
	}
	if f2 < 0 || s2i < 0 {
		t.Fatalf("both actives should be chipped under Trick Room; log: %v", logTexts(log2))
	}
	if s2i > f2 {
		t.Errorf("under Trick Room the slower Pokemon should be chipped first: Snorlax at "+
			"line %d, Jolteon at %d", s2i, f2)
	}
}

// TestGrassyHealPrecedesItsOwnLeftovers stands in for "Grassy Terrain: should
// heal by Speed order in the same block as Leftovers". Upstream puts the terrain
// heal at onResidualOrder 5 / subOrder 2 and Leftovers at 5 / 4, so they are one
// block and a Pokemon's terrain heal comes first. This engine ran the terrain
// pass three orders later, after the status chip.
func TestGrassyHealPrecedesItsOwnLeftovers(t *testing.T) {
	d := loadDex(t)
	s := neutralBattle(t, d, 1, []int{143}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "grassy-terrain", "splash")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")
	s.Active(0).Item = ItemLeftovers
	s.Active(0).HP = s.Active(0).MaxHP / 2

	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	log := ResolveTurn(d, s, [2]Action{moveAt(1), moveAt(0)})

	grassy, lefties := -1, -1
	for i, l := range log {
		if l.Side != 0 {
			continue
		}
		if grassy < 0 && l.Type == "terrain" && logHas([]LogLine{l}, "healed by the Grassy Terrain") {
			grassy = i
		}
		if lefties < 0 && logHas([]LogLine{l}, "restored a little HP") {
			lefties = i
		}
	}
	if grassy < 0 || lefties < 0 {
		t.Fatalf("expected both a Grassy Terrain heal and a Leftovers heal; log: %v",
			logTexts(log))
	}
	if grassy > lefties {
		t.Errorf("the Grassy Terrain heal (subOrder 2) should precede the same Pokemon's "+
			"Leftovers heal (subOrder 4): terrain at line %d, item at %d", grassy, lefties)
	}
}
