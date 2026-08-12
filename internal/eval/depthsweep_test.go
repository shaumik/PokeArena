package eval

import (
	"fmt"
	"os"
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
)

// TestDepthSweep reproduces docs/benchmark.md §6's only load-bearing
// measurement: expectimax vs the heuristic at depths 1-3, 6 teams x 20 seeds x
// 2 orientations = 240 games per depth, exactly the published methodology.
//
// Gated behind POKEARENA_DEPTH_SWEEP=1 rather than testing.Short because it
// takes ~22 minutes — far too slow for the suite, but committed so the numbers
// under §6 can be re-derived instead of taken on trust. It prints the table; it
// asserts nothing, because the point is to produce figures for a human to read
// into the document, not to pin them (they legitimately move when the baseline
// or the team library changes, which is the whole reason this exists).
//
//	POKEARENA_DEPTH_SWEEP=1 go test ./internal/eval -run TestDepthSweep -v -timeout 60m
func TestDepthSweep(t *testing.T) {
	if os.Getenv("POKEARENA_DEPTH_SWEEP") == "" {
		t.Skip("set POKEARENA_DEPTH_SWEEP=1 to run the ~22-minute sweep")
	}
	d := loadDex(t)
	lib, err := LoadTeamLibrary(libraryPath, d)
	if err != nil {
		t.Fatal(err)
	}
	seeds := SeedRange(20)
	heur := Contestant{Name: "heuristic", New: func(uint64) ai.Agent { return ai.NewHeuristicAgent(d) }}

	for _, depth := range []int{1, 2, 3} {
		dp := depth
		exp := Contestant{Name: fmt.Sprintf("expectimax-d%d", dp),
			New: func(uint64) ai.Agent { return ai.NewExpectimaxAgentFixed(d, dp) }}
		wins, games := 0, 0
		for _, team := range lib.Teams {
			picks := [2][]engine.TeamPick{team.Picks, team.Picks}
			mr, err := RunMatch(d, exp, heur, team.Name, picks, seeds, 0)
			if err != nil {
				t.Fatal(err)
			}
			wins += mr.AWins
			games += mr.AWins + mr.BWins + mr.Draws
		}
		lo, hi := WilsonInterval(float64(wins), games, 1.96)
		fmt.Printf("depth %d: %d/%d = %.1f%%  95%% CI [%.1f%%, %.1f%%]\n",
			dp, wins, games, 100*float64(wins)/float64(games), 100*lo, 100*hi)
	}
}
