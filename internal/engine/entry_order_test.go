package engine

import "testing"

// Entry effects used to run interleaved with the arrivals, side 0 then side 1,
// which made the answer depend on side index. Canon draws a line this engine did
// not: two *chosen* switches are two actions in a queue, resolved one at a time
// in the outgoing Pokemon's Speed order; two replacements after a double KO are
// simultaneous, installed together and then resolved as one Speed-ordered block.
// Upstream's queue is explicit about it — `runSwitch` carries order 101 and
// `switch` order 103, so a chosen switch's entry effects run before the other
// side's switch action is even reached.

// TestSimultaneousReplacementsIntimidateEachOther stands in for "Intimidate:
// should wait until all simultaneous switch ins after double-KOs have completed
// before activating".
//
// The old shape gave one side the drop for free: p1's replacement entered and
// its Intimidate fired against a slot that still held p2's corpse, where the
// hook's own fainted check swallowed it; then p2's replacement entered and
// intimidated normally.
func TestSimultaneousReplacementsIntimidateEachOther(t *testing.T) {
	d := loadDex(t)
	const (
		chansey  = 113
		arcanine = 59
		gyarados = 130
	)
	// Both leads faint to their own burn on the same tick, so both sides are
	// replacing at once.
	s := neutralBattle(t, d, 3, []int{chansey, arcanine}, []int{chansey, gyarados})
	for _, side := range []int{0, 1} {
		teachMoves(t, d, &s.Sides[side].Team[0], "splash")
		teachMoves(t, d, &s.Sides[side].Team[1], "splash")
		s.Sides[side].Team[0].HP = 1
		s.Sides[side].Team[0].Status = StatusBurn
	}
	s.Sides[0].Team[1].Ability = "intimidate"
	s.Sides[1].Team[1].Ability = "intimidate"

	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if s.Phase != PhaseReplace || !s.Replace[0] || !s.Replace[1] {
		t.Fatalf("setup: both sides should be replacing; phase=%s replace=%v", s.Phase, s.Replace)
	}
	in0, in1 := switchTo(1), switchTo(1)
	ResolveReplace(s, [2]*Action{&in0, &in1})

	if got := s.Active(0).Stages.Atk; got != -1 {
		t.Errorf("side 0's replacement should have been intimidated by side 1's: atk %d, want -1",
			got)
	}
	if got := s.Active(1).Stages.Atk; got != -1 {
		t.Errorf("side 1's replacement should have been intimidated by side 0's: atk %d, want -1",
			got)
	}
}

// TestChosenSwitchesResolveOneAtATimeByOutgoingSpeed is the other half, and the
// reason the two phases cannot share one rule. The faster side's replacement
// arrives while the slower side's outgoing Pokemon is still on the field, so an
// Intimidate on the way in cuts a Pokemon that is about to leave and the slower
// side's arrival walks in untouched.
func TestChosenSwitchesResolveOneAtATimeByOutgoingSpeed(t *testing.T) {
	d := loadDex(t)
	const (
		electrode = 101 // 150 base Speed — leaves first
		snorlax   = 143 // 30 base Speed, carries the Intimidate in
		alakazam  = 65  // 120 base Speed
		rhydon    = 112 // 40 base Speed
	)
	// Side 0's Electrode is the fastest thing on the field, so its switch action
	// goes first and Snorlax arrives while Alakazam is still out. Rhydon then
	// arrives to an empty threat.
	s := neutralBattle(t, d, 5, []int{electrode, snorlax}, []int{alakazam, rhydon})
	for _, side := range []int{0, 1} {
		for j := range s.Sides[side].Team {
			teachMoves(t, d, &s.Sides[side].Team[j], "splash")
			s.Sides[side].Team[j].Ability = AbilityNone
		}
	}
	s.Sides[0].Team[1].Ability = "intimidate"

	log := ResolveTurn(d, s, [2]Action{switchTo(1), switchTo(1)})
	if !logHas(log, "Intimidate cuts") {
		t.Fatalf("setup: the Intimidate should have fired; log %v", logTexts(log))
	}
	if got := s.Active(1).Stages.Atk; got != 0 {
		t.Errorf("the slower side's replacement arrives after the Intimidate has already "+
			"resolved, so it should be untouched: atk %d, want 0", got)
	}
}

// TestTrickRoomReachesTheEntryPhase stands in for "Trick Room: should also
// affect the activation order for abilities and other non-move actions". The
// entry phase reads speedOrder, which reads Trick Room — so a weather race
// between two simultaneous arrivals resolves the way the turn order would, and
// the *slower* entrant has the last word.
func TestTrickRoomReachesTheEntryPhase(t *testing.T) {
	d := loadDex(t)
	const (
		chansey   = 113
		ninetales = 38  // Drought
		rhydon    = 112 // a body for Drizzle
	)
	weatherAfterEntry := func(trickRoom bool) WeatherKind {
		s := neutralBattle(t, d, 7, []int{chansey, ninetales}, []int{chansey, rhydon})
		for _, side := range []int{0, 1} {
			teachMoves(t, d, &s.Sides[side].Team[0], "splash")
			teachMoves(t, d, &s.Sides[side].Team[1], "splash")
			s.Sides[side].Team[0].HP = 1
			s.Sides[side].Team[0].Status = StatusBurn
		}
		s.Sides[0].Team[1].Ability = "drought"
		s.Sides[1].Team[1].Ability = "drizzle"
		if trickRoom {
			s.PseudoWeather.TrickRoom = &PWTimer{TurnsLeft: 5}
		}
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		if s.Phase != PhaseReplace {
			t.Fatalf("setup: both sides should be replacing; phase %s", s.Phase)
		}
		in0, in1 := switchTo(1), switchTo(1)
		ResolveReplace(s, [2]*Action{&in0, &in1})
		if s.Weather == nil {
			t.Fatalf("one of the two entrants should have set weather")
		}
		return s.Weather.Kind
	}
	// Ninetales (100 base Speed) is faster than Rhydon (40), so bare it sets
	// the sun first and the Drizzle overwrites it: rain.
	if got := weatherAfterEntry(false); got != WeatherRain {
		t.Errorf("without Trick Room the slower Drizzle should have the last word: got %q", got)
	}
	// Under Trick Room the order inverts and the sun survives.
	if got := weatherAfterEntry(true); got != WeatherSun {
		t.Errorf("under Trick Room the slower entrant goes first, so the faster one's "+
			"Drought should have the last word: got %q", got)
	}
}
