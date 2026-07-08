// Command bench-history reads the persisted run index and renders the
// benchmark's timeline: one row per run, each contestant's Elo over time, and
// the cumulative token spend. It is the read side of the results store —
// bench writes runs, this reads the history back.
//
// Nothing here re-runs a battle; it only reads runs/index.jsonl, so a timeline
// is cheap to draw and stays true to what was actually recorded.
//
// Usage:
//
//	bench-history                 # timeline from ./runs
//	bench-history -runs some/dir  # a different store
//	bench-history -agent llm      # focus one contestant's Elo + cost trend
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"pokearena/internal/eval"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[bench-history] ")

	runsDir := flag.String("runs", "runs", "run store directory (holds index.jsonl)")
	agent := flag.String("agent", "", "if set, show only this contestant's Elo and cost trend")
	flag.Parse()

	entries, err := eval.LoadIndex(*runsDir)
	if err != nil {
		log.Fatalf("%v", err)
	}
	if len(entries) == 0 {
		fmt.Printf("no runs recorded in %s yet — run bench to create some.\n", *runsDir)
		return
	}
	eval.SortIndexByTime(entries)

	if *agent != "" {
		printAgentTrend(entries, *agent)
		return
	}
	printTimeline(entries)
}

// printTimeline shows one row per run: when, which dataset/library, the ranked
// contestants by Elo, and the run's total cost.
func printTimeline(entries []eval.RunIndexEntry) {
	fmt.Printf("%d run(s):\n\n", len(entries))
	for _, e := range entries {
		ranked := append([]eval.IndexStanding(nil), e.Standings...)
		sort.Slice(ranked, func(i, j int) bool { return ranked[i].Elo > ranked[j].Elo })

		parts := make([]string, len(ranked))
		for i, s := range ranked {
			parts[i] = fmt.Sprintf("%s %.0f", s.Name, s.Elo)
		}
		cost := ""
		if e.TotalCostUSD > 0 {
			cost = fmt.Sprintf("  $%.4f", e.TotalCostUSD)
		}
		fmt.Printf("%s  [%s lib=%s data=%s g=%d]%s\n    %s\n",
			e.GeneratedAt, short(e.EngineRevision), e.TeamLibrary, e.DataSimVersion, e.Games, cost,
			strings.Join(parts, "   "))
	}
}

// printAgentTrend isolates one contestant across runs, so an Elo change (or
// cost change) as the model or code evolves is legible at a glance.
func printAgentTrend(entries []eval.RunIndexEntry, agent string) {
	fmt.Printf("trend for %q:\n\n", agent)
	fmt.Printf("  %-22s %8s %12s  %s\n", "when", "elo", "cost", "engine")
	found := false
	for _, e := range entries {
		for _, s := range e.Standings {
			if s.Name != agent {
				continue
			}
			found = true
			cost := "-"
			if s.CostUSD > 0 {
				cost = fmt.Sprintf("$%.4f", s.CostUSD)
			}
			fmt.Printf("  %-22s %8.0f %12s  %s\n", e.GeneratedAt, s.Elo, cost, short(e.EngineRevision))
		}
	}
	if !found {
		fmt.Fprintf(os.Stderr, "no runs contain a contestant named %q\n", agent)
		os.Exit(1)
	}
}

func short(rev string) string {
	if len(rev) > 10 {
		return rev[:10]
	}
	if rev == "" {
		return "?"
	}
	return rev
}
