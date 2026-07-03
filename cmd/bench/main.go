// Command bench runs the PokéArena battle benchmark: a round-robin of agents
// over a fixed seed set, writing a JSONL trace and a human-readable summary.
//
// Every number it produces is reproducible from the command line alone — the
// same agents, team, depth, and game count yield byte-identical games (the
// stochastic agents are seeded from the game seed, and expectimax runs in its
// fixed-depth mode). That reproducibility is the point: anyone can re-run the
// exact benchmark and get the exact traces.
//
// Usage:
//
//	bench -agents random,heuristic,expectimax -games 20 -team 6,9,26 -out run.jsonl
//
// Each pairing plays -games seeds in both side orientations (so 2×games games
// per pairing), which cancels first-mover advantage from the win rate.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/eval"

	// Blank import: engine's init() populates internal/specs so LoadDex can
	// validate move/ability vocabularies. eval pulls in engine transitively,
	// but keep it explicit so this binary never silently loses validation.
	_ "pokearena/internal/engine"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[bench] ")

	var (
		dataDir  = flag.String("data", "data", "dataset directory")
		agentCSV = flag.String("agents", "random,heuristic,expectimax", "comma-separated agents to enter (round-robin)")
		games    = flag.Int("games", 20, "seeds per pairing (each played in both side orientations)")
		teamCSV  = flag.String("team", "6,9,26", "comma-separated dex numbers; mirrored to both sides")
		depth    = flag.Int("depth", 2, "fixed search depth for the expectimax agent")
		budgetMs = flag.Int("budget-ms", 0, "per-decision time budget in ms (0 = none; for LLM agents)")
		outPath  = flag.String("out", "", "JSONL output path (default: stdout)")
	)
	flag.Parse()

	dex, err := domain.LoadDex(*dataDir, "bench")
	if err != nil {
		log.Fatalf("load dex from %s: %v", *dataDir, err)
	}

	dexNos, err := parseTeam(*teamCSV)
	if err != nil {
		log.Fatalf("bad -team: %v", err)
	}
	teams, err := eval.MirrorPicks(dex, dexNos)
	if err != nil {
		log.Fatalf("build team: %v", err)
	}

	names := splitCSV(*agentCSV)
	if len(names) < 2 {
		log.Fatalf("need at least 2 agents, got %d", len(names))
	}
	contestants := make([]eval.Contestant, len(names))
	for i, n := range names {
		c, err := makeContestant(n, dex, *depth)
		if err != nil {
			log.Fatalf("%v", err)
		}
		contestants[i] = c
	}

	out := os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatalf("create -out %s: %v", *outPath, err)
		}
		defer f.Close()
		out = f
	}

	seeds := eval.SeedRange(*games)
	budget := eval.Budget(time.Duration(*budgetMs) * time.Millisecond)

	log.Printf("round-robin: %d agents, %d pairings, %d seeds x2 orientations = %d games/pairing",
		len(contestants), nPairs(len(contestants)), *games, 2**games)

	var matches []eval.MatchResult
	var summaries []string
	for i := 0; i < len(contestants); i++ {
		for j := i + 1; j < len(contestants); j++ {
			mr, err := eval.RunMatch(dex, contestants[i], contestants[j], teams, seeds, budget)
			if err != nil {
				log.Fatalf("match %s vs %s: %v", contestants[i].Name, contestants[j].Name, err)
			}
			if err := eval.WriteMatch(out, mr); err != nil {
				log.Fatalf("write trace: %v", err)
			}
			matches = append(matches, mr)
			total := mr.AWins + mr.BWins + mr.Draws
			summaries = append(summaries, fmt.Sprintf("  %-12s %3d - %-3d %-12s  (draws %d, n=%d)",
				mr.A, mr.AWins, mr.BWins, mr.B, mr.Draws, total))
		}
	}

	fmt.Fprintln(os.Stderr, "\nhead-to-head (wins):")
	for _, s := range summaries {
		fmt.Fprintln(os.Stderr, s)
	}

	fmt.Fprintln(os.Stderr, "\nstandings (Elo, win rate with Wilson 95% CI):")
	fmt.Fprintf(os.Stderr, "  %-12s %6s  %-8s %-18s %s\n", "agent", "elo", "winrate", "95% CI", "W-L-D")
	for _, r := range eval.Standings(matches) {
		fmt.Fprintf(os.Stderr, "  %-12s %6.0f  %6.1f%%  [%5.1f%%, %5.1f%%]  %d-%d-%d (n=%d)\n",
			r.Name, r.Elo, 100*r.WinRate, 100*r.CILow, 100*r.CIHigh, r.Wins, r.Losses, r.Draws, r.Games)
	}

	if *outPath != "" {
		log.Printf("wrote JSONL trace to %s", *outPath)
	}
}

// makeContestant maps an agent name to a fresh-per-game factory. Random is
// seeded from the game seed for reproducibility; heuristic and expectimax are
// deterministic and ignore it. Expectimax uses the fixed-depth (reproducible)
// mode so its choices don't depend on machine speed.
func makeContestant(name string, dex *domain.Dex, depth int) (eval.Contestant, error) {
	switch name {
	case "random":
		return eval.Contestant{Name: "random", New: func(seed uint64) ai.Agent { return ai.NewRandomAgent(seed) }}, nil
	case "heuristic":
		return eval.Contestant{Name: "heuristic", New: func(uint64) ai.Agent { return ai.NewHeuristicAgent(dex) }}, nil
	case "expectimax":
		return eval.Contestant{Name: "expectimax", New: func(uint64) ai.Agent { return ai.NewExpectimaxAgentFixed(dex, depth) }}, nil
	default:
		return eval.Contestant{}, fmt.Errorf("unknown agent %q (known: random, heuristic, expectimax)", name)
	}
}

func parseTeam(csv string) ([]int, error) {
	parts := splitCSV(csv)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty team")
	}
	team := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("dex number %q: %w", p, err)
		}
		team[i] = n
	}
	return team, nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func nPairs(n int) int { return n * (n - 1) / 2 }
