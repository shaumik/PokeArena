package engine

import (
	"testing"

	"pokearena/internal/domain"
)

func slotPtr(i int) *int { return &i }

// pivotBattle puts Jolteon (Volt Switch, Baton Pass) in front of a three-deep
// bench so "lowest live" and "the one I want" are different answers.
func pivotBattle(t *testing.T, d *domain.Dex, moveID string) *BattleState {
	t.Helper()
	// Jolteon leads; Aerodactyl sits at slot 1, Mewtwo at slot 2 — the exact
	// shape from the issue, where a Volt Switch aimed at Mewtwo landed on
	// Aerodactyl because Aerodactyl was alive and earlier in the party.
	s, err := NewBattle(d, "b", "P1", []int{135, 142, 150}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 20, MaxPP: 20}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	return s
}

// TestSelfSwitchHonorsTheChosenTarget is the fix: a pivot move brings in the
// teammate the controller named, not whoever sits earliest in party order.
func TestSelfSwitchHonorsTheChosenTarget(t *testing.T) {
	d := loadDex(t)
	s := pivotBattle(t, d, "volt-switch")

	ResolveTurn(d, s, [2]Action{
		{Kind: ActionMove, Index: 0, SwitchTarget: slotPtr(2)},
		{Kind: ActionMove, Index: 0},
	})
	if got := s.Sides[0].Active; got != 2 {
		t.Errorf("Volt Switch aimed at slot 2 brought in slot %d (%s)", got, s.Active(0).Name)
	}
}

// TestSelfSwitchWithoutATargetIsUnchanged: an untargeted action still takes
// the lowest-indexed live teammate, so replays and controllers that predate
// the field behave exactly as before.
func TestSelfSwitchWithoutATargetIsUnchanged(t *testing.T) {
	d := loadDex(t)
	s := pivotBattle(t, d, "volt-switch")

	ResolveTurn(d, s, [2]Action{
		{Kind: ActionMove, Index: 0},
		{Kind: ActionMove, Index: 0},
	})
	if got := s.Sides[0].Active; got != 1 {
		t.Errorf("an untargeted pivot should take the lowest live slot (1), got %d", got)
	}
}

// TestBatonPassCarriesToTheChosenTeammate: the boosts have to land on the
// Pokémon the pass was aimed at. Passing +2 to "whoever sits earliest" isn't
// a strategy, which is what made party order a hidden stat.
func TestBatonPassCarriesToTheChosenTeammate(t *testing.T) {
	d := loadDex(t)
	s := pivotBattle(t, d, "baton-pass")
	s.Active(0).Stages.SpA = 2
	s.Active(0).Stages.Spe = 1

	ResolveTurn(d, s, [2]Action{
		{Kind: ActionMove, Index: 0, SwitchTarget: slotPtr(2)},
		{Kind: ActionMove, Index: 0},
	})
	if s.Sides[0].Active != 2 {
		t.Fatalf("Baton Pass should have brought in slot 2, got %d", s.Sides[0].Active)
	}
	in := s.Active(0)
	if in.Stages.SpA != 2 || in.Stages.Spe != 1 {
		t.Errorf("boosts should have traveled to the chosen teammate: SpA=%d Spe=%d, want 2/1",
			in.Stages.SpA, in.Stages.Spe)
	}
	if s.Sides[0].Team[1].Stages.SpA != 0 {
		t.Error("the teammate that was not chosen should not have received the boosts")
	}
}

// TestSelfSwitchFallsBackOnAnUnreachableTarget: a fainted, out-of-range or
// already-active choice takes the default rather than failing the move.
// LegalActions never offers these, so this only fires for a controller that
// went around it — and canon has no "your pivot fizzled" outcome to copy.
func TestSelfSwitchFallsBackOnAnUnreachableTarget(t *testing.T) {
	d := loadDex(t)
	for _, c := range []struct {
		name string
		want *int
		prep func(*BattleState)
	}{
		{"out of range", slotPtr(9), nil},
		{"negative", slotPtr(-3), nil},
		{"the active itself", slotPtr(0), nil},
		{"a fainted teammate", slotPtr(2), func(s *BattleState) {
			s.Sides[0].Team[2].Fainted = true
			s.Sides[0].Team[2].HP = 0
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := pivotBattle(t, d, "volt-switch")
			if c.prep != nil {
				c.prep(s)
			}
			ResolveTurn(d, s, [2]Action{
				{Kind: ActionMove, Index: 0, SwitchTarget: c.want},
				{Kind: ActionMove, Index: 0},
			})
			if got := s.Sides[0].Active; got != 1 {
				t.Errorf("should have fallen back to slot 1, got %d", got)
			}
		})
	}
}

