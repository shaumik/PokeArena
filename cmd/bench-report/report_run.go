package main

// Reporting for a bench-run output directory: pooled win rate with Wilson
// intervals per entrant, plus head-to-head-free standings.
//
// This reads only the per-game result files bench-run wrote. It deliberately
// does not need Postgres, the gateway, or any API access, so the numbers behind
// a published table can be recomputed from a directory that fits in a git
// commit.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"pokearena/internal/eval"
)

// runGame mirrors the record bench-run writes per game. Duplicated rather than
// shared so the two commands can be read independently; the json tags are the
// contract between them.
type runGame struct {
	Label    string `json:"label"`
	Entrant  string `json:"entrant"`
	Harness  string `json:"harness"`
	Model    string `json:"model"`
	Team     string `json:"team"`
	BattleID string `json:"battle_id"`
	Winner   int    `json:"winner"`
	Status   string `json:"status"`
	Seconds  int    `json:"seconds"`
}

// EntrantStanding is one row of the run's results table.
type EntrantStanding struct {
	Entrant    string  `json:"entrant"`
	Harness    string  `json:"harness"`
	Model      string  `json:"model"`
	Games      int     `json:"games"`
	Wins       int     `json:"wins"`
	Losses     int     `json:"losses"`
	Unfinished int     `json:"unfinished"`
	WinRate    float64 `json:"win_rate"`
	CILow      float64 `json:"ci_low"`
	CIHigh     float64 `json:"ci_high"`
	MedianSecs int     `json:"median_seconds"`
}

// LoadRun reads every per-game result in a bench-run output directory.
func LoadRun(dir string) ([]runGame, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []runGame
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || name == "config.json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var g runGame
		if err := json.Unmarshal(b, &g); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// Standings folds games into one row per entrant.
//
// Unfinished games (winner == -1: the agent hung, timed out, or abandoned) are
// counted and reported but excluded from the win-rate denominator. Counting
// them as losses would conflate "played badly" with "the harness fell over",
// which are different findings — and a harness that fails to finish games is
// itself a result worth seeing in its own column rather than hidden inside a
// win rate.
func Standings(games []runGame) []EntrantStanding {
	type acc struct {
		harness, model      string
		wins, losses, unfin int
		secs                []int
	}
	byID := map[string]*acc{}
	for _, g := range games {
		a := byID[g.Entrant]
		if a == nil {
			a = &acc{harness: g.Harness, model: g.Model}
			byID[g.Entrant] = a
		}
		switch g.Winner {
		case 0:
			a.wins++
		case 1:
			a.losses++
		default:
			a.unfin++
		}
		if g.Seconds > 0 {
			a.secs = append(a.secs, g.Seconds)
		}
	}

	out := make([]EntrantStanding, 0, len(byID))
	for id, a := range byID {
		decided := a.wins + a.losses
		s := EntrantStanding{
			Entrant: id, Harness: a.harness, Model: a.model,
			Games: decided + a.unfin, Wins: a.wins, Losses: a.losses,
			Unfinished: a.unfin, MedianSecs: medianInt(a.secs),
		}
		if decided > 0 {
			s.WinRate = float64(a.wins) / float64(decided)
			s.CILow, s.CIHigh = eval.WilsonInterval(float64(a.wins), decided, 1.96)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].WinRate != out[j].WinRate {
			return out[i].WinRate > out[j].WinRate
		}
		return out[i].Entrant < out[j].Entrant
	})
	return out
}

func medianInt(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// FormatStandings renders the table. The confidence interval is printed beside
// every win rate rather than offered as an option, because a win rate from a
// small run is the single easiest number in this project to over-read.
func FormatStandings(rows []EntrantStanding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-24s %6s %5s %5s %6s %8s %-18s %7s\n",
		"entrant", "games", "W", "L", "unfin", "win%", "95% CI", "med s")
	for _, r := range rows {
		ci := fmt.Sprintf("[%.0f%%, %.0f%%]", 100*r.CILow, 100*r.CIHigh)
		fmt.Fprintf(&b, "%-24s %6d %5d %5d %6d %7.1f%% %-18s %7d\n",
			r.Entrant, r.Games, r.Wins, r.Losses, r.Unfinished, 100*r.WinRate, ci, r.MedianSecs)
	}
	return b.String()
}
