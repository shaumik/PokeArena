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
	"pokearena/internal/engine"
	"pokearena/internal/eval"
	"pokearena/internal/llm"
	"pokearena/internal/usage"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[bench] ")

	var (
		dataDir   = flag.String("data", "data", "dataset directory")
		agentCSV  = flag.String("agents", "random,heuristic,expectimax", "comma-separated baseline agents (random, heuristic, expectimax)")
		llmCSV    = flag.String("llm", "", "comma-separated LLM contestants as [label=][vendor:]model[/condition]; vendor in {anthropic,openai,gemini,ollama} (default anthropic), condition raw (default) or cot; keys from <VENDOR>_API_KEY")
		cotBudget = flag.Int("cot-budget", 2048, "extended-thinking token budget for /cot contestants")
		games     = flag.Int("games", 20, "seeds per pairing per team (each played in both side orientations)")
		libPath   = flag.String("teams", "data/benchmark-teams.json", "competitive team library; every team is mirror-matched and results aggregated")
		teamCSV   = flag.String("team", "", "ad-hoc override: comma-separated dex numbers, mirrored to both sides (bypasses -teams)")
		depth     = flag.Int("depth", 2, "fixed search depth for the expectimax agent")
		budgetMs  = flag.Int("budget-ms", 0, "per-decision time budget in ms (0 = none; recommended for LLM agents)")
		outPath   = flag.String("out", "", "JSONL output path (default: stdout)")
		runsDir   = flag.String("runs", "runs", "directory to persist the run record + append the run index (\"\" to disable)")
		pricePath = flag.String("pricing", "data/model-pricing.json", "model pricing table for costing LLM token usage")
	)
	flag.Parse()

	dex, err := domain.LoadDex(*dataDir, "bench")
	if err != nil {
		log.Fatalf("load dex from %s: %v", *dataDir, err)
	}

	benchTeams, libVersion, err := loadBenchTeams(dex, *teamCSV, *libPath)
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
	llmCs, models, conditions, localModels := llmContestants(splitCSV(*llmCSV), dex, *cotBudget)
	contestants = append(contestants, llmCs...)
	if len(contestants) < 2 {
		log.Fatalf("need at least 2 contestants (via -agents and/or -llm), got %d", len(contestants))
	}
	// Contestant names are the identity used to attribute wins and rank the
	// board, so a collision silently corrupts the results. Catch it here with an
	// actionable message rather than letting RunMatch reject it mid-run.
	if dup := firstDuplicateName(contestants); dup != "" {
		log.Fatalf("duplicate contestant name %q — every contestant needs a unique label; "+
			"disambiguate with label= (e.g. -llm a=%s,b=%s)", dup, dup, dup)
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

	// Reproducibility header: pin dataset + code + ruleset + config as the first
	// line, so any trace names exactly what produced it.
	prov, err := eval.LoadProvenance(*dataDir)
	if err != nil {
		log.Fatalf("%v", err)
	}
	header := eval.RunHeader{
		EngineRevision:  eval.EngineRevision(),
		DataSimVersion:  prov.SimVersion,
		DataCurationSHA: prov.CurationSHA,
		DataSourceGen:   prov.SourceGen,
		Level:           engine.Level,
		Ruleset:         eval.Ruleset(),
		TeamLibrary:     libVersion,
		Teams:           teamNames(benchTeams),
		Contestants:     contestantNames(contestants),
		ExpectimaxDepth: *depth,
		GamesPerPairing: *games,
		Orientations:    2,
		Seeds:           fmt.Sprintf("0..%d", *games-1),
		BudgetMs:        *budgetMs,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
	}
	if err := eval.WriteRunHeader(out, header); err != nil {
		log.Fatalf("write run header: %v", err)
	}

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

	// Build the run record now, and print FROM it, so the console, the saved
	// JSON, and any later report all cite the exact same numbers. Pricing is
	// loaded only when a paid model is in the run — a baseline-only or
	// local-only run has nothing to cost.
	pricing := loadPricing(*pricePath, len(models) > len(localModels))
	// Local models spend tokens but cost nothing: price them at zero so the
	// report shows "free", not "unknown" (which is reserved for a missing price
	// on a paid model).
	for m := range localModels {
		if _, ok := pricing[m]; !ok {
			if pricing == nil {
				pricing = map[string]usage.Pricing{}
			}
			pricing[m] = usage.Pricing{}
		}
	}
	record := eval.BuildRunRecord(header, matches, models, conditions, pricing)

	// Per-team Elo surfaces whether the ranking holds across teams or is an
	// artifact of one — the reason the benchmark runs across a library rather
	// than a single team.
	if len(record.PerTeam) > 0 {
		fmt.Fprintln(os.Stderr, "\nper-team Elo:")
		for _, tr := range record.PerTeam {
			parts := make([]string, len(tr.Ranks))
			for i, r := range tr.Ranks {
				parts[i] = fmt.Sprintf("%s %.0f", r.Name, r.Elo)
			}
			fmt.Fprintf(os.Stderr, "  %-10s %s\n", tr.Team, strings.Join(parts, "  "))
		}
	}

	fmt.Fprintln(os.Stderr, "\noverall standings (Elo, win rate with Wilson 95% CI):")
	fmt.Fprintf(os.Stderr, "  %-12s %6s  %-8s %-18s %s\n", "agent", "elo", "winrate", "95% CI", "W-L-D")
	for _, r := range record.Contestants {
		fmt.Fprintf(os.Stderr, "  %-12s %6.0f  %6.1f%%  [%5.1f%%, %5.1f%%]  %d-%d-%d (n=%d)\n",
			r.Name, r.Elo, 100*r.WinRate, 100*r.CILow, 100*r.CIHigh, r.Wins, r.Losses, r.Draws, r.Games)
	}

	printCost(record)
	if *runsDir != "" {
		path, err := eval.SaveRun(*runsDir, record)
		if err != nil {
			log.Fatalf("save run: %v", err)
		}
		log.Printf("saved run %s to %s (index: %s)", record.RunID, path, *runsDir+"/index.jsonl")
	}

	if *outPath != "" {
		log.Printf("wrote JSONL trace to %s", *outPath)
	}
}

