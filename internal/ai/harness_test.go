package ai

import (
	"testing"

	"pokearena/internal/engine"
)

// TestNewHeuristicHarness checks the live-battle opponent: its primary strategy
// is the heuristic (our strongest programmatic bot), and it returns a legal
// action for a real battle state rather than a zero value.
func TestNewHeuristicHarness(t *testing.T) {
	dex := loadDex(t)

	h := NewHeuristicHarness(dex, 0)
	if h.Name() != "heuristic" {
		t.Fatalf("NewHeuristicHarness Name()=%q, want \"heuristic\"", h.Name())
	}
	st, err := engine.NewBattle(dex, "b", "R", []int{6, 9, 26}, "B", []int{3, 65, 143}, 7)
	if err != nil {
		t.Fatalf("NewBattle: %v", err)
	}
	v := MakeView(st, 0)
	act := h.DecideView(v)
	legal := LegalActions(v)
	if len(legal) == 0 {
		t.Fatal("no legal actions from the opening view — test fixture is wrong")
	}
	found := false
	for _, la := range legal {
		if la == act {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("heuristic harness returned %+v, not among the legal actions", act)
	}
}
