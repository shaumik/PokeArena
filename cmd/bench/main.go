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

	"pokearena/internal/agentloop"
	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/eval"
	"pokearena/internal/llm"

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
		agentCSV = flag.String("agents", "random,heuristic,expectimax", "comma-separated baseline agents (random, heuristic, expectimax)")
		llmCSV   = flag.String("llm", "", "comma-separated LLM contestants as label=model or model (Anthropic; needs ANTHROPIC_API_KEY)")
		games    = flag.Int("games", 20, "seeds per pairing per team (each played in both side orientations)")
		libPath  = flag.String("teams", "data/benchmark-teams.json", "competitive team library; every team is mirror-matched and results aggregated")
		teamCSV  = flag.String("team", "", "ad-hoc override: comma-separated dex numbers, mirrored to both sides (bypasses -teams)")
		depth    = flag.Int("depth", 2, "fixed search depth for the expectimax agent")
		budgetMs = flag.Int("budget-ms", 0, "per-decision time budget in ms (0 = none; recommended for LLM agents)")
		outPath  = flag.String("out", "", "JSONL output path (default: stdout)")
	)
	flag.Parse()

	dex, err := domain.LoadDex(*dataDir, "bench")
	if err != nil {
		log.Fatalf("load dex from %s: %v", *dataDir, err)
	}

	benchTeams, err := loadBenchTeams(dex, *teamCSV, *libPath)
	if err != nil {
		log.Fatalf("%v", err)
	}

	var contestants []eval.Contestant
	for _, n := range splitCSV(*agentCSV) {
		c, err := makeContestant(n, dex, *depth)
		if err != nil {
			log.Fatalf("%v", err)
		}
		contestants = append(contestants, c)
	}
	for _, c := range llmContestants(splitCSV(*llmCSV), dex) {
		contestants = append(contestants, c)
	}
	if len(contestants) < 2 {
		log.Fatalf("need at least 2 contestants (via -agents and/or -llm), got %d", len(contestants))
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

	perTeamGames := 2 * *games * nPairs(len(contestants))
	log.Printf("round-robin: %d contestants, %d pairings x %d teams, %d seeds x2 orientations = %d games/team, %d total",
		len(contestants), nPairs(len(contestants)), len(benchTeams), *games, perTeamGames, perTeamGames*len(benchTeams))

	var matches []eval.MatchResult
	for _, team := range benchTeams {
		mirror := team.Mirror()
		for i := 0; i < len(contestants); i++ {
			for j := i + 1; j < len(contestants); j++ {
				mr, err := eval.RunMatch(dex, contestants[i], contestants[j], team.Name, mirror, seeds, budget)
				if err != nil {
					log.Fatalf("match %s vs %s on %s: %v", contestants[i].Name, contestants[j].Name, team.Name, err)
				}
				if err := eval.WriteMatch(out, mr); err != nil {
					log.Fatalf("write trace: %v", err)
				}
				matches = append(matches, mr)
			}
		}
	}

	// Per-team Elo surfaces whether the ranking holds across teams or is an
	// artifact of one — the reason the benchmark runs across a library rather
	// than a single team.
	if len(benchTeams) > 1 {
		fmt.Fprintln(os.Stderr, "\nper-team Elo:")
		for _, team := range benchTeams {
			var tm []eval.MatchResult
			for _, m := range matches {
				if m.Team == team.Name {
					tm = append(tm, m)
				}
			}
			parts := []string{}
			for _, r := range eval.Standings(tm) {
				parts = append(parts, fmt.Sprintf("%s %.0f", r.Name, r.Elo))
			}
			fmt.Fprintf(os.Stderr, "  %-10s %s\n", team.Name, strings.Join(parts, "  "))
		}
	}

	fmt.Fprintln(os.Stderr, "\noverall standings (Elo, win rate with Wilson 95% CI):")
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

// loadBenchTeams resolves the teams to run on. An explicit -team (dex numbers)
// is an ad-hoc single-team override; otherwise the curated, legality-checked
// library at libPath is used and every team is mirror-matched.
func loadBenchTeams(dex *domain.Dex, teamCSV, libPath string) ([]eval.NamedTeam, error) {
	if teamCSV != "" {
		dexNos, err := parseTeam(teamCSV)
		if err != nil {
			return nil, fmt.Errorf("bad -team: %w", err)
		}
		picks, err := eval.PicksFromDex(dex, dexNos)
		if err != nil {
			return nil, fmt.Errorf("build ad-hoc team: %w", err)
		}
		return []eval.NamedTeam{{Name: "adhoc", Picks: picks}}, nil
	}
	lib, err := eval.LoadTeamLibrary(libPath, dex)
	if err != nil {
		return nil, fmt.Errorf("load team library: %w", err)
	}
	return lib.Teams, nil
}

// llmContestants builds Anthropic-backed contestants from specs of the form
// "label=model" or "model". The API key comes from ANTHROPIC_API_KEY and never
// leaves the machine. The client is stateless and shared across that model's
// games; the game seed is irrelevant to a model we don't seed, so the factory
// ignores it — non-determinism is expected and handled by the confidence
// intervals, not pretended away.
func llmContestants(specs []string, dex *domain.Dex) []eval.Contestant {
	if len(specs) == 0 {
		return nil
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		log.Fatalf("-llm requires ANTHROPIC_API_KEY (your key, used locally)")
	}
	out := make([]eval.Contestant, 0, len(specs))
	for _, spec := range specs {
		label, model := spec, spec
		if eq := strings.IndexByte(spec, '='); eq >= 0 {
			label, model = spec[:eq], spec[eq+1:]
		}
		if label == "" || model == "" {
			log.Fatalf("bad -llm entry %q (want label=model or model)", spec)
		}
		client := llm.NewAnthropic(key, model)
		out = append(out, eval.Contestant{
			Name: label,
			New:  func(uint64) ai.Agent { return agentloop.NewAgent(label, dex, client) },
		})
	}
	return out
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