// loadPricing loads the model pricing table. It is required only when the run
// has LLM contestants to cost; a baseline-only run tolerates a missing file so
// the benchmark works out of the box without one.
func loadPricing(path string, required bool) map[string]usage.Pricing {
	p, err := eval.LoadPricing(path)
	if err != nil {
		if required {
			log.Fatalf("load pricing (needed to cost -llm token usage): %v", err)
		}
		return nil
	}
	return p
}

// printCost reports each contestant's measured token cost. Deterministic agents
// are free; a model whose price we lack is shown as unknown, never as $0, so a
// missing price can't be mistaken for a cheap model.
func printCost(rec eval.RunRecord) {
	anyPaid := false
	for _, c := range rec.Contestants {
		if !c.Usage.IsZero() {
			anyPaid = true
			break
		}
	}
	if !anyPaid {
		return
	}
	fmt.Fprintln(os.Stderr, "\ntoken cost (measured):")
	fmt.Fprintf(os.Stderr, "  %-12s %10s %10s %12s %12s\n", "agent", "in", "out", "$/game", "$ total")
	for _, c := range rec.Contestants {
		if c.Usage.IsZero() {
			continue
		}
		cost, perGame := "unknown", "unknown"
		if c.CostKnown {
			cost = fmt.Sprintf("$%.4f", c.CostUSD)
			perGame = fmt.Sprintf("$%.5f", c.CostPerGameUSD)
		}
		fmt.Fprintf(os.Stderr, "  %-12s %10d %10d %12s %12s\n",
			c.Name, c.Usage.InputTokens+c.Usage.CacheReadTokens+c.Usage.CacheWriteTokens, c.Usage.OutputTokens, perGame, cost)
	}
	fmt.Fprintf(os.Stderr, "  run total: $%.4f\n", rec.TotalCostUSD)
}

