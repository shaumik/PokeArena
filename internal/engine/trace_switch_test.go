package engine

import "testing"

// Trace's copy is field-scoped: canon has the tracer wear the borrowed
// ability only while it is on the field, and copy again — from whoever is
// standing opposite this time — every time it comes back.
//
// The engine used to assign p.Ability = foe.Ability in place with nothing
// remembering what had been overwritten, so Trace fired at most once per
// battle. The semifinal referee that filed this watched Porygon Trace Flame
// Body off Moltres, pivot out, and come back opposite a Drought Ninetales
// holding Flame Body and copying nothing: a Trace user that ever switched was
// locked to its first copy for the rest of the game, and wore an ability it
// had no legal claim to in the meantime.
func TestTraceRevertsOnSwitchOutAndCopiesAgain(t *testing.T) {
	d := loadDex(t)
	// p1: Porygon (Trace) with a body to pivot to. p2 leads Moltres (Flame
	// Body) and has Ninetales (Drought) behind it, so the second copy has a
	// different ability to find.
	s, err := NewBattle(d, "b", "A", []int{137, 143}, "B", []int{146, 38}, 9)
	if err != nil {
		t.Fatal(err)
	}
	tracer := s.Active(0)
	tracer.Ability = "trace"
	s.Active(1).Ability = "flame-body"
	s.Sides[1].Team[1].Ability = "drought"

	// Trace runs on switch-in, and the battle's opening send-out already
	// happened, so trigger it the way a mid-battle entry would.
	log := []LogLine{}
	applyOnSwitchIn(s, 0, &log)
	if got := s.Active(0).Ability; got != "flame-body" {
		t.Fatalf("first Trace copied %q, want flame-body", got)
	}
	if s.Active(0).BaseAbility != "trace" {
		t.Fatalf("BaseAbility = %q after copying, want trace (nothing remembers what to restore)",
			s.Active(0).BaseAbility)
	}

	// Pivot out. The borrowed ability must not travel to the bench with it.
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
	benched := s.Sides[0].Team[0]
	if benched.Ability != "trace" {
		t.Fatalf("benched tracer holds %q, want trace back — the copy lasts only while it is on the field",
			benched.Ability)
	}
	if benched.BaseAbility != "" {
		t.Fatalf("BaseAbility = %q on the bench, want cleared once restored", benched.BaseAbility)
	}

	// Bring it back opposite a different ability: it must copy again, and copy
	// the new one rather than re-run the old.
	s.Sides[1].Active = 1
	log = log[:0]
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 0}, {Kind: ActionMove, Index: 0}})
	if got := s.Sides[0].Team[0].Ability; got != "drought" {
		t.Fatalf("second Trace copied %q, want drought — re-entry must be free to copy again", got)
	}
}

// A traced ability is public the moment Trace announces it, and knowledge does
// not un-happen: restoring the original on the way out must not re-hide it.
func TestTraceRestoreLeavesTheRevealAlone(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{137, 143}, "B", []int{146, 38}, 9)
	if err != nil {
		t.Fatal(err)
	}
	s.Active(0).Ability = "trace"
	s.Active(1).Ability = "flame-body"

	log := []LogLine{}
	applyOnSwitchIn(s, 0, &log)
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})

	if !s.Sides[0].Team[0].AbilityRevealed {
		t.Error("switching out un-revealed the tracer's ability; the copy had already announced itself")
	}
}
