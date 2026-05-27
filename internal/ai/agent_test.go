package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

func TestNewHarness_UnknownDifficultyIsAnError(t *testing.T) {
	d := loadDex(t)
	if _, err := NewHarness(d, "GODMODE", 100*time.Millisecond); !errors.Is(err, ErrUnknownDifficulty) {
		t.Fatalf("expected ErrUnknownDifficulty, got %v", err)
	}
}

func loadDex(t *testing.T) *domain.Dex {
	t.Helper()
	d, err := domain.LoadDex("../../data", "test")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	return d
}

func TestRandomAgentReturnsLegal(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9}, "B", []int{3, 65}, 1)
	a := NewRandomAgent(42)
	for i := 0; i < 50; i++ {
		v := MakeView(s, 0)
		act, _ := a.Decide(context.Background(), v)
		if !isLegal(v, act) {
			t.Fatalf("random agent produced illegal action %+v", act)
		}
	}
}

func TestHeuristicTakesKnockout(t *testing.T) {
	d := loadDex(t)
	// Charizard vs Vileplume — drop the foe to a sliver of HP.
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{45}, 1)
	s.Sides[1].Team[0].HP = 8
	v := MakeView(s, 0)

	act, _ := NewHeuristicAgent(d).Decide(context.Background(), v)
	if act.Kind != engine.ActionMove || act.Index < 0 {
		t.Fatalf("expected a real move, got %+v", act)
	}
	m := d.Moves[v.Self.Team[0].Moves[act.Index].MoveID]
	if dmg := engine.ExpectedDamage(d, &v.Self.Team[0], &v.Foe, m); dmg < v.Foe.HP {
		t.Fatalf("heuristic skipped the KO: chose %s (%d dmg vs %d HP)", m.Name, dmg, v.Foe.HP)
	}
}

func TestExpectimaxReturnsLegalWithinBudget(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9, 26}, "B", []int{3, 65, 143}, 5)
	v := MakeView(s, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	act, err := NewExpectimaxAgent(d).Decide(ctx, v)
	if err != nil {
		t.Fatalf("expectimax error: %v", err)
	}
	if !isLegal(v, act) {
		t.Fatalf("expectimax produced illegal action %+v", act)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("expectimax overran its budget: %v", elapsed)
	}
}

// panicAgent and slowAgent are fault injectors for the harness.
type panicAgent struct{}

func (panicAgent) Name() string { return "panic" }
func (panicAgent) Decide(context.Context, View) (engine.Action, error) {
	panic("boom")
}

type slowAgent struct{}

func (slowAgent) Name() string { return "slow" }
func (slowAgent) Decide(ctx context.Context, v View) (engine.Action, error) {
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
	}
	return engine.Action{Kind: engine.ActionMove, Index: 0}, nil
}

func TestHarnessFallsBackOnPanic(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9}, "B", []int{3, 65}, 1)
	h := &Harness{primary: panicAgent{}, fallback: NewHeuristicAgent(d), budget: 200 * time.Millisecond}
	act := h.Decide(s, 0)
	if !isLegal(MakeView(s, 0), act) {
		t.Fatalf("harness did not recover from a panicking agent: %+v", act)
	}
}

func TestHarnessFallsBackOnTimeout(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9}, "B", []int{3, 65}, 1)
	h := &Harness{primary: slowAgent{}, fallback: NewHeuristicAgent(d), budget: 50 * time.Millisecond}
	start := time.Now()
	act := h.Decide(s, 0)
	if time.Since(start) > 2*time.Second {
		t.Fatal("harness did not enforce the time budget")
	}
	if !isLegal(MakeView(s, 0), act) {
		t.Fatalf("harness fallback produced an illegal action: %+v", act)
	}
}