// makeContestant maps an agent name to a fresh-per-game factory. Random is
// seeded from the game seed for reproducibility; heuristic and expectimax are
// deterministic and ignore it. Expectimax uses the fixed-depth (reproducible)
// mode so its choices don't depend on machine speed.
//
// "expectimax" uses the -depth flag; "expectimax@N" pins depth N and becomes a
// distinct contestant named "expectimax-dN", so a single run can pit several
// search depths against each other (e.g. to compare whether deeper plays
// better — on this format it does not always, see docs/benchmark.md §6).
func makeContestant(name string, dex *domain.Dex, depth int) (eval.Contestant, error) {
	switch {
	case name == "random":
		return eval.Contestant{Name: "random", New: func(seed uint64) ai.Agent { return ai.NewRandomAgent(seed) }}, nil
	case name == "heuristic":
		return eval.Contestant{Name: "heuristic", New: func(uint64) ai.Agent { return ai.NewHeuristicAgent(dex) }}, nil
	case name == "expectimax":
		return eval.Contestant{Name: "expectimax", New: func(uint64) ai.Agent { return ai.NewExpectimaxAgentFixed(dex, depth) }}, nil
	case strings.HasPrefix(name, "expectimax@"):
		d, err := strconv.Atoi(strings.TrimPrefix(name, "expectimax@"))
		if err != nil || d < 1 {
			return eval.Contestant{}, fmt.Errorf("bad expectimax depth in %q (want expectimax@N, N>=1)", name)
		}
		label := fmt.Sprintf("expectimax-d%d", d)
		return eval.Contestant{Name: label, New: func(uint64) ai.Agent { return ai.NewExpectimaxAgentFixed(dex, d) }}, nil
	default:
		return eval.Contestant{}, fmt.Errorf("unknown agent %q (known: random, heuristic, expectimax, expectimax@N)", name)
	}
}

// loadBenchTeams resolves the teams to run on. An explicit -team (dex numbers)
// is an ad-hoc single-team override; otherwise the curated, legality-checked
// library at libPath is used and every team is mirror-matched.
func loadBenchTeams(dex *domain.Dex, teamCSV, libPath string) (teams []eval.NamedTeam, version string, err error) {
	if teamCSV != "" {
		dexNos, err := parseTeam(teamCSV)
		if err != nil {
			return nil, "", fmt.Errorf("bad -team: %w", err)
		}
		picks, err := eval.PicksFromDex(dex, dexNos)
		if err != nil {
			return nil, "", fmt.Errorf("build ad-hoc team: %w", err)
		}
		return []eval.NamedTeam{{Name: "adhoc", Picks: picks}}, "adhoc", nil
	}
	lib, err := eval.LoadTeamLibrary(libPath, dex)
	if err != nil {
		return nil, "", fmt.Errorf("load team library: %w", err)
	}
	return lib.Teams, lib.Version, nil
}

func teamNames(teams []eval.NamedTeam) []string {
	names := make([]string, len(teams))
	for i, t := range teams {
		names[i] = t.Name
	}
	return names
}

func contestantNames(cs []eval.Contestant) []string {
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Name
	}
	return names
}

// firstDuplicateName returns the first contestant name that appears more than
// once, or "" if every name is unique. Names are the identity the round-robin
// attributes wins to, so a duplicate must be caught before any match runs.
func firstDuplicateName(cs []eval.Contestant) string {
	seen := make(map[string]bool, len(cs))
	for _, c := range cs {
		if seen[c.Name] {
			return c.Name
		}
		seen[c.Name] = true
	}
	return ""
}

// llmSpec is a parsed -llm entry: a display label, the vendor and model id to
// call, and the standardized harness condition it runs under.
type llmSpec struct {
	label     string
	vendor    string // "" means anthropic (the default vendor)
	model     string
	condition string // "raw" or "cot"
}

