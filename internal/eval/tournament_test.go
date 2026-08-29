package eval

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/ai"
)

// TestTeamTournament_Bookkeeping runs a small real tournament and checks the
// aggregate invariants: every pair produces games, per-team wins+losses+draws
// sum to games, and win rates are in range.
func TestTeamTournament_Bookkeeping(t *testing.T) {
	d := loadDex(t)
	lib, err := LoadTeamLibrary(libraryPath, d)
	if err != nil {
		t.Fatalf("load library: %v", err)
	}
	teams := lib.Teams[:3] // keep it fast

	pilot := Contestant{Name: "heuristic", New: func(uint64) ai.Agent { return ai.NewHeuristicAgent(d) }}
	res, err := TeamTournament(d, teams, pilot, SeedRange(2), 0)
	if err != nil {
		t.Fatalf("TeamTournament: %v", err)
	}

	wantMatchups := 3 // C(3,2)
	if len(res.Matchups) != wantMatchups {
		t.Fatalf("got %d matchups, want %d", len(res.Matchups), wantMatchups)
	}
	if len(res.Teams) != 3 {
		t.Fatalf("got %d team standings, want 3", len(res.Teams))
	}
	for _, ts := range res.Teams {
		if ts.Wins+ts.Losses+ts.Draws != ts.Games {
			t.Fatalf("%s: %d+%d+%d != %d games", ts.Name, ts.Wins, ts.Losses, ts.Draws, ts.Games)
		}
		if ts.WinRate < 0 || ts.WinRate > 1 {
			t.Fatalf("%s: win rate %.3f out of range", ts.Name, ts.WinRate)
		}
		if ts.AvgTurns <= 0 {
			t.Fatalf("%s: avg turns %.1f should be positive", ts.Name, ts.AvgTurns)
		}
	}
	// Standings sorted by win rate descending.
	for i := 1; i < len(res.Teams); i++ {
		if res.Teams[i].WinRate > res.Teams[i-1].WinRate {
			t.Fatalf("standings not sorted by win rate")
		}
	}
}

// TestAssessBalance_FlagsImbalance: a synthetic result with a dominant team and
// a lopsided matchup must be flagged; a balanced one must be clean.
func TestAssessBalance_FlagsImbalance(t *testing.T) {
	bad := TournamentResult{
		Teams: []TeamStanding{
			{Name: "OP", WinRate: 0.90, AvgTurns: 20},
			{Name: "Weak", WinRate: 0.10, AvgTurns: 20},
		},
		Matchups: []MatchupResult{
			{A: "OP", B: "Weak", AWinRate: 0.95},
		},
	}
	flags := AssessBalance(bad)
	if len(flags) < 3 {
		t.Fatalf("expected dominant + weak + lopsided flags, got %v", flags)
	}

	good := TournamentResult{
		Teams: []TeamStanding{
			{Name: "A", WinRate: 0.52, AvgTurns: 22},
			{Name: "B", WinRate: 0.48, AvgTurns: 22},
		},
		Matchups: []MatchupResult{
			{A: "A", B: "B", AWinRate: 0.55},
		},
	}
	if flags := AssessBalance(good); len(flags) != 0 {
		t.Fatalf("balanced result should have no flags, got %v", flags)
	}
}