func TestMakeView_RedactsFoeFog(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	foe := &s.Sides[1].Team[0]

	// Burn a single PP off slot 1: it has been "used" and should be revealed.
	if len(foe.Moves) < 2 {
		t.Fatalf("test fixture needs at least 2 moves on the foe; got %d", len(foe.Moves))
	}
	foe.Moves[1].PP--
	usedMoveID := foe.Moves[1].MoveID

	// Pile on hidden status state so we can confirm it's all zeroed.
	foe.Status = engine.StatusToxic
	foe.ToxicCounter = 7
	foe.SleepTurns = 3
	foe.Volatiles.Confusion = &engine.ConfusionState{Turns: 4}
	foe.HP = 137 // odd value, off a 1%-bucket grid

	v := MakeView(s, 0)

	// Used move stays visible; the others are blanked but the slot count remains.
	if got := len(v.Foe.Moves); got != len(foe.Moves) {
		t.Fatalf("redaction must preserve move-slot count: got %d, want %d", got, len(foe.Moves))
	}
	revealed := 0
	for _, m := range v.Foe.Moves {
		if m.MoveID != "" {
			revealed++
		}
	}
	if revealed != 1 {
		t.Fatalf("expected exactly one revealed move, got %d", revealed)
	}
	if v.Foe.Moves[1].MoveID != usedMoveID {
		t.Fatalf("revealed slot 1 should carry the used move %q, got %q", usedMoveID, v.Foe.Moves[1].MoveID)
	}

	// HP redacted to the nearest 1% bucket; original 137 must not leak.
	if v.Foe.HP == 137 {
		t.Errorf("exact HP %d leaked through redaction", v.Foe.HP)
	}

	// Status visible, clocks hidden.
	if v.Foe.Status != engine.StatusToxic {
		t.Errorf("status condition was redacted: got %q, want %q", v.Foe.Status, engine.StatusToxic)
	}
	if v.Foe.ToxicCounter != 0 {
		t.Errorf("ToxicCounter must be zeroed; got %d", v.Foe.ToxicCounter)
	}
	if v.Foe.SleepTurns != 0 {
		t.Errorf("SleepTurns must be zeroed; got %d", v.Foe.SleepTurns)
	}
	if v.Foe.Volatiles.Confusion == nil {
		t.Errorf("confusion presence must remain visible; got nil")
	} else if v.Foe.Volatiles.Confusion.Turns != 0 {
		t.Errorf("confusion turn count must be hidden; got %d", v.Foe.Volatiles.Confusion.Turns)
	}

	// Source state must not be mutated by view construction.
	if foe.HP != 137 || foe.ToxicCounter != 7 {
		t.Fatalf("MakeView mutated the source BattleState: HP=%d, ToxicCounter=%d", foe.HP, foe.ToxicCounter)
	}
}

func TestMakeView_LiveFoeNeverFakeFaints(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6}, "B", []int{3}, 1)
	foe := &s.Sides[1].Team[0]
	// 1 HP out of a few hundred — strictly less than the 1%-bucket size.
	foe.HP = 1
	v := MakeView(s, 0)
	if v.Foe.HP <= 0 {
		t.Fatalf("a live foe (HP=1) must never round to 0 in the view; got %d", v.Foe.HP)
	}
}

func TestAIBattleTerminates(t *testing.T) {
	d := loadDex(t)
	h1, err := NewHarness(d, "hard", 150*time.Millisecond)
	if err != nil {
		t.Fatalf("NewHarness(hard): %v", err)
	}
	h2, err := NewHarness(d, "easy", 150*time.Millisecond)
	if err != nil {
		t.Fatalf("NewHarness(easy): %v", err)
	}
	h := [2]*Harness{h1, h2}
	s, _ := engine.NewBattle(d, "b", "Red", []int{6, 9, 26}, "Blue", []int{3, 65, 143}, 7)

	for guard := 0; !s.Ended(); guard++ {
		if guard > 2000 {
			t.Fatal("AI-vs-AI battle failed to terminate")
		}
		switch s.Phase {
		case engine.PhaseChoosing:
			engine.ResolveTurn(d, s, [2]engine.Action{h[0].Decide(s, 0), h[1].Decide(s, 1)})
		case engine.PhaseReplace:
			var sw [2]*engine.Action
			for i := 0; i < 2; i++ {
				if s.Replace[i] {
					a := h[i].Decide(s, i)
					sw[i] = &a
				}
			}
			engine.ResolveReplace(s, sw)
		}
	}
	if s.Winner < 0 || s.Winner > 2 {
		t.Fatalf("invalid winner %d", s.Winner)
	}
}
