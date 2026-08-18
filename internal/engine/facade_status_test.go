package engine

import "testing"

// Facade doubles off the *user's* status and ignores burn's Attack halve.
// Both halves were missing: the move was absent from statusDoublingMoves
// entirely, so a burned Guts Raticate — the whole Flame Orb package — swung a
// 70 BP move where canon gives it 140. Found by a referee agent mid-tournament
// from the raw damage numbers.
func TestFacadeDoublesOffOwnStatus(t *testing.T) {
	d := loadDex(t)
	dmg := func(st StatusCond, seed uint64) int {
		s, err := NewBattle(d, "b", "A", []int{20, 6}, "B", []int{143, 3}, seed)
		if err != nil {
			t.Fatal(err)
		}
		a, b := s.Active(0), s.Active(1)
		a.Ability, b.Ability = AbilityNone, AbilityNone
		a.Item, b.Item = ItemNone, ItemNone
		a.Moves = []MoveSlot{{MoveID: "facade", PP: 20, MaxPP: 20}}
		b.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		a.Status = st
		before := b.HP
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		return before - b.HP
	}
	// Paralysis and sleep can eat the turn outright, so compare the best roll
	// over a seed range rather than a single seed: a turn the user lost to full
	// paralysis says nothing about the move's power. The range is wide enough
	// that every status still lands the top of the 85–100 damage roll despite
	// losing a quarter of its turns, so the comparison is power against power.
	best := func(st StatusCond) int {
		top := 0
		for seed := uint64(90); seed < 490; seed++ {
			if got := dmg(st, seed); got > top {
				top = got
			}
		}
		return top
	}
	clean := best(StatusNone)
	for _, st := range []StatusCond{StatusBurn, StatusPoison, StatusToxic, StatusParalysis} {
		got := best(st)
		if got < clean*9/5 {
			t.Errorf("facade with %v peaked at %d, want ~2x the healthy %d", st, got, clean)
		}
	}
	// Sleep is excluded per canon: a Pokémon that cannot act earns no bonus.
	// On the rare seed it does connect, the power must be undoubled.
	if got := best(StatusSleep); got > clean {
		t.Errorf("facade while asleep peaked at %d, want no more than the undoubled %d", got, clean)
	}
}

// A burned Guts user must not have the halve multiplied back out of a
// reduction Facade already exempted it from.
func TestGutsFacadeDoesNotDoubleCancelBurn(t *testing.T) {
	d := loadDex(t)
	dmg := func(move string, ability AbilityKind, st StatusCond) int {
		s, err := NewBattle(d, "b", "A", []int{20, 6}, "B", []int{143, 3}, 99)
		if err != nil {
			t.Fatal(err)
		}
		a, b := s.Active(0), s.Active(1)
		a.Ability, b.Ability = ability, AbilityNone
		a.Item, b.Item = ItemNone, ItemNone
		a.Moves = []MoveSlot{{MoveID: move, PP: 20, MaxPP: 20}}
		b.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		a.Status = st
		before := b.HP
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		return before - b.HP
	}
	// Guts is x1.5 on top of the doubled Facade — not x3.
	plain := dmg("facade", AbilityNone, StatusBurn)
	guts := dmg("facade", "guts", StatusBurn)
	if guts < plain*13/10 || guts > plain*17/10 {
		t.Errorf("burned Guts Facade dealt %d against a plain burned %d; want ~1.5x, not a double-canceled 3x", guts, plain)
	}
	// Body Slam still eats the burn halve, and Guts still cancels it.
	bsPlain := dmg("body-slam", AbilityNone, StatusNone)
	bsBurn := dmg("body-slam", AbilityNone, StatusBurn)
	if bsBurn >= bsPlain {
		t.Errorf("burned Body Slam dealt %d, want less than the healthy %d — the burn halve should still apply", bsBurn, bsPlain)
	}
}
