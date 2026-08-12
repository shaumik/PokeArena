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

// The metric's soundness property, and the limit of it.
//
// A single oracle cannot rank skilled policies: scoring the same 72 games
// against an expectimax oracle and against the heuristic produces exactly
// opposite orderings of the three non-random policies, because each oracle
// rewards its own family (docs/decision-quality.md). So the ordering this test
// pins is the one that survives both judges — random is worst — and it
// deliberately does NOT assert anything about heuristic vs expectimax, which is
// judge-dependent and would be a false guarantee.
//
// Decisions are pooled over several teams and seeds because the property is a
// property of the *aggregate*, not of any one battle: on a single game the
// heuristic judge rates random above heuristic often enough to flake, which is
// itself worth knowing — a per-battle blunder rate is noise.
func TestScoreDecisions_RanksRandomWorstThanASkilledPolicy(t *testing.T) {
	if testing.Short() {
		t.Skip("runs oracles over many full games")
	}
	d := loadDex(t)
	lib, err := LoadTeamLibrary(libraryPath, d)
	if err != nil {
		t.Fatalf("load team library: %v", err)
	}
	teams := lib.Teams
	if len(teams) > 3 {
		teams = teams[:3]
	}
	seeds := SeedRange(2)

	// pooledBlunderRate plays every (team, seed) with the policy in the scored
	// seat and returns the blunder rate over all their decisions combined.
	pooledBlunderRate := func(t *testing.T, label string, oracle Oracle, build func() ai.Agent) float64 {
		t.Helper()
		decisions, blunders := 0, 0
		for _, team := range teams {
			for _, seed := range seeds {
				agents := [2]ai.Agent{build(), ai.NewHeuristicAgent(d)}
				picks := [2][]engine.TeamPick{team.Picks, team.Picks}
				_, turns, err := CaptureStored(d, agents, picks, seed, 0)
				if err != nil {
					t.Fatalf("%s / %s / %d: capture: %v", label, team.Name, seed, err)
				}
				scores, _, err := ScoreDecisions(d, oracle, 0, turns)
				if err != nil {
					t.Fatalf("%s / %s / %d: score: %v", label, team.Name, seed, err)
				}
				for _, s := range scores {
					decisions++
					if s.Blunder {
						blunders++
					}
				}
			}
		}
		if decisions == 0 {
			t.Fatalf("%s: no decisions scored", label)
		}
		rate := float64(blunders) / float64(decisions)
		t.Logf("%s: %d/%d decisions blundered (%.0f%%)", label, blunders, decisions, 100*rate)
		return rate
	}

	// Scored against the heuristic oracle only, and that is not a shortcut.
	// The expectimax oracle's ability to separate these two is entirely a
	// function of depth: on this same sample it gives random 35% and heuristic
	// 33% at depth 2 — a 2-point margin, i.e. it cannot tell a random player
	// from a competent one — against 43% vs 15% at depth 3. Depth 3 is ~50s
	// here, too slow to sit in the default suite, so the expectimax arm lives
	// in cmd/decision-sim. The depth requirement itself is documented in
	// docs/decision-quality.md; this test covers the cheap, wide-margin half.
	oracle := ai.NewHeuristicAgent(d)
	random := pooledBlunderRate(t, "random", oracle, func() ai.Agent { return ai.NewRandomAgent(20260811) })
	skilled := pooledBlunderRate(t, "heuristic", oracle, func() ai.Agent { return ai.NewHeuristicAgent(d) })
	if !(random > skilled) {
		t.Errorf("random blundered %.0f%% and heuristic %.0f%%: the metric does not rank a worse policy as worse",
			100*random, 100*skilled)
	}
}

// Each oracle rates its own policy's choices as near-perfect. That is the
// family bias stated as an executable fact rather than a caveat in prose: the
// same agent, on the same games, is a 2%-blunder player to one judge and a
// 21%-blunder player to the other. Any future change that makes a single
// oracle's blunder rate look like an absolute quality score should fail here.
func TestScoreDecisions_OracleFamilyBiasIsReal(t *testing.T) {
	if testing.Short() {
		t.Skip("runs an oracle over full games")
	}
	d := loadDex(t)
	lib, err := LoadTeamLibrary(libraryPath, d)
	if err != nil {
		t.Fatalf("load team library: %v", err)
	}
	picks := lib.Teams[0].Picks

	agents := [2]ai.Agent{ai.NewHeuristicAgent(d), ai.NewHeuristicAgent(d)}
	_, turns, err := CaptureStored(d, agents, [2][]engine.TeamPick{picks, picks}, 4242, 0)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	matchRate := func(t *testing.T, oracle Oracle) float64 {
		t.Helper()
		scores, _, err := ScoreDecisions(d, oracle, 0, turns)
		if err != nil {
			t.Fatalf("score: %v", err)
		}
		if len(scores) == 0 {
			t.Fatalf("no decisions scored")
		}
		agree := 0
		for _, s := range scores {
			if s.Agree {
				agree++
			}
		}
		return float64(agree) / float64(len(scores))
	}

	// The scored policy IS the heuristic, so the heuristic oracle should almost
	// always name the move it just watched being played, while the expectimax
	// oracle — same information, same view — frequently prefers another.
	own := matchRate(t, ai.NewHeuristicAgent(d))
	other := matchRate(t, ai.NewExpectimaxAgentFixed(d, 2))
	t.Logf("heuristic policy: %.0f%% match vs its own family, %.0f%% vs expectimax", 100*own, 100*other)

	if own <= other {
		t.Errorf("same-family match %.0f%% did not exceed cross-family %.0f%%; "+
			"the family-bias finding this metric is documented against no longer reproduces",
			100*own, 100*other)
	}
}
