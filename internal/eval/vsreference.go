package eval

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
)

// The benchmark report reunites the two arms — the baseline round-robin and the
// live agentic runs — into one RunRecord, so the published page IS the standard
// report with every feature: the leaderboard, the head-to-head matrix, per-team
// Elo, embedded replays with the momentum graph, and roster reveal.
//
// The baselines are a full round-robin (every agent vs every other), so their
// matches carry per-game records and re-simulate into watchable replays and a
// dense matrix. The language models each played only the reference bot over the
// live gateway, so they join as contestants with a single sparse matrix cell
// (their win rate vs the reference) and no local replay — everything else on the
// page is shared, and one Bradley-Terry fit ranks them all on the same scale.

// modelDisplay maps an agentic config key ("cc-haiku", "agy-gemini") to a human
// display name and the model id that marks a contestant model-backed on the
// board (an empty id renders as a deterministic agent).
func modelDisplay(config string) (name, model string) {
	switch {
	case strings.HasPrefix(config, "agy-"):
		return "Gemini 3.1 Pro", "gemini-3.1-pro"
	case strings.HasPrefix(config, "cc-"):
		short := strings.TrimPrefix(config, "cc-")
		disp := map[string]string{"haiku": "Claude Haiku 4.5", "sonnet": "Claude Sonnet 4.6", "opus": "Claude Opus 4.8"}[short]
		id := map[string]string{"haiku": "claude-haiku-4-5", "sonnet": "claude-sonnet-4-6", "opus": "claude-opus-4-8"}[short]
		if disp == "" {
			return "Claude " + short, "claude-" + short
		}
		return disp, id
	}
	return config, ""
}

// baselineFactory resolves a baseline contestant name to an agent constructor so
// its recorded games can be re-simulated for the report's replays and matrix. It
// returns false for a name it cannot build (an unknown or model-backed agent).
func baselineFactory(name string, dex *domain.Dex) (AgentFactory, bool) {
	switch {
	case name == "random":
		return func(seed uint64) ai.Agent { return ai.NewRandomAgent(seed) }, true
	case name == "heuristic":
		return func(uint64) ai.Agent { return ai.NewHeuristicAgent(dex) }, true
	case strings.HasPrefix(name, "expectimax-d"):
		d, err := strconv.Atoi(strings.TrimPrefix(name, "expectimax-d"))
		if err != nil || d < 1 {
			return nil, false
		}
		return func(uint64) ai.Agent { return ai.NewExpectimaxAgentFixed(dex, d) }, true
	}
	return nil, false
}

