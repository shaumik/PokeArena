package eval

import (
	"encoding/json"
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
)

// The capture is only worth anything if the recovery can invert it. This plays
// a real library-v2 game and asserts that ScoreDecisions re-derives, for every
// scored turn, the exact action the agent actually chose — which is the
// property the whole metric rests on and the one a hand-built fixture cannot
// demonstrate.
func TestCaptureStored_RoundTripsThroughActionRecovery(t *testing.T) {
	d := loadDex(t)
	lib, err := LoadTeamLibrary(libraryPath, d)
	if err != nil {
		t.Fatalf("load team library: %v", err)
	}
	picks := lib.Teams[0].Picks

	// Heuristic vs heuristic: a real, varied line (unlike a move#0 mirror) that
	// is still deterministic, so this test is reproducible.
	agents := [2]ai.Agent{ai.NewHeuristicAgent(d), ai.NewHeuristicAgent(d)}
	res, turns, err := CaptureStored(d, agents, [2][]engine.TeamPick{picks, picks}, 20260811, 0)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if res.Winner < 0 {
		t.Fatalf("game did not finish: winner=%d", res.Winner)
	}
	if len(turns) < 5 {
		t.Fatalf("captured %d turns, want a real game", len(turns))
	}

	// Every stored state must be choosing or ended — never a mid-turn replace.
	// A leaked replace state would shift the pre/post pairing ScoreDecisions
	// relies on, mis-attributing every decision after the first KO.
	for i, st := range turns {
		var s engine.BattleState
		if err := json.Unmarshal(st.State, &s); err != nil {
			t.Fatalf("turn %d: unmarshal: %v", i, err)
		}
		if s.Phase != engine.PhaseChoosing && s.Phase != engine.PhaseEnded {
			t.Errorf("turn %d stored in phase %q, want choosing or ended", i, s.Phase)
		}
	}

	// The stub oracle scores every legal action, so a decision is skipped only
	// when recovery genuinely failed — which makes `skipped` a direct measure of
	// round-trip fidelity rather than of oracle opinion.
	scores, skipped, err := ScoreDecisions(d, allActionsOracle{}, 0, turns)
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if len(scores) == 0 {
		t.Fatal("recovered no decisions at all")
	}
	if skipped > 0 {
		t.Errorf("%d of %d turns failed to recover; capture and recovery disagree",
			skipped, skipped+len(scores))
	}

	// Re-running the same seed must produce byte-identical states, or the
	// metric is not reproducible and no published number from it can be checked.
	_, again, err := CaptureStored(d, [2]ai.Agent{ai.NewHeuristicAgent(d), ai.NewHeuristicAgent(d)},
		[2][]engine.TeamPick{picks, picks}, 20260811, 0)
	if err != nil {
		t.Fatalf("recapture: %v", err)
	}
	if len(again) != len(turns) {
		t.Fatalf("replay produced %d turns, first run %d", len(again), len(turns))
	}
	for i := range turns {
		if string(again[i].State) != string(turns[i].State) {
			t.Fatalf("turn %d differs between identical-seed runs", i)
		}
	}
}

// allActionsOracle values every legal action equally. It exists so a test can
// separate "recovery failed" from "the oracle declined to score this turn":
// ScoreDecisions skips both, and only the first is a bug in the capture.
type allActionsOracle struct{}

func (allActionsOracle) ScoreActions(v ai.View) []ai.ActionValue {
	acts := ai.LegalActions(v)
	if len(acts) <= 1 {
		return nil // mirror expectimax's "no free choice" contract
	}
	out := make([]ai.ActionValue, len(acts))
	for i, a := range acts {
		out[i] = ai.ActionValue{Action: a}
	}
	return out
}

// The metric's soundness property: a better policy must blunder less. It was
// only ever validated on data integrity (does recovery reproduce the stored
// state), then pointed at four models whose true ordering nobody knows — so a
// subtly inverted metric would have gone unnoticed. This pins the ordering
// against policies whose relative strength is not in question.
//
// Deliberately coarse: random vs heuristic is the widest gap available, scored
// by a shallow oracle on one team, because the property under test is the
// direction of the ranking, not its precise values. The full sweep across four
// policies lives in cmd/decision-sim and is documented in docs/decision-quality.md.
func TestScoreDecisions_RanksAWorsePolicyAsBlunderingMore(t *testing.T) {
	if testing.Short() {
		t.Skip("runs an expectimax oracle over a full game")
	}
	d := loadDex(t)
	lib, err := LoadTeamLibrary(libraryPath, d)
	if err != nil {
		t.Fatalf("load team library: %v", err)
	}
	picks := lib.Teams[0].Picks
	oracle := ai.NewExpectimaxAgentFixed(d, 2)

	blunderRate := func(t *testing.T, label string, scored ai.Agent) float64 {
		t.Helper()
		agents := [2]ai.Agent{scored, ai.NewHeuristicAgent(d)}
		_, turns, err := CaptureStored(d, agents, [2][]engine.TeamPick{picks, picks}, 4242, 0)
		if err != nil {
			t.Fatalf("%s: capture: %v", label, err)
		}
		scores, _, err := ScoreDecisions(d, oracle, 0, turns)
		if err != nil {
			t.Fatalf("%s: score: %v", label, err)
		}
		if len(scores) == 0 {
			t.Fatalf("%s: no decisions scored", label)
		}
		blunders := 0
		for _, s := range scores {
			if s.Blunder {
				blunders++
			}
		}
		rate := float64(blunders) / float64(len(scores))
		t.Logf("%s: %d/%d decisions blundered (%.0f%%)", label, blunders, len(scores), 100*rate)
		return rate
	}

	random := blunderRate(t, "random", ai.NewRandomAgent(20260811))
	heuristic := blunderRate(t, "heuristic", ai.NewHeuristicAgent(d))

	if !(random > heuristic) {
		t.Errorf("random blundered %.0f%% and heuristic %.0f%%: the metric does not rank a worse policy as worse",
			100*random, 100*heuristic)
	}
}
