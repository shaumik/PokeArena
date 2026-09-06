// Command team-validate measures whether the competitive team library is
// balanced. It cross-matches every pair of teams with a single neutral pilot on
// both sides, so what's measured is team quality rather than policy, and
// reports each team's win rate plus any dominant team, weak team, or lopsided
// matchup.
//
// This is the "prove it, don't assert it" companion to the library: legality is
// guaranteed at load, but balance can only be measured by play.
//
// Usage:
//
//	team-validate                 # default library, heuristic pilot
//	team-validate -games 40       # more games for tighter intervals
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/eval"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[team-validate] ")

	dataDir := flag.String("data", "data", "dataset directory")
	libPath := flag.String("teams", "data/benchmark-teams.json", "team library to validate")
	games := flag.Int("games", 20, "seeds per team pairing (each played in both orientations)")
	depth := flag.Int("depth", 0, "if >0, use fixed-depth expectimax as the pilot instead of the heuristic")
	flag.Parse()

	dex, err := domain.LoadDex(*dataDir, "team-validate")
	if err != nil {
		log.Fatalf("load dex: %v", err)
	}
	lib, err := eval.LoadTeamLibrary(*libPath, dex)
	if err != nil {
		log.Fatalf("%v", err)
	}

	pilot := eval.Contestant{Name: "heuristic", New: func(uint64) ai.Agent { return ai.NewHeuristicAgent(dex) }}
	if *depth > 0 {
		d := *depth
		pilot = eval.Contestant{Name: fmt.Sprintf("expectimax-d%d", d), New: func(uint64) ai.Agent { return ai.NewExpectimaxAgentFixed(dex, d) }}
	}

	log.Printf("library %q: %d teams, pilot=%s, %d seeds x2 orientations", lib.Version, len(lib.Teams), pilot.Name, *games)

	res, err := eval.TeamTournament(dex, lib.Teams, pilot, eval.SeedRange(*games), eval.Budget(0))
	if err != nil {
		log.Fatalf("tournament: %v", err)
	}

	fmt.Fprintf(os.Stdout, "\nteam standings (piloted by %s, level %d):\n", res.Pilot, engine.Level)
	fmt.Fprintf(os.Stdout, "  %-10s %-8s %-18s %-8s %s\n", "team", "winrate", "95% CI", "avgturns", "W-L-D")
	for _, t := range res.Teams {
		fmt.Fprintf(os.Stdout, "  %-10s %6.1f%%  [%5.1f%%, %5.1f%%]  %6.1f    %d-%d-%d (n=%d)\n",
			t.Name, 100*t.WinRate, 100*t.CILow, 100*t.CIHigh, t.AvgTurns, t.Wins, t.Losses, t.Draws, t.Games)
	}

	fmt.Fprintln(os.Stdout, "\nmatchups (A win% vs B):")
	for _, m := range res.Matchups {
		fmt.Fprintf(os.Stdout, "  %-10s vs %-10s  %5.1f%%  (n=%d)\n", m.A, m.B, 100*m.AWinRate, m.Games)
	}

	// Balance is advisory, not a legality failure: a mirror-match battle
	// benchmark cancels cross-team strength anyway (both sides get the same
	// team), and a diverse library deliberately includes offensive and
	// defensive styles that won't be 50-50 against each other. These flags
	// matter most for the Build track, where cross-team balance IS the metric.
	// So the tool reports and exits 0 — it informs, it doesn't gate.
	flags := eval.AssessBalance(res)
	fmt.Fprintln(os.Stdout, "")
	if len(flags) == 0 {
		fmt.Fprintln(os.Stdout, "BALANCED: no dominant/weak team, no lopsided matchup, no stomps.")
		return
	}
	fmt.Fprintf(os.Stdout, "%d balance flag(s) — advisory (see note in source; cross-team balance is a Build-track concern):\n", len(flags))
	for _, f := range flags {
		fmt.Fprintf(os.Stdout, "  - %s\n", f)
	}
}
