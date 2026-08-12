package eval

import "testing"

// TestAggregateByModel checks the roll-up arithmetic and the heavy-tail
// handling: a single missed-lethal (regret 1e6) must not move the median or
// blow up the winsorized mean, and models must sort by blunder rate ascending.
func TestAggregateByModel(t *testing.T) {
	sc := func(regret float64, agree bool) DecisionScore {
		return DecisionScore{Regret: regret, Agree: agree, Blunder: regret > BlunderThreshold}
	}
	results := []BattleResult{
		// "clean": 4 decisions, 1 blunder, regrets {0,100,200,1e6}, won.
		{Model: "clean", Won: true, Scores: []DecisionScore{
			sc(0, true), sc(100, false), sc(200, false), sc(1_000_000, false),
		}},
		// "sloppy": 2 decisions, both blunders, regrets {500,700}, lost.
		{Model: "sloppy", Won: false, Scores: []DecisionScore{
			sc(500, false), sc(700, false),
		}},
	}

	stats := AggregateByModel(results, 3000)
	if len(stats) != 2 {
		t.Fatalf("want 2 models, got %d", len(stats))
	}
	// clean has the lower blunder rate (1/4 vs 2/2) so it sorts first.
	if stats[0].Model != "clean" || stats[1].Model != "sloppy" {
		t.Fatalf("sort order = [%s %s], want [clean sloppy]", stats[0].Model, stats[1].Model)
	}

	c := stats[0]
	if c.Games != 1 || c.Wins != 1 || c.WinRate != 1 {
		t.Errorf("clean games/wins/winrate = %d/%d/%.2f, want 1/1/1.00", c.Games, c.Wins, c.WinRate)
	}
	if c.Decisions != 4 || c.Blunders != 1 {
		t.Errorf("clean decisions/blunders = %d/%d, want 4/1", c.Decisions, c.Blunders)
	}
	if c.BlunderRate != 0.25 {
		t.Errorf("clean blunder rate = %.3f, want 0.25", c.BlunderRate)
	}
	if c.MatchRate != 0.25 {
		t.Errorf("clean match rate = %.3f, want 0.25", c.MatchRate)
	}
	// Median of {0,100,200,1e6} = (100+200)/2 = 150 — the missed-lethal is ignored.
	if c.MedianRegret != 150 {
		t.Errorf("clean median regret = %.0f, want 150 (heavy tail must not move it)", c.MedianRegret)
	}
	// Winsorized mean at cap 3000: (0+100+200+3000)/4 = 825, not ~250k.
	if c.MeanRegret != 825 {
		t.Errorf("clean mean regret = %.0f, want 825 (winsorized at 3000)", c.MeanRegret)
	}

	s := stats[1]
	if s.BlunderRate != 1 {
		t.Errorf("sloppy blunder rate = %.3f, want 1.0", s.BlunderRate)
	}
	if s.MedianRegret != 600 {
		t.Errorf("sloppy median regret = %.0f, want 600", s.MedianRegret)
	}
	if s.WinRate != 0 {
		t.Errorf("sloppy win rate = %.2f, want 0", s.WinRate)
	}
}
