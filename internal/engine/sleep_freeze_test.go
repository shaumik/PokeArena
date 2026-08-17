package engine

import "testing"

// A pivot must not cure sleep. switching.go used to zero SleepTurns on the way
// out, and canAct wakes anything at <= 0, so a sleeper woke the instant it
// returned. Found by the semifinal referee after both pilots exploited it.
func TestSwitchDoesNotCureSleep(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "A", []int{38, 59}, "B", []int{124, 3}, 5)
	if err != nil {
		t.Fatal(err)
	}
	a := s.Active(0)
	a.Status, a.SleepTurns = StatusSleep, 3
	// Out to the bench, then back in.
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
	benched := s.Sides[0].Team[0]
	if benched.Status != StatusSleep || benched.SleepTurns <= 0 {
		t.Fatalf("after switching out: status=%v turns=%d, want sleep with the counter intact",
			benched.Status, benched.SleepTurns)
	}
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 0}, {Kind: ActionMove, Index: 0}})
	if got := s.Sides[0].Team[0]; got.Status != StatusSleep {
		t.Errorf("switching back in woke it: status=%v turns=%d", got.Status, got.SleepTurns)
	}
}

// Harsh sunlight has forbidden freeze since Gen 2.
func TestSunBlocksFreeze(t *testing.T) {
	d := loadDex(t)
	frozen := func(sun bool) int {
		n := 0
		for seed := uint64(1); seed <= 600; seed++ {
			s, err := NewBattle(d, "b", "A", []int{121, 6}, "B", []int{71, 3}, seed)
			if err != nil {
				t.Fatal(err)
			}
			a, b := s.Active(0), s.Active(1)
			a.Ability, b.Ability = AbilityNone, AbilityNone
			a.Item, b.Item = ItemNone, ItemNone
			a.Moves = []MoveSlot{{MoveID: "ice-beam", PP: 10, MaxPP: 10}}
			b.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			b.HP = b.MaxHP
			if sun {
				s.Weather = &WeatherState{Kind: WeatherSun, TurnsLeft: 8}
			}
			ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
			if s.Sides[1].Team[0].Status == StatusFreeze {
				n++
			}
		}
		return n
	}
	if got := frozen(true); got != 0 {
		t.Errorf("Ice Beam froze a target in harsh sunlight %d/600 times, want 0", got)
	}
	if got := frozen(false); got == 0 {
		t.Error("Ice Beam never froze anything without sun — the probe is not exercising the path")
	}
}