// TestLegalActionsEnumeratesPivotTargets: the option has to be discoverable,
// so a self-switch move appears once per bench member it could reach.
func TestLegalActionsEnumeratesPivotTargets(t *testing.T) {
	d := loadDex(t)
	s := pivotBattle(t, d, "volt-switch")

	acts := LegalActionsDex(d, s, 0)
	seen := map[int]bool{}
	moves := 0
	for _, a := range acts {
		if a.Kind != ActionMove {
			continue
		}
		moves++
		if a.SwitchTarget == nil {
			t.Errorf("a pivot move should be offered with a target, got %+v", a)
			continue
		}
		seen[*a.SwitchTarget] = true
	}
	if moves != 2 || !seen[1] || !seen[2] {
		t.Errorf("want one entry per live bench member (slots 1 and 2), got %d entries %v", moves, seen)
	}

	// The dex-less path can't know the move self-switches, so it offers the
	// move once, untargeted — and the engine picks, as it always did.
	for _, a := range LegalActions(s, 0) {
		if a.Kind == ActionMove && a.SwitchTarget != nil {
			t.Errorf("the dex-less path should not enumerate targets, got %+v", a)
		}
	}
}

// TestLegalActionsLeavesOrdinaryMovesAlone: only self-switch moves are
// enumerated. Everything else stays a single entry.
func TestLegalActionsLeavesOrdinaryMovesAlone(t *testing.T) {
	d := loadDex(t)
	s := pivotBattle(t, d, "thunderbolt")

	n := 0
	for _, a := range LegalActionsDex(d, s, 0) {
		if a.Kind != ActionMove {
			continue
		}
		n++
		if a.SwitchTarget != nil {
			t.Errorf("an ordinary move should carry no target, got %+v", a)
		}
	}
	if n != 1 {
		t.Errorf("want a single entry for a non-pivot move, got %d", n)
	}
}

// TestActionAllowedToleratesTargets: the legality gate has to accept both the
// targeted and the untargeted form, and reject a target that names a slot the
// side can't send out. Every gate goes through this, so a controller that
// submits a pivot the enumeration didn't produce verbatim isn't refused.
func TestActionAllowedToleratesTargets(t *testing.T) {
	d := loadDex(t)
	s := pivotBattle(t, d, "volt-switch")

	cases := []struct {
		name string
		act  Action
		want bool
	}{
		{"untargeted", Action{Kind: ActionMove, Index: 0}, true},
		{"targeted at a live teammate", Action{Kind: ActionMove, Index: 0, SwitchTarget: slotPtr(2)}, true},
		{"targeted at the active", Action{Kind: ActionMove, Index: 0, SwitchTarget: slotPtr(0)}, false},
		{"targeted out of range", Action{Kind: ActionMove, Index: 0, SwitchTarget: slotPtr(9)}, false},
		{"a move slot that doesn't exist", Action{Kind: ActionMove, Index: 3}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ActionAllowed(nil, s, 0, c.act); got != c.want {
				t.Errorf("ActionAllowed = %v, want %v", got, c.want)
			}
		})
	}

	s.Sides[0].Team[2].Fainted = true
	if ActionAllowed(nil, s, 0, Action{Kind: ActionMove, Index: 0, SwitchTarget: slotPtr(2)}) {
		t.Error("a fainted target should not be allowed")
	}
}

// TestActionEqualFollowsTheTarget: Action stopped being comparable with ==
// when it grew a pointer field, and the replacement has to compare the value.
func TestActionEqualFollowsTheTarget(t *testing.T) {
	a := Action{Kind: ActionMove, Index: 0, SwitchTarget: slotPtr(2)}
	b := Action{Kind: ActionMove, Index: 0, SwitchTarget: slotPtr(2)} // different pointer
	if !a.Equal(b) {
		t.Error("equal targets should compare equal even through distinct pointers")
	}
	if a.Equal(Action{Kind: ActionMove, Index: 0, SwitchTarget: slotPtr(1)}) {
		t.Error("different targets should not compare equal")
	}
	if a.Equal(Action{Kind: ActionMove, Index: 0}) {
		t.Error("a targeted action should not equal an untargeted one")
	}
	if !(Action{Kind: ActionMove, Index: 0}).Equal(Action{Kind: ActionMove, Index: 0}) {
		t.Error("two untargeted actions should compare equal")
	}
}