// parseLLMSpec parses one -llm entry. The grammar is
//
//	[label=][vendor:]model[/condition]
//
// vendor is one of the known providers (anthropic default when omitted);
// condition is "raw" (default; 1-shot, no thinking) or "cot" (1-shot, thinking
// enabled) — the two reproducible columns of the board. When no label is given
// it defaults to model, suffixed with the condition ("model:cot") so a
// cross-product of the same model in both conditions gets distinct names.
//
// The vendor prefix is only consumed when the text before the first colon is a
// known vendor, so a local model id that itself contains a colon (Ollama's
// "llama3.1:8b") is left intact under an explicit "ollama:" prefix.
func parseLLMSpec(spec string) (llmSpec, error) {
	s := llmSpec{condition: "raw"}
	rest := spec
	if slash := strings.LastIndexByte(rest, '/'); slash >= 0 {
		cond := rest[slash+1:]
		if cond != "raw" && cond != "cot" {
			return llmSpec{}, fmt.Errorf("bad -llm entry %q: condition must be raw or cot, got %q", spec, cond)
		}
		s.condition, rest = cond, rest[:slash]
	}
	label := ""
	if eq := strings.IndexByte(rest, '='); eq >= 0 {
		label, rest = rest[:eq], rest[eq+1:]
	}
	if colon := strings.IndexByte(rest, ':'); colon >= 0 {
		if cand := rest[:colon]; llm.KnownVendor(cand) {
			s.vendor, rest = strings.ToLower(cand), rest[colon+1:]
		}
	}
	model := rest
	if model == "" {
		return llmSpec{}, fmt.Errorf("bad -llm entry %q (want [label=][vendor:]model[/condition])", spec)
	}
	if label == "" {
		label = model
		if s.condition != "raw" {
			label += ":" + s.condition
		}
	}
	s.label, s.model = label, model
	return s, nil
}

// llmContestants builds model-backed contestants from -llm specs, one adapter
// per spec via the vendor factory. API keys come from each vendor's env var and
// never leave the machine; a local vendor (Ollama) needs none. The client is
// stateless and shared across that model's games; the game seed is irrelevant
// to a model we don't seed, so the factory ignores it — non-determinism is
// expected and handled by the confidence intervals, not pretended away.
//
// cotBudget is the thinking token budget applied to "cot" contestants. It
// returns name→model and name→condition maps for pricing and the board, plus
// the set of model ids that run locally (Ollama) and therefore cost nothing.
// Keys are resolved lazily and cached, so a run touches only the vendors it
// uses.
func llmContestants(specs []string, dex *domain.Dex, cotBudget int) (cs []eval.Contestant, models, conditions map[string]string, localModels map[string]bool) {
	if len(specs) == 0 {
		return nil, nil, nil, nil
	}
	keys := map[string]string{} // vendor -> resolved key
	models = make(map[string]string, len(specs))
	conditions = make(map[string]string, len(specs))
	localModels = make(map[string]bool)
	for _, spec := range specs {
		s, err := parseLLMSpec(spec)
		if err != nil {
			log.Fatalf("%v", err)
		}
		cfg := llm.Config{
			Key:   resolveKey(keys, s.vendor),
			Model: s.model,
		}
		if s.condition == "cot" {
			// Thinking on: a budget for deliberation plus headroom for the
			// answer. Each adapter maps the budget onto its own reasoning knob.
			cfg.Thinking = cotBudget
			cfg.MaxTokens = cotBudget + defaultAnswerTokens
		}
		client, err := llm.New(s.vendor, cfg)
		if err != nil {
			log.Fatalf("%v", err)
		}
		label := s.label
		cs = append(cs, eval.Contestant{
			Name: label,
			New:  func(uint64) ai.Agent { return agentloop.NewAgent(label, dex, client) },
		})
		models[label] = s.model // for pricing the run's measured token cost
		conditions[label] = s.condition
		if llm.IsLocal(s.vendor) {
			localModels[s.model] = true
		}
	}
	return cs, models, conditions, localModels
}

// resolveKey returns the API key for a vendor, reading its env var once and
// caching the result. A local vendor needs no key. Exits with a clear message
// if a required key is missing.
func resolveKey(cache map[string]string, vendor string) string {
	if v, ok := cache[vendor]; ok {
		return v
	}
	env, needs := llm.KeyEnvVar(vendor)
	if !needs {
		cache[vendor] = ""
		return ""
	}
	key := os.Getenv(env)
	if key == "" {
		v := vendor
		if v == "" {
			v = "anthropic"
		}
		log.Fatalf("-llm contestant needs %s (your %s key, used locally)", env, v)
	}
	cache[vendor] = key
	return key
}

// defaultAnswerTokens is the output headroom reserved past the thinking budget
// for a cot contestant's actual decision.
const defaultAnswerTokens = 512

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
