package engine

import (
	"testing"

	"pokearena/internal/domain"
)

// Taunt is a volatile, so switching out must drop it — and bringing the same
// Pokémon back in must not resurrect it. The interaction had no coverage: the
// Taunt tests all keep the taunted Pokémon on the field, and the switching
// tests never taunt anything. It is exactly the kind of gap where a later
// refactor that rebuilds Volatiles field-by-field (rather than zeroing the
// struct) would silently leave the timer behind.
//
// The re-entry half matters most. Clearing on switch-out is the obvious half;
// the subtle failure is a stale timer surviving on the *incoming* Pokémon,
// which doSwitchWithCarry guards by zeroing both sides of the swap.
func TestTauntClearsOnSwitchOutAndDoesNotReturn(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Red", []int{3, 143}, "Blue", []int{65, 94}, 99)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}

	// Taunt the active directly: this test is about the volatile's lifecycle
	// across a switch, not about the move landing.
	var log []LogLine
	applyTauntVolatile(s.Active(0), 0, d.Moves["taunt"], s, NewRNG(1), &log)
	if s.Active(0).Volatiles.Taunt == nil {
		t.Fatal("setup: Taunt was not applied")
	}
	taunted := s.Sides[0].Active

	// Control: while the Taunt is up, the status moves really are filtered out
	// of the legal set. Without this the re-entry assertion below could pass
	// against a detector that never reports "gagged" at all.
	if hasStatusMoveOption(d, s, 0) {
		t.Fatal("setup: status moves still offered while taunted — detector is blind")
	}

	// Switch out. The taunted Pokémon leaves the field.
	doSwitch(s, 0, 1, NewRNG(2), &log)
	if got := s.Sides[0].Team[taunted].Volatiles.Taunt; got != nil {
		t.Errorf("Taunt survived switch-out: %+v", got)
	}
	if got := s.Active(0).Volatiles.Taunt; got != nil {
		t.Errorf("Taunt leaked onto the incoming Pokémon: %+v", got)
	}

	// Switch back. The previously taunted Pokémon must return clean.
	doSwitch(s, 0, taunted, NewRNG(3), &log)
	if got := s.Active(0).Volatiles.Taunt; got != nil {
		t.Errorf("Taunt returned on re-entry: %+v", got)
	}

	// The observable consequence: its status moves are legal again. Growth is
	// a status move Venusaur knows, so a still-taunted Pokémon would have it
	// filtered out of the legal set.
	if !hasStatusMoveOption(d, s, 0) {
		t.Error("no status move offered after re-entry — the Pokémon is still gagged")
	}
}

// hasStatusMoveOption reports whether any status-category move is in side's
// legal set, which is the read path Taunt actually filters.
func hasStatusMoveOption(d *domain.Dex, s *BattleState, side int) bool {
	act := s.Active(side)
	for _, a := range LegalActionsDex(d, s, side) {
		if a.Kind != ActionMove || a.Index < 0 || a.Index >= len(act.Moves) {
			continue
		}
		if d.Moves[act.Moves[a.Index].MoveID].Category == "status" {
			return true
		}
	}
	return false
}
