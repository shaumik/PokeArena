package engine

import (
	"testing"
)

// These tests target a recurring class of bug: a residual chip
// (leech seed, partial trap, curse, nightmare, status, recoil, hazards)
// takes a Pokémon to 0 HP and then *something else in the same path
// reads its volatiles*. faint() wipes Volatiles to the zero value, so
// any post-chip read crashes with a nil deref. The Whirlpool-induced
// gateway crash hit this on Leech Seed; the same shape can land on
// any chip residual, so each gets its own row.
//
// Each case sets up a state where:
//   - the target is at 1 HP
//   - the residual will kill it
//   - the residual logic must still complete and leave a consistent
//     state (Fainted=true, HP=0, no nil dereference in the rest of
//     the same function)

func TestResidualKillSafety_LeechSeed(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{26}, "B", []int{143}, 1)
	tgt := s.Active(1)
	tgt.HP = 1
	tgt.Volatiles.LeechSeed = &LeechSeedState{SourceSide: 0}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("applyLeechSeedResidual panicked on lethal tick: %v", r)
		}
	}()
	var log []LogLine
	applyLeechSeedResidual(s, 1, &log)

	if !tgt.Fainted || tgt.HP != 0 {
		t.Errorf("target should be fainted with HP=0; got fainted=%v hp=%d", tgt.Fainted, tgt.HP)
	}
}

func TestResidualKillSafety_PartialTrap(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{26}, "B", []int{143}, 1)
	tgt := s.Active(1)
	tgt.HP = 1
	tgt.Volatiles.PartialTrap = &PartialTrapState{Turns: 3, MoveName: "Whirlpool"}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("applyPartialTrapResidual panicked on lethal tick: %v", r)
		}
	}()
	var log []LogLine
	applyPartialTrapResidual(tgt, 1, &log)

	if !tgt.Fainted {
		t.Errorf("target should be fainted on lethal trap tick; got fainted=%v hp=%d", tgt.Fainted, tgt.HP)
	}
	if tgt.Volatiles.PartialTrap != nil {
		t.Errorf("PartialTrap should be cleared after lethal tick")
	}
}

func TestResidualKillSafety_Curse(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{26}, "B", []int{143}, 1)
	tgt := s.Active(0)
	tgt.HP = 1
	tgt.Volatiles.Curse = true

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("tickStatusVols panicked on lethal curse tick: %v", r)
		}
	}()
	var log []LogLine
	tickStatusVols(s, 0, &log)

	if !tgt.Fainted || tgt.HP != 0 {
		t.Fatalf("lethal Curse must set Fainted=true / HP=0; got fainted=%v hp=%d", tgt.Fainted, tgt.HP)
	}
}

func TestResidualKillSafety_Nightmare(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{26}, "B", []int{143}, 1)
	tgt := s.Active(0)
	tgt.HP = 1
	tgt.Status = StatusSleep
	tgt.Volatiles.Nightmare = true

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("tickStatusVols panicked on lethal nightmare tick: %v", r)
		}
	}()
	var log []LogLine
	tickStatusVols(s, 0, &log)

	if !tgt.Fainted || tgt.HP != 0 {
		t.Fatalf("lethal Nightmare must set Fainted=true / HP=0; got fainted=%v hp=%d", tgt.Fainted, tgt.HP)
	}
}

func TestResidualKillSafety_BurnPoisonToxic(t *testing.T) {
	for _, st := range []StatusCond{StatusBurn, StatusPoison, StatusToxic} {
		t.Run(string(st), func(t *testing.T) {
			d := loadDex(t)
			s, _ := NewBattle(d, "b", "A", []int{26}, "B", []int{143}, 1)
			tgt := s.Active(0)
			tgt.HP = 1
			tgt.Status = st
			tgt.ToxicCounter = 1 // matters only for toxic

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("applyStatusResidual panicked on lethal %s tick: %v", st, r)
				}
			}()
			var log []LogLine
			applyStatusResidual(tgt, 0, &log)

			if !tgt.Fainted {
				t.Errorf("lethal %s tick must set Fainted=true; got fainted=%v hp=%d", st, tgt.Fainted, tgt.HP)
			}
		})
	}
}

// TestResidualSweep_FullTurnNoPanic ResolveTurns a full turn where every
// chip residual is one tick away from killing. This exercises the
// composed end-of-turn ordering — if any tick reads a now-nil volatile
// after a sibling's faint, it crashes here.
func TestResidualSweep_FullTurnNoPanic(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "A", []int{26}, "B", []int{143}, 1)
	atk, def := s.Active(0), s.Active(1)
	atk.HP = 1
	atk.Status = StatusBurn
	atk.Volatiles.LeechSeed = &LeechSeedState{SourceSide: 1}
	atk.Volatiles.PartialTrap = &PartialTrapState{Turns: 3, MoveName: "Whirlpool"}
	atk.Volatiles.Curse = true
	def.HP = 1
	def.Status = StatusToxic
	def.ToxicCounter = 1

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ResolveTurn panicked with concurrent lethal residuals: %v", r)
		}
	}()
	actions := [2]Action{
		{Kind: ActionMove, Index: 0},
		{Kind: ActionMove, Index: 0},
	}
	_ = ResolveTurn(d, s, actions)
	if !atk.Fainted || !def.Fainted {
		t.Errorf("expected both actives to faint; atk.fainted=%v def.fainted=%v", atk.Fainted, def.Fainted)
	}
}
