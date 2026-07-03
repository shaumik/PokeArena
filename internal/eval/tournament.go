package eval

import (
	"fmt"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// A team tournament measures team QUALITY, not policy. It cross-matches every
// pair of library teams with a single neutral pilot on BOTH sides, so the only
// variable is the teams themselves — the "fixed battler pilots everyone's
// teams" isolation from issue #101. It answers the question the battle
// benchmark can't: are these teams actually balanced, or is one a free win and
// some matchup a hard counter?
//
// This is also the seed of the Build track: swap the fixed library for
// model-built teams and the same harness scores build skill.

// TeamStanding is one team's record across all its cross-team matchups under
// the neutral pilot.
type TeamStanding struct {
	Name     string
	Wins     int
	Losses   int
	Draws    int
	Games    int
	WinRate  float64
	CILow    float64
	CIHigh   float64
	AvgTurns float64
}

// MatchupResult is one team-vs-team result under the neutral pilot, from team
// A's perspective.
type MatchupResult struct {
	A        string
	B        string
	AWins    int
	BWins    int
	Draws    int
	Games    int
	AWinRate float64
}

// TournamentResult is the full balance picture: the pilot used, per-team
// standings (sorted by win rate), and every matchup.
type TournamentResult struct {
	Pilot    string
	Teams    []TeamStanding
	Matchups []MatchupResult
}

// TeamTournament cross-matches every pair of teams with pilot on both sides,
// over the seed set in both orientations (so seat advantage cancels). Games
// pit different teams against each other — not a mirror — which is exactly what
// exposes an unbalanced matchup.
func TeamTournament(dex *domain.Dex, teams []NamedTeam, pilot Contestant, seeds []uint64, budget Budget) (TournamentResult, error) {
	res := TournamentResult{Pilot: pilot.Name}

	type rec struct {
		wins, losses, draws, games, turns int
	}
	agg := make(map[string]*rec, len(teams))
	for _, t := range teams {
		agg[t.Name] = &rec{}
	}

	for i := 0; i < len(teams); i++ {
		for j := i + 1; j < len(teams); j++ {
			a, b := teams[i], teams[j]
			mu := MatchupResult{A: a.Name, B: b.Name}

			for _, seed := range seeds {
				// Both orientations: A on side 0 then B on side 0.
				for _, o := range [2][2]NamedTeam{{a, b}, {b, a}} {
					s0, s1 := o[0], o[1]
					sideTeams := [2][]engine.TeamPick{s0.Picks, s1.Picks}
					agents := [2]ai.Agent{pilot.New(seed), pilot.New(seed ^ sideSalt)}
					game, err := RunGame(dex, agents, sideTeams, seed, budget)
					if err != nil {
						return res, fmt.Errorf("%s vs %s: %w", s0.Name, s1.Name, err)
					}

					var winner string
					switch game.Winner {
					case 0:
						winner = s0.Name
					case 1:
						winner = s1.Name
					default:
						winner = ""
					}

					for _, name := range []string{a.Name, b.Name} {
						agg[name].games++
						agg[name].turns += game.Turns
					}
					switch winner {
					case a.Name:
						agg[a.Name].wins++
						agg[b.Name].losses++
						mu.AWins++
					case b.Name:
						agg[b.Name].wins++
						agg[a.Name].losses++
						mu.BWins++
					default:
						agg[a.Name].draws++
						agg[b.Name].draws++
						mu.Draws++
					}
					mu.Games++
				}
			}
			if mu.Games > 0 {
				mu.AWinRate = (float64(mu.AWins) + 0.5*float64(mu.Draws)) / float64(mu.Games)
			}
			res.Matchups = append(res.Matchups, mu)
		}
	}

	for _, t := range teams {
		r := agg[t.Name]
		success := float64(r.wins) + 0.5*float64(r.draws)
		lo, hi := WilsonInterval(success, r.games, z95)
		wr, avgTurns := 0.0, 0.0
		if r.games > 0 {
			wr = success / float64(r.games)
			avgTurns = float64(r.turns) / float64(r.games)
		}
		res.Teams = append(res.Teams, TeamStanding{
			Name: t.Name, Wins: r.wins, Losses: r.losses, Draws: r.draws, Games: r.games,
			WinRate: wr, CILow: lo, CIHigh: hi, AvgTurns: avgTurns,
		})
	}
	sortTeamsByWinRate(res.Teams)
	return res, nil
}

func sortTeamsByWinRate(ts []TeamStanding) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].WinRate > ts[j-1].WinRate; j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

// Balance thresholds. These are advisory guidelines for a "healthy" library,
// not hard rules — a benchmark can knowingly include a lopsided matchup, but it
// should be a deliberate choice, so the harness surfaces it rather than hiding
// it.
const (
	// A team whose overall win rate strays outside this band is dominant or
	// weak relative to the field.
	balancedLow  = 0.35
	balancedHigh = 0.65
	// A single matchup outside this band is a near-hard-counter.
	lopsidedLow  = 0.20
	lopsidedHigh = 0.80
	// Games shorter than this on average are stomps, not contests.
	minAvgTurns = 8.0
)

// AssessBalance returns human-readable flags for anything outside the advisory
// bands. Empty means the library looks balanced.
func AssessBalance(r TournamentResult) []string {
	var flags []string
	for _, t := range r.Teams {
		switch {
		case t.WinRate > balancedHigh:
			flags = append(flags, fmt.Sprintf("DOMINANT: %s wins %.0f%% overall (>%.0f%%)", t.Name, 100*t.WinRate, 100*balancedHigh))
		case t.WinRate < balancedLow:
			flags = append(flags, fmt.Sprintf("WEAK: %s wins %.0f%% overall (<%.0f%%)", t.Name, 100*t.WinRate, 100*balancedLow))
		}
		if t.AvgTurns > 0 && t.AvgTurns < minAvgTurns {
			flags = append(flags, fmt.Sprintf("STOMPS: %s games average %.1f turns (<%.0f)", t.Name, t.AvgTurns, minAvgTurns))
		}
	}
	for _, m := range r.Matchups {
		if m.AWinRate > lopsidedHigh {
			flags = append(flags, fmt.Sprintf("LOPSIDED: %s beats %s %.0f%%", m.A, m.B, 100*m.AWinRate))
		} else if m.AWinRate < lopsidedLow {
			flags = append(flags, fmt.Sprintf("LOPSIDED: %s beats %s %.0f%%", m.B, m.A, 100*(1-m.AWinRate)))
		}
	}
	return flags
}