// BuildVsReferenceRecord assembles the full benchmark RunRecord from the two
// arms: the baseline round-robin trace at baselinePath and the agentic results
// under agenticDir (each live model's per-team record vs ref, default
// "heuristic"). Standings, Elo, and Wilson intervals come from the shared
// BuildRunRecord over one combined match set; the baseline matches drive
// CaptureMatchups (replays + matrix) and the per-team breakdown, and teams
// supplies the rosters. Every feature of the standard report is populated.
func BuildVsReferenceRecord(dex *domain.Dex, baselinePath, agenticDir, ref string, teams []NamedTeam, header RunHeader) (RunRecord, error) {
	if ref == "" {
		ref = "heuristic"
	}

	var matches []MatchResult
	models := map[string]string{}
	baselineNames := map[string]bool{}

	// Baselines: the full round-robin, every pairing on every team, with per-game
	// records so the matches re-simulate into replays and a dense matrix.
	if baselinePath != "" {
		bm, err := baselineMatches(baselinePath)
		if err != nil {
			return RunRecord{}, err
		}
		matches = append(matches, bm...)
		for _, m := range bm {
			baselineNames[m.A] = true
			baselineNames[m.B] = true
		}
	}

	// Agentic: each live model's per-team record vs the reference. Kept per team
	// so it folds into the existing per-team cards; the model id marks it
	// model-backed on the board.
	if agenticDir != "" {
		am, mods, err := agenticMatches(agenticDir, ref)
		if err != nil && !os.IsNotExist(err) {
			return RunRecord{}, err
		}
		matches = append(matches, am...)
		for name, id := range mods {
			models[name] = id
		}
	}

	rec := BuildRunRecord(header, matches, models, nil, nil)

	// Only the baselines have factories, so only they re-simulate. CaptureMatchups
	// skips any pairing whose agents it cannot rebuild (the model rows), leaving
	// their matrix cell present but replayless — which is exactly right.
	names := make([]string, 0, len(baselineNames))
	for n := range baselineNames {
		if _, ok := baselineFactory(n, dex); ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	contestants := make([]Contestant, 0, len(names))
	for _, n := range names {
		f, _ := baselineFactory(n, dex)
		contestants = append(contestants, Contestant{Name: n, New: f})
	}

	replays, matrix := CaptureMatchups(dex, contestants, teams, matches, rec.Contestants, Budget(0))
	rec.Replays = replays
	rec.Matrix = &matrix
	rec.Rosters = BuildRosters(dex, teams)

	// A live model's game vs the reference can't be re-simulated from a seed, but
	// its turns were persisted, so a reconstructed replay (dropped in
	// <agenticDir>/replays) is wired into the matrix cell for that matchup — the
	// one battle a viewer can actually watch a model play.
	if agenticDir != "" {
		attachAgenticReplays(&rec, filepath.Join(agenticDir, "replays"))
	}

	// Restate the leaderboard's win-rate bar as each contestant's head-to-head
	// record against the reference, so the bar is one apples-to-apples axis
	// (a baseline's overall round-robin rate would use a different denominator
	// than a model that only ever faced the reference). The Elo ranking, matrix,
	// per-team, and replays are unchanged — they still reflect all the games.
	// The reference itself has no self-play, so it sits at the 50% mark: by the
	// symmetry of a mirror it is exactly even with itself.
	scoreVsRef(rec.Contestants, matches, ref)
	return rec, nil
}

// scoreVsRef overwrites each contestant's win/loss record and win rate (with a
// Wilson interval) to count only its games against ref, in place. The reference
// row is set to the 50% self-reference. Order and Elo are left untouched.
func scoreVsRef(contestants []ContestantResult, matches []MatchResult, ref string) {
	type wl struct{ w, l, d int }
	vs := map[string]wl{}
	for _, m := range matches {
		switch ref {
		case m.B:
			r := vs[m.A]
			r.w, r.l, r.d = r.w+m.AWins, r.l+m.BWins, r.d+m.Draws
			vs[m.A] = r
		case m.A:
			r := vs[m.B]
			r.w, r.l, r.d = r.w+m.BWins, r.l+m.AWins, r.d+m.Draws
			vs[m.B] = r
		}
	}
	for i := range contestants {
		c := &contestants[i]
		if c.Name == ref {
			c.Wins, c.Losses, c.Draws, c.Games = 0, 0, 0, 0
			c.WinRate, c.CILow, c.CIHigh = 0.5, 0.5, 0.5
			continue
		}
		r := vs[c.Name]
		n := r.w + r.l + r.d
		c.Wins, c.Losses, c.Draws, c.Games = r.w, r.l, r.d, n
		if n == 0 {
			c.WinRate, c.CILow, c.CIHigh = 0, 0, 0
			continue
		}
		success := float64(r.w) + 0.5*float64(r.d)
		c.WinRate = success / float64(n)
		c.CILow, c.CIHigh = WilsonInterval(success, n, Z95)
	}
}

// attachAgenticReplays loads reconstructed model-vs-reference replays from dir
// (one Replay JSON per file), appends each to the record, and points its matrix
// cell (both orientations) at it, so the matchup chip becomes watchable. A
// replay whose two sides are not both on the board is skipped.
func attachAgenticReplays(rec *RunRecord, dir string) {
	if rec.Matrix == nil {
		return
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(files)
	idxOf := map[string]int{}
	for i, a := range rec.Matrix.Agents {
		idxOf[a] = i
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var rep Replay
		if err := json.Unmarshal(data, &rep); err != nil {
			continue
		}
		ri, ok0 := idxOf[rep.Side0]
		ci, ok1 := idxOf[rep.Side1]
		if !ok0 || !ok1 {
			continue
		}
		newIdx := len(rec.Replays)
		rec.Replays = append(rec.Replays, rep)
		for k := range rec.Matrix.Cells {
			c := &rec.Matrix.Cells[k]
			if (c.Row == ri && c.Col == ci) || (c.Row == ci && c.Col == ri) {
				c.Replay = newIdx
			}
		}
	}
}

// baselineMatches parses a round-robin JSONL trace into per-team MatchResults
// (one per pairing per team), each carrying the game records the replay/matrix
// capture re-simulates. The A/B roles come from the "A-vs-B" match name so a
// pairing aggregates consistently across its two orientations.
func baselineMatches(path string) ([]MatchResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	agg := map[string]*MatchResult{}
	var order []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"type":"game"`) {
			continue
		}
		var g struct {
			Match, Team, Side0, Side1, Winner string
			Seed                              uint64
			Turns                             int
		}
		if err := json.Unmarshal(line, &g); err != nil {
			continue
		}
		a, b, ok := splitMatchName(g.Match)
		if !ok {
			continue
		}
		key := g.Match + "|" + g.Team
		mr := agg[key]
		if mr == nil {
			mr = &MatchResult{A: a, B: b, Team: g.Team}
			agg[key] = mr
			order = append(order, key)
		}
		switch g.Winner {
		case a:
			mr.AWins++
		case b:
			mr.BWins++
		default:
			mr.Draws++
		}
		mr.Games = append(mr.Games, GameRecord{
			Match: g.Match, Team: g.Team, Seed: g.Seed,
			Side0: g.Side0, Side1: g.Side1, Winner: g.Winner,
			Result: GameResult{Seed: g.Seed, Turns: g.Turns, Winner: winnerIndex(g.Winner, g.Side0, g.Side1)},
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	out := make([]MatchResult, 0, len(order))
	for _, k := range order {
		out = append(out, *agg[k])
	}
	return out, nil
}

// splitMatchName splits an "A-vs-B" match name into its two contestants.
func splitMatchName(match string) (a, b string, ok bool) {
	parts := strings.SplitN(match, "-vs-", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// winnerIndex maps a winner name to a board side (0/1), or 2 for a draw.
func winnerIndex(winner, side0, side1 string) int {
	switch winner {
	case side0:
		return 0
	case side1:
		return 1
	default:
		return 2
	}
}

// agenticMatches reads the agentic results directory (subdirs named
// "<model>-<team>", each with a results.txt of "gN winner=<0|1|-1> ..." lines)
// and returns one MatchResult per model per team: the model's decided wins
// (winner=0) and losses (winner=1) against ref, undecided games (winner=-1)
// dropped. It also returns the display-name -> model-id map for the board.
func agenticMatches(dir, ref string) ([]MatchResult, map[string]string, error) {
	dirs, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return nil, nil, err
	}
	var matches []MatchResult
	models := map[string]string{}
	for _, d := range dirs {
		data, err := os.ReadFile(filepath.Join(d, "results.txt"))
		if err != nil {
			continue
		}
		// "cc-haiku-Genesis" -> model key "cc-haiku", team "Genesis".
		cfg := filepath.Base(d)
		i := strings.LastIndex(cfg, "-")
		if i <= 0 || i == len(cfg)-1 {
			continue
		}
		modelKey, team := cfg[:i], cfg[i+1:]
		name, id := modelDisplay(modelKey)
		models[name] = id

		var w, l int
		for _, ln := range strings.Split(string(data), "\n") {
			switch {
			case strings.Contains(ln, "winner=0"):
				w++
			case strings.Contains(ln, "winner=1"):
				l++
			}
		}
		if w+l == 0 {
			continue // decided nothing on this team: dropped, not booked as a loss
		}
		matches = append(matches, MatchResult{A: name, B: ref, Team: team, AWins: w, BWins: l})
	}
	return matches, models, nil
}
