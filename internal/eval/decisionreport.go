package eval

import "sort"

// Decision-quality rolls up from per-decision scores (see decisionquality.go) to
// a per-model table: across every recovered free choice a model made, how often
// did it blunder, and by how much did it typically fall short of the oracle. The
// headline is the blunder rate; median regret is the tie-breaker that separates
// two models with the same blunder count but different typical mistake sizes.

// BattleResult is one scored live battle attributed to a model: the model's
// recovered decisions and whether the model (side 0, the "Agent" seat) won.
type BattleResult struct {
	Model  string
	Won    bool
	Scores []DecisionScore
}

// ModelStats is the decision-quality summary for one model, aggregated over all
// its scored battles.
type ModelStats struct {
	Model        string  `json:"model"`
	Games        int     `json:"games"`
	Wins         int     `json:"wins"`
	WinRate      float64 `json:"win_rate"`     // Wins / Games
	Decisions    int     `json:"decisions"`    // free choices scored
	Blunders     int     `json:"blunders"`     // decisions with Regret > BlunderThreshold
	BlunderRate  float64 `json:"blunder_rate"` // Blunders / Decisions — the headline
	MatchRate    float64 `json:"match_rate"`   // fraction agreeing with the oracle's top pick
	MedianRegret float64 `json:"median_regret"`
	MeanRegret   float64 `json:"mean_regret"` // capped at regretCap so missed-lethals don't dominate
}

// AggregateByModel folds scored battles into one ModelStats per model, sorted by
// blunder rate ascending (best decision-maker first).
//
// Regret is reported two ways because its distribution is heavy-tailed: a missed
// lethal scores regret ≈ winValue (~1e6), which would swamp a raw mean. The
// median ignores those tails by construction; the mean is winsorized at
// regretCap (each regret clipped to the cap before averaging) so it still
// reflects a run's missed lethals without letting one of them set the scale.
func AggregateByModel(results []BattleResult, regretCap float64) []ModelStats {
	type acc struct {
		games, wins, decisions, blunders, agrees int
		regrets                                  []float64
		cappedSum                                float64
	}
	byModel := map[string]*acc{}
	var order []string
	for _, r := range results {
		a := byModel[r.Model]
		if a == nil {
			a = &acc{}
			byModel[r.Model] = a
			order = append(order, r.Model)
		}
		a.games++
		if r.Won {
			a.wins++
		}
		for _, s := range r.Scores {
			a.decisions++
			if s.Blunder {
				a.blunders++
			}
			if s.Agree {
				a.agrees++
			}
			a.regrets = append(a.regrets, s.Regret)
			capped := s.Regret
			if capped > regretCap {
				capped = regretCap
			}
			a.cappedSum += capped
		}
	}

	stats := make([]ModelStats, 0, len(order))
	for _, m := range order {
		a := byModel[m]
		st := ModelStats{Model: m, Games: a.games, Wins: a.wins, Decisions: a.decisions, Blunders: a.blunders}
		if a.games > 0 {
			st.WinRate = float64(a.wins) / float64(a.games)
		}
		if a.decisions > 0 {
			n := float64(a.decisions)
			st.BlunderRate = float64(a.blunders) / n
			st.MatchRate = float64(a.agrees) / n
			st.MeanRegret = a.cappedSum / n
			st.MedianRegret = median(a.regrets)
		}
		stats = append(stats, st)
	}
	sort.SliceStable(stats, func(i, j int) bool {
		return stats[i].BlunderRate < stats[j].BlunderRate
	})
	return stats
}

// median returns the middle value of xs (mean of the two middles when the count
// is even), or 0 for an empty slice. It sorts a copy so the caller's slice keeps
// its original order.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}
