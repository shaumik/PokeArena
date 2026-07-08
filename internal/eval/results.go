package eval

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"pokearena/internal/usage"
)

// A benchmark that only prints its numbers forgets them. This file makes a run
// a durable, comparable record: every run is saved as one JSON file plus an
// appended line in an index, so a leaderboard can be tracked over time, two
// runs diffed, and cost accounted — without re-running anything.
//
// The store is deliberately plain files (a directory of run JSON + a JSONL
// index), not a database: the whole benchmark is built to be reproduced from a
// clone, and a text index is the format a third party can read, grep, and
// re-derive without our tooling.

// ContestantResult is one contestant's complete standing in a run: the ranking
// numbers from Standings, joined with the measured token cost. It is the row a
// leaderboard-over-time is built from.
type ContestantResult struct {
	Name string `json:"name"`
	// Model is the model id (empty for deterministic agents). Condition is the
	// standardized harness column — "raw" (1-shot, no thinking) or "cot" (1-shot,
	// thinking enabled) — empty for deterministic agents. Together they place a
	// contestant on the model×condition board.
	Model     string  `json:"model,omitempty"`
	Condition string  `json:"condition,omitempty"`
	Elo       float64 `json:"elo"`
	Wins      int     `json:"wins"`
	Losses    int     `json:"losses"`
	Draws     int     `json:"draws"`
	Games     int     `json:"games"`
	WinRate   float64 `json:"win_rate"`
	CILow     float64 `json:"ci_low"`
	CIHigh    float64 `json:"ci_high"`

	Usage usage.Usage `json:"usage"`
	// CostKnown is false when the agent spent tokens but no pricing was found
	// for its model — the cost is then reported as unknown rather than as free,
	// so a missing price can never masquerade as a cheap model.
	CostKnown      bool    `json:"cost_known"`
	CostUSD        float64 `json:"cost_usd"`
	CostPerGameUSD float64 `json:"cost_per_game_usd"`
}

// RunRecord is the full, persisted result of one benchmark run: what produced
// it (Header), how everyone ranked and what it cost (Contestants), the per-team
// Elo breakdown, and the run-level total. Saved verbatim, it is the artifact a
// published number cites.
type RunRecord struct {
	RunID        string             `json:"run_id"`
	GeneratedAt  string             `json:"generated_at"`
	Header       RunHeader          `json:"header"`
	Contestants  []ContestantResult `json:"contestants"`
	PerTeam      []TeamRanking      `json:"per_team,omitempty"`
	TotalCostUSD float64            `json:"total_cost_usd"`
}

// NameElo is one contestant's Elo on one team — the compact form used for the
// per-team breakdown, where cost and record are already in Contestants.
type NameElo struct {
	Name string  `json:"name"`
	Elo  float64 `json:"elo"`
}

// TeamRanking is the Elo ordering of contestants on a single team, sorted best
// first. It is what surfaces whether a ranking holds across the library or is an
// artifact of one team — the reason the benchmark runs across a library at all.
type TeamRanking struct {
	Team  string    `json:"team"`
	Ranks []NameElo `json:"ranks"`
}

// perTeamRankings groups matches by team and ranks contestants by Elo within
// each, preserving the order teams first appear so the breakdown reads in
// library order rather than randomly.
func perTeamRankings(matches []MatchResult) []TeamRanking {
	var order []string
	byTeam := map[string][]MatchResult{}
	for _, m := range matches {
		if _, seen := byTeam[m.Team]; !seen {
			order = append(order, m.Team)
		}
		byTeam[m.Team] = append(byTeam[m.Team], m)
	}
	// A single-team run (ad-hoc -team) has no cross-team story to tell.
	if len(order) < 2 {
		return nil
	}
	out := make([]TeamRanking, 0, len(order))
	for _, team := range order {
		tr := TeamRanking{Team: team}
		for _, s := range Standings(byTeam[team]) {
			tr.Ranks = append(tr.Ranks, NameElo{Name: s.Name, Elo: s.Elo})
		}
		out = append(out, tr)
	}
	return out
}

// usageByContestant sums the per-game, per-seat token usage across every match
// back to the contestant who occupied that seat. Side0/Side1 name the seats, so
// a mirror match (same team both sides) still attributes each side's cost to
// the right contestant.
func usageByContestant(matches []MatchResult) map[string]usage.Usage {
	out := map[string]usage.Usage{}
	for _, m := range matches {
		for _, g := range m.Games {
			out[g.Side0] = out[g.Side0].Add(g.Result.Usage[0])
			out[g.Side1] = out[g.Side1].Add(g.Result.Usage[1])
		}
	}
	return out
}

