package ai

import (
	"context"
	"testing"
	"time"

	"pokearena/internal/engine"
)

// TestExpectimaxFixed_Deterministic: the fixed-depth agent must return the
// identical action for the identical View every time — the property the
// benchmark's ground-truth pilot depends on. Depth is a free parameter here;
// reproducibility is depth-independent, so the tests use depth 1 to stay fast
// (depth-2+ search costs seconds per decision).
func TestExpectimaxFixed_Deterministic(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9, 26}, "B", []int{3, 65, 143}, 7)
	v := MakeView(s, 0)

	a := NewExpectimaxAgentFixed(d, 1)
	first, err := a.Decide(context.Background(), v)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	for i := 0; i < 5; i++ {
		got, err := a.Decide(context.Background(), v)
		if err != nil {
			t.Fatalf("decide %d: %v", i, err)
		}
		if got != first {
			t.Fatalf("non-deterministic choice: run %d gave %+v, want %+v", i, got, first)
		}
	}
}

// TestExpectimaxFixed_IgnoresDeadline: an already-expired context must not
// change the fixed-depth agent's answer. The time-budgeted agent would bail
// early and fall back to a shallower (or first-action) choice; the fixed-depth
// agent searches to full depth regardless, which is what makes it machine-
// independent.
func TestExpectimaxFixed_IgnoresDeadline(t *testing.T) {
	d := loadDex(t)
	s, _ := engine.NewBattle(d, "b", "R", []int{6, 9, 26}, "B", []int{3, 65, 143}, 7)
	v := MakeView(s, 0)

	a := NewExpectimaxAgentFixed(d, 1)
	want, _ := a.Decide(context.Background(), v)

	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()
	got, err := a.Decide(expired, v)
	if err != nil {
		t.Fatalf("decide with expired ctx: %v", err)
	}
	if got != want {
		t.Fatalf("expired deadline changed the choice: got %+v, want %+v", got, want)
	}
}
