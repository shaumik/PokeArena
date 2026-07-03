package eval

import (
	"reflect"
	"testing"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
)

func loadDex(t *testing.T) *domain.Dex {
	t.Helper()
	d, err := domain.LoadDex("../../data", "test")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	return d
}

// mirrorTeams is a fixed roster used on both sides — the variance-controlled
// mirror-match setup the benchmark relies on.
var mirrorTeams = [2][]int{{6, 9, 26}, {6, 9, 26}}

// TestRunGame_Deterministic is the load-bearing property of the whole
// benchmark: the same agents, teams, and seed must produce a byte-identical
// result. RandomAgent constructed from the same seed is deterministic, so two
// independent runs must agree on winner, turn count, and the full decision
// trace (including every state fingerprint).
func TestRunGame_Deterministic(t *testing.T) {
	d := loadDex(t)

	run := func() GameResult {
		agents := [2]ai.Agent{ai.NewRandomAgent(1), ai.NewRandomAgent(2)}
		res, err := RunGame(d, agents, mirrorTeams, 7, 0)
		if err != nil {
			t.Fatalf("RunGame: %v", err)
		}
		return res
	}

	a, b := run(), run()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("non-deterministic result:\n first: winner=%d turns=%d decisions=%d\nsecond: winner=%d turns=%d decisions=%d",
			a.Winner, a.Turns, len(a.Decisions), b.Winner, b.Turns, len(b.Decisions))
	}
}

// TestRunGame_Terminates checks a game actually ends with a real winner and a
// non-empty trace, across a range of seeds. Heuristic and random are both fast
// and deterministic, so the sweep is cheap.
func TestRunGame_Terminates(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(0); seed < 5; seed++ {
		agents := [2]ai.Agent{ai.NewHeuristicAgent(d), ai.NewRandomAgent(seed)}
		res, err := RunGame(d, agents, mirrorTeams, seed, 0)
		if err != nil {
			t.Fatalf("seed %d: RunGame: %v", seed, err)
		}
		if res.Winner != 0 && res.Winner != 1 && res.Winner != 2 {
			t.Fatalf("seed %d: bad winner %d", seed, res.Winner)
		}
		if len(res.Decisions) == 0 {
			t.Fatalf("seed %d: empty decision trace", seed)
		}
		if res.Turns == 0 {
			t.Fatalf("seed %d: zero turns", seed)
		}
	}
}

// TestRunGame_Expectimax smoke-tests the flagship baseline through the driver.
// Expectimax iterative-deepens under a wall-clock deadline, so it needs a real
// per-decision budget; without one it searches to full depth every turn. NOTE:
// because depth-reached depends on machine speed, expectimax under a time
// budget is NOT reproducible — the benchmark's ground-truth pilot will need a
// fixed-depth mode before it can anchor per-move regret (Step 3).
func TestRunGame_Expectimax(t *testing.T) {
	d := loadDex(t)
	agents := [2]ai.Agent{ai.NewExpectimaxAgent(d), ai.NewHeuristicAgent(d)}
	res, err := RunGame(d, agents, mirrorTeams, 1, Budget(50*time.Millisecond))
	if err != nil {
		t.Fatalf("RunGame: %v", err)
	}
	if res.Winner != 0 && res.Winner != 1 && res.Winner != 2 {
		t.Fatalf("bad winner %d", res.Winner)
	}
}

// TestRunGame_TraceIsLegal asserts every recorded action was legal at its
// decision point (agents that propose illegal actions are corrected and
// flagged, so a non-fallback decision must appear in its own legal set).
func TestRunGame_TraceIsLegal(t *testing.T) {
	d := loadDex(t)
	agents := [2]ai.Agent{ai.NewHeuristicAgent(d), ai.NewRandomAgent(9)}
	res, err := RunGame(d, agents, mirrorTeams, 3, 0)
	if err != nil {
		t.Fatalf("RunGame: %v", err)
	}
	for i, dec := range res.Decisions {
		if dec.Fallback {
			continue
		}
		if !isLegal(dec.Legal, dec.Action) {
			t.Fatalf("decision %d: action %+v not in legal set %+v", i, dec.Action, dec.Legal)
		}
	}
}
