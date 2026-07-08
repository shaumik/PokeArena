package eval

import (
	"math"
	"sort"
)

// This file turns raw match outcomes into the benchmark's headline numbers:
// win rate with a confidence interval, and an Elo rating. Both are pure
// functions of the match results, so a standings table is reproducible from
// the same runs — no hidden state, no game order dependence.

// Z95 is the standard normal quantile for a 95% two-sided interval. Exported so
// every surface that draws a win-rate CI (the CLI standings, the HTML board)
// uses the one constant and can't diverge on rounding.
const Z95 = 1.959963984540054

// AgentStanding is one row of the leaderboard: aggregate record, win rate with
// a Wilson 95% interval, and an Elo rating fit across the whole round-robin.
type AgentStanding struct {
	Name    string
	Wins    int
	Losses  int
	Draws   int
	Games   int
	WinRate float64 // (wins + draws/2) / games
	CILow   float64 // Wilson 95% lower bound on win rate
	CIHigh  float64 // Wilson 95% upper bound
	Elo     float64
}

// WilsonInterval returns the Wilson score interval for a binomial proportion:
// successes out of n, at the confidence implied by z (use Z95 for 95%). Unlike
// the naive normal interval it stays inside [0,1] and behaves sanely at the
// extremes and for small n — which is why it's the right tool for win rates off
// a few hundred games rather than millions.
func WilsonInterval(successes float64, n int, z float64) (lo, hi float64) {
	if n == 0 {
		return 0, 0
	}
	nf := float64(n)
	phat := successes / nf
	z2 := z * z
	denom := 1 + z2/nf
	center := (phat + z2/(2*nf)) / denom
	margin := (z * math.Sqrt(phat*(1-phat)/nf+z2/(4*nf*nf))) / denom
	lo = center - margin
	hi = center + margin
	if lo < 0 {
		lo = 0
	}
	if hi > 1 {
		hi = 1
	}
	return lo, hi
}

// Standings aggregates a set of head-to-head matches into a sorted leaderboard:
// per-agent record, win rate with a Wilson 95% interval, and Elo. Rows are
// sorted by Elo descending (win rate breaks ties).
func Standings(matches []MatchResult) []AgentStanding {
	type rec struct{ wins, losses, draws, games int }
	agg := map[string]*rec{}
	// scores[i][j] = points i took off j (win=1, draw=0.5); pairGames[i][j] = games.
	scores := map[string]map[string]float64{}
	pairGames := map[string]map[string]int{}

	touch := func(name string) {
		if agg[name] == nil {
			agg[name] = &rec{}
			scores[name] = map[string]float64{}
			pairGames[name] = map[string]int{}
		}
	}

	for _, m := range matches {
		touch(m.A)
		touch(m.B)
		total := m.AWins + m.BWins + m.Draws

		agg[m.A].wins += m.AWins
		agg[m.A].losses += m.BWins
		agg[m.A].draws += m.Draws
		agg[m.A].games += total

		agg[m.B].wins += m.BWins
		agg[m.B].losses += m.AWins
		agg[m.B].draws += m.Draws
		agg[m.B].games += total

		scores[m.A][m.B] += float64(m.AWins) + 0.5*float64(m.Draws)
		scores[m.B][m.A] += float64(m.BWins) + 0.5*float64(m.Draws)
		pairGames[m.A][m.B] += total
		pairGames[m.B][m.A] += total
	}

	names := make([]string, 0, len(agg))
	for n := range agg {
		names = append(names, n)
	}
	sort.Strings(names)

	elo := bradleyTerryElo(names, scores, pairGames)

	out := make([]AgentStanding, 0, len(names))
	for _, n := range names {
		r := agg[n]
		// success counts a draw as half a win, matching the WinRate point
		// estimate. Feeding a fractional success into a Wilson interval is an
		// approximation — the interval assumes 0/1 Bernoulli trials and a draw is
		// a 0.5 — so with a high draw rate the width is slightly off. It's the
		// standard game-rating convention and adequate at benchmark n; the
		// assumption lives here, in the one place the CI is computed.
		success := float64(r.wins) + 0.5*float64(r.draws)
		lo, hi := WilsonInterval(success, r.games, Z95)
		wr := 0.0
		if r.games > 0 {
			wr = success / float64(r.games)
		}
		out = append(out, AgentStanding{
			Name: n, Wins: r.wins, Losses: r.losses, Draws: r.draws, Games: r.games,
			WinRate: wr, CILow: lo, CIHigh: hi, Elo: elo[n],
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Elo != out[j].Elo {
			return out[i].Elo > out[j].Elo
		}
		return out[i].WinRate > out[j].WinRate
	})
	return out
}

// bradleyTerryElo fits Bradley-Terry strengths by the standard MM (minorize-
// maximize) iteration and maps them onto the Elo scale (400*log10, mean 1500).
//
// Bradley-Terry is chosen over sequential K-factor Elo deliberately: the fit is
// a function of the win/loss counts alone, not the order games were played, so
// the ratings are reproducible. A small number of virtual games against a
// neutral anchor regularizes the fit, which keeps an agent that wins (or loses)
// every game at a finite, bounded rating instead of diverging.
func bradleyTerryElo(names []string, scores map[string]map[string]float64, pairGames map[string]map[string]int) map[string]float64 {
	const (
		anchorStrength = 1.0 // strength of the virtual neutral opponent
		virtualGames   = 2.0 // per agent: one virtual win, one virtual loss vs the anchor
		maxIter        = 10000
		eps            = 1e-12
	)

	p := make(map[string]float64, len(names))
	for _, n := range names {
		p[n] = 1.0
	}
	// Total points each agent took (real games + one virtual win vs the anchor).
	wins := make(map[string]float64, len(names))
	for _, n := range names {
		w := virtualGames / 2.0
		for _, s := range scores[n] {
			w += s
		}
		wins[n] = w
	}

	for iter := 0; iter < maxIter; iter++ {
		next := make(map[string]float64, len(names))
		for _, i := range names {
			denom := virtualGames / (p[i] + anchorStrength)
			for _, j := range names {
				if i == j {
					continue
				}
				if g := pairGames[i][j]; g > 0 {
					denom += float64(g) / (p[i] + p[j])
				}
			}
			if denom == 0 {
				next[i] = p[i]
				continue
			}
			next[i] = wins[i] / denom
		}
		// Normalize by the geometric mean so the scale can't drift, and
		// measure convergence in log space.
		var logSum float64
		for _, n := range names {
			if next[n] <= 0 {
				next[n] = eps
			}
			logSum += math.Log(next[n])
		}
		gm := math.Exp(logSum / float64(len(names)))
		var maxDelta float64
		for _, n := range names {
			next[n] /= gm
			if d := math.Abs(math.Log(next[n]) - math.Log(p[n])); d > maxDelta {
				maxDelta = d
			}
			p[n] = next[n]
		}
		if maxDelta < eps {
			break
		}
	}

	// Map to Elo. After geometric-mean normalization the mean of log10(p) is 0,
	// so centering the scale at 1500 needs no extra shift.
	elo := make(map[string]float64, len(names))
	for _, n := range names {
		elo[n] = 1500 + 400*math.Log10(p[n])
	}
	return elo
}