// BuildRunRecord joins the standings, the measured token usage, and a pricing
// table into a persistable record. models maps a contestant name to its model
// id and conditions maps a name to its harness column ("raw"/"cot"); only LLM
// contestants appear in either. pricing maps a model id to its rates. A
// contestant that spent tokens but whose model is absent from pricing is marked
// CostKnown=false rather than free.
func BuildRunRecord(header RunHeader, matches []MatchResult, models, conditions map[string]string, pricing map[string]usage.Pricing) RunRecord {
	standings := Standings(matches)
	tokens := usageByContestant(matches)

	rec := RunRecord{
		RunID:       runID(header),
		GeneratedAt: header.Timestamp,
		Header:      header,
		PerTeam:     perTeamRankings(matches),
	}
	for _, s := range standings {
		u := tokens[s.Name]
		cr := ContestantResult{
			Name:      s.Name,
			Model:     models[s.Name],
			Condition: conditions[s.Name],
			Elo:       s.Elo,
			Wins:      s.Wins,
			Losses:    s.Losses,
			Draws:     s.Draws,
			Games:     s.Games,
			WinRate:   s.WinRate,
			CILow:     s.CILow,
			CIHigh:    s.CIHigh,
			Usage:     u,
		}
		// Cost known-ness keys on whether the contestant is MODEL-BACKED, not on
		// whether it happened to spend tokens. A deterministic agent (no model id)
		// is free with certainty. A model agent's cost is known only if we have its
		// price — even when its measured usage is zero (e.g. every call errored
		// after billing), so a priced model still reports an honest $0 while an
		// unpriced model stays CostKnown=false instead of masquerading as free.
		if models[s.Name] == "" {
			cr.CostKnown = true // deterministic agent: free, not merely token-less
		} else if p, ok := pricing[cr.Model]; ok {
			cr.CostKnown = true
			cr.CostUSD = u.Cost(p)
			if s.Games > 0 {
				cr.CostPerGameUSD = cr.CostUSD / float64(s.Games)
			}
		}
		rec.TotalCostUSD += cr.CostUSD
		rec.Contestants = append(rec.Contestants, cr)
	}
	return rec
}

// runID is a sortable, comparable identifier: the run's UTC timestamp (so runs
// order chronologically) joined with a short hash of the config that defines
// the run (engine, dataset, library, contestants, game count). The same config
// re-run later gets the same suffix but a new timestamp, which is exactly what
// makes a timeline of "the same benchmark over time" legible.
func runID(h RunHeader) string {
	ts := h.Timestamp
	if ts == "" {
		ts = "unknown"
	}
	// Compact the RFC3339 timestamp into a filename-safe token.
	stamp := strings.NewReplacer(":", "", "-", "").Replace(ts)

	hsh := fnv.New32a()
	fmt.Fprint(hsh, h.EngineRevision, "|", h.DataSimVersion, "|", h.TeamLibrary, "|",
		strings.Join(h.Contestants, ","), "|", h.GamesPerPairing)
	return fmt.Sprintf("%s-%08x", stamp, hsh.Sum32())
}

const indexFile = "index.jsonl"

// RunIndexEntry is the compact, one-line-per-run summary appended to the index.
// It carries just enough to draw a timeline and a cost trend without opening
// every full run file.
type RunIndexEntry struct {
	RunID          string          `json:"run_id"`
	GeneratedAt    string          `json:"generated_at"`
	EngineRevision string          `json:"engine_revision"`
	DataSimVersion string          `json:"data_sim_version"`
	TeamLibrary    string          `json:"team_library"`
	Games          int             `json:"games"`
	TotalCostUSD   float64         `json:"total_cost_usd"`
	Standings      []IndexStanding `json:"standings"`
}

// IndexStanding is a contestant's headline numbers in the index: rank signal
// (Elo) and cost, enough to chart either over successive runs.
type IndexStanding struct {
	Name    string  `json:"name"`
	Elo     float64 `json:"elo"`
	CostUSD float64 `json:"cost_usd"`
}

func indexEntry(rec RunRecord) RunIndexEntry {
	e := RunIndexEntry{
		RunID:          rec.RunID,
		GeneratedAt:    rec.GeneratedAt,
		EngineRevision: rec.Header.EngineRevision,
		DataSimVersion: rec.Header.DataSimVersion,
		TeamLibrary:    rec.Header.TeamLibrary,
		Games:          rec.Header.GamesPerPairing,
		TotalCostUSD:   rec.TotalCostUSD,
	}
	for _, c := range rec.Contestants {
		e.Standings = append(e.Standings, IndexStanding{Name: c.Name, Elo: c.Elo, CostUSD: c.CostUSD})
	}
	return e
}

// SaveRun persists a run to dir: the full record as <run_id>.json and a compact
// line appended to index.jsonl. It creates dir if needed and returns the path
// of the full record. The index is append-only so history accumulates; the
// per-run file is the authoritative detail.
func SaveRun(dir string, rec RunRecord) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create runs dir: %w", err)
	}

	full, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode run: %w", err)
	}
	path := filepath.Join(dir, rec.RunID+".json")
	if err := os.WriteFile(path, append(full, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write run file: %w", err)
	}

	line, err := json.Marshal(indexEntry(rec))
	if err != nil {
		return "", fmt.Errorf("encode index entry: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, indexFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("open index: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return "", fmt.Errorf("append index: %w", err)
	}
	return path, nil
}

// LoadIndex reads the run index in chronological (append) order. A missing
// index is not an error — it just means no runs have been recorded yet.
func LoadIndex(dir string) ([]RunIndexEntry, error) {
	data, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read index: %w", err)
	}
	var entries []RunIndexEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e RunIndexEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("parse index line: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// LoadPricing reads a model-id -> Pricing table from a JSON file. The file maps
// each model id to its per-million-token rates. Keys beginning with "_" are
// metadata (e.g. "_note") and skipped, so the file can document itself. A
// missing file is an error: pricing a run without a table would silently report
// every model as free.
func LoadPricing(path string) (map[string]usage.Pricing, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pricing %s: %w", path, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse pricing %s: %w", path, err)
	}
	table := make(map[string]usage.Pricing, len(raw))
	for model, v := range raw {
		if strings.HasPrefix(model, "_") {
			continue
		}
		var p usage.Pricing
		if err := json.Unmarshal(v, &p); err != nil {
			return nil, fmt.Errorf("parse pricing for %q: %w", model, err)
		}
		table[model] = p
	}
	return table, nil
}

// SortIndexByTime returns the entries sorted by generated-at ascending, so a
// caller that accumulated an index out of order can still render a timeline.
func SortIndexByTime(entries []RunIndexEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].GeneratedAt < entries[j].GeneratedAt
	})
}
