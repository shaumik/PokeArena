package engine

import "testing"

// newWaterSpoutBattle builds a Blastoise-vs-Snorlax 1v1 where the user holds
// only Water Spout and the foe Splashes. Snorlax is Normal, so Water Spout
// (Water) connects neutrally instead of being resisted.
func newWaterSpoutBattle(t *testing.T) *BattleState {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{9}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "water-spout", PP: 10, MaxPP: 10}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	return s
}

// TestWaterSpoutScalesWithHP: Water Spout's base power is floor(150 × curHP ÷
// maxHP), so a full-HP user hits far harder than a half-HP one, which in turn
// beats a near-fainted user. The RNG state lives in s and is consumed along the
// same path regardless of power, so equal seeds make the comparison clean.
func TestWaterSpoutScalesWithHP(t *testing.T) {
	d := loadDex(t)
	dmgAtHP := func(hp int) int {
		s := newWaterSpoutBattle(t)
		s.Active(0).HP = hp
		before := s.Active(1).HP
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		return before - s.Active(1).HP
	}
	max := newWaterSpoutBattle(t).Active(0).MaxHP

	full := dmgAtHP(max)
	half := dmgAtHP(max / 2)
	if full <= 0 {
		t.Fatalf("Water Spout at full HP dealt no damage")
	}
	if half >= full {
		t.Errorf("Water Spout should weaken as HP drops: full=%d, half=%d", full, half)
	}
}

// TestWaterSpoutFloorsAtOnePower: a near-fainted user (1 HP) still does chip
// damage — the floor(…)||1 clamp prevents a zero-power, damageless hit.
func TestWaterSpoutFloorsAtOnePower(t *testing.T) {
	d := loadDex(t)
	s := newWaterSpoutBattle(t)
	s.Active(0).HP = 1
	before := s.Active(1).HP
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if dealt := before - s.Active(1).HP; dealt <= 0 {
		t.Errorf("Water Spout at 1 HP should still deal floored chip damage, got %d", dealt)
	}
}
