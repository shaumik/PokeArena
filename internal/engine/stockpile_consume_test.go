package engine

import (
	"math"
	"testing"
)

// newStockpileBattle builds a Snorlax-vs-Snorlax 1v1 where the user holds only
// moveID and the foe Splashes. Snorlax is Normal, so Spit Up (Normal) connects
// neutrally instead of being walled.
func newStockpileBattle(t *testing.T, moveID string) *BattleState {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 10, MaxPP: 10}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	return s
}

// TestSpitUpScalesWithStockpile: Spit Up's power is 100× the stockpile count,
// so three stacks hit substantially harder than one.
func TestSpitUpScalesWithStockpile(t *testing.T) {
	d := loadDex(t)
	dmgFor := func(count int) int {
		s := newStockpileBattle(t, "spit-up")
		s.Active(0).Volatiles.Stockpile = &StockpileState{Count: count}
		before := s.Active(1).HP
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		return before - s.Active(1).HP
	}
	d1, d3 := dmgFor(1), dmgFor(3)
	if d1 <= 0 {
		t.Fatalf("Spit Up with 1 stack dealt no damage")
	}
	if d3 <= d1 {
		t.Errorf("Spit Up should scale with stockpile: 1 stack=%d, 3 stacks=%d", d1, d3)
	}
}

// TestSpitUpEmptiesStockpileAndRemovesBoosts: firing Spit Up clears the
// stockpile and strips the +Def/+SpD stages it had granted.
func TestSpitUpEmptiesStockpileAndRemovesBoosts(t *testing.T) {
	d := loadDex(t)
	s := newStockpileBattle(t, "spit-up")
	s.Active(0).Volatiles.Stockpile = &StockpileState{Count: 2, Def: 2, SpD: 2}
	s.Active(0).Stages.Def = 2
	s.Active(0).Stages.SpD = 2

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Active(0).Volatiles.Stockpile != nil {
		t.Errorf("Spit Up should empty the stockpile, got %+v", s.Active(0).Volatiles.Stockpile)
	}
	if def, spd := s.Active(0).Stages.Def, s.Active(0).Stages.SpD; def != 0 || spd != 0 {
		t.Errorf("Spit Up should strip stockpile boosts, got Def=%d SpD=%d", def, spd)
	}
}

// TestSpitUpFailsWithoutStockpile: no stacks means the move flat-out fails and
// deals no damage.
func TestSpitUpFailsWithoutStockpile(t *testing.T) {
	d := loadDex(t)
	s := newStockpileBattle(t, "spit-up")
	before := s.Active(1).HP
	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.Active(1).HP != before {
		t.Errorf("Spit Up with no stockpile should deal no damage (%d → %d)", before, s.Active(1).HP)
	}
	if !logHas(log, "But it failed") {
		t.Errorf("Spit Up with no stockpile should fail loudly; log: %v", logTexts(log))
	}
}

// TestSwallowHealsByStockpileCount: three stacks heal the user fully, then the
// stockpile and its boosts are gone.
func TestSwallowHealsByStockpileCount(t *testing.T) {
	d := loadDex(t)
	s := newStockpileBattle(t, "swallow")
	s.Active(0).HP = 1
	s.Active(0).Volatiles.Stockpile = &StockpileState{Count: 3, Def: 3, SpD: 3}
	s.Active(0).Stages.Def = 3
	s.Active(0).Stages.SpD = 3
	max := s.Active(0).MaxHP

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if got := s.Active(0).HP; got != max {
		t.Errorf("Swallow with 3 stacks should fully heal, got %d/%d", got, max)
	}
	if s.Active(0).Volatiles.Stockpile != nil {
		t.Error("Swallow should empty the stockpile")
	}
	if def, spd := s.Active(0).Stages.Def, s.Active(0).Stages.SpD; def != 0 || spd != 0 {
		t.Errorf("Swallow should strip stockpile boosts, got Def=%d SpD=%d", def, spd)
	}
}

// TestSwallowHalfHealAtTwoStacks: two stacks restore 1/2 max HP.
func TestSwallowHalfHealAtTwoStacks(t *testing.T) {
	d := loadDex(t)
	s := newStockpileBattle(t, "swallow")
	s.Active(0).HP = 1
	s.Active(0).Volatiles.Stockpile = &StockpileState{Count: 2, Def: 2, SpD: 2}
	s.Active(0).Stages.Def = 2
	s.Active(0).Stages.SpD = 2
	max := s.Active(0).MaxHP

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	want := 1 + int(math.Round(float64(max)*0.5))
	if want > max {
		want = max
	}
	if got := s.Active(0).HP; got != want {
		t.Errorf("Swallow with 2 stacks healed to %d, want %d (max %d)", got, want, max)
	}
}

// TestSwallowFailsWithoutStockpile: no stacks means no heal and a loud fail.
func TestSwallowFailsWithoutStockpile(t *testing.T) {
	d := loadDex(t)
	s := newStockpileBattle(t, "swallow")
	s.Active(0).HP = 1
	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.Active(0).HP != 1 {
		t.Errorf("Swallow with no stockpile should not heal, HP is now %d", s.Active(0).HP)
	}
	if !logHas(log, "But it failed") {
		t.Errorf("Swallow with no stockpile should fail loudly; log: %v", logTexts(log))
	}
}
