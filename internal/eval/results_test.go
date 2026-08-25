package eval

import (
	"path/filepath"
	"testing"

	"github.com/shaumik/PokeArena/internal/usage"
)

// mkMatch builds a synthetic two-contestant match with the given per-seat token
// usage on each game, so the store can be tested without playing real battles.
func mkMatch(a, b string, winsA, winsB int, ua, ub usage.Usage) MatchResult {
	m := MatchResult{A: a, B: b, Team: "T", AWins: winsA, BWins: winsB}
	for i := 0; i < winsA+winsB; i++ {
		winner := a
		if i >= winsA {
			winner = b
		}
		var res GameResult
		res.Usage[0] = ua
		res.Usage[1] = ub
		m.Games = append(m.Games, GameRecord{
			Match: a + "-vs-" + b, Team: "T", Side0: a, Side1: b, Winner: winner, Result: res,
		})
	}
	return m
}

func TestBuildRunRecord_CostAttribution(t *testing.T) {
	// llm spent tokens each game; heuristic is deterministic (free).
	perGame := usage.Usage{InputTokens: 1000, OutputTokens: 100}
	matches := []MatchResult{mkMatch("llm", "heuristic", 6, 4, perGame, usage.Usage{})}

	header := RunHeader{Timestamp: "2026-07-03T15:30:00Z", EngineRevision: "abc", Contestants: []string{"llm", "heuristic"}, GamesPerPairing: 5}
	models := map[string]string{"llm": "claude-haiku-4-5-20251001"}
	pricing := map[string]usage.Pricing{"claude-haiku-4-5-20251001": {Input: 1.0, Output: 5.0}}

	rec := BuildRunRecord(header, matches, models, nil, pricing)

	byName := map[string]ContestantResult{}
	for _, c := range rec.Contestants {
		byName[c.Name] = c
	}
	llm, heur := byName["llm"], byName["heuristic"]

	// 10 games, 1000 in + 100 out each => 10000 in, 1000 out.
	wantUsage := usage.Usage{InputTokens: 10000, OutputTokens: 1000}
	if llm.Usage != wantUsage {
		t.Fatalf("llm usage = %+v, want %+v", llm.Usage, wantUsage)
	}
	// cost = 10000/1e6*1 + 1000/1e6*5 = 0.01 + 0.005 = 0.015
	if !llm.CostKnown || abs(llm.CostUSD-0.015) > 1e-9 {
		t.Fatalf("llm cost = %v (known=%v), want 0.015", llm.CostUSD, llm.CostKnown)
	}
	if abs(llm.CostPerGameUSD-0.0015) > 1e-9 {
		t.Fatalf("llm cost/game = %v, want 0.0015", llm.CostPerGameUSD)
	}
	// Deterministic agent: free and known.
	if !heur.CostKnown || heur.CostUSD != 0 || !heur.Usage.IsZero() {
		t.Fatalf("heuristic should be free+known, got %+v", heur)
	}
	if abs(rec.TotalCostUSD-0.015) > 1e-9 {
		t.Fatalf("total cost = %v, want 0.015", rec.TotalCostUSD)
	}
	if rec.RunID == "" {
		t.Fatal("empty run id")
	}
}

// A model that spent tokens but has no price must be flagged unknown, never
// silently reported as free.
func TestBuildRunRecord_UnknownPriceIsNotFree(t *testing.T) {
	matches := []MatchResult{mkMatch("mystery", "heuristic", 5, 5, usage.Usage{InputTokens: 500}, usage.Usage{})}
	header := RunHeader{Timestamp: "2026-07-03T15:30:00Z", Contestants: []string{"mystery", "heuristic"}}
	rec := BuildRunRecord(header, matches, map[string]string{"mystery": "unpriced-model"}, nil, map[string]usage.Pricing{})

	for _, c := range rec.Contestants {
		if c.Name == "mystery" {
			if c.CostKnown {
				t.Fatal("unpriced model must be cost-unknown, not free")
			}
			if c.Usage.IsZero() {
				t.Fatal("mystery should still record its tokens")
			}
		}
	}
}

// A model-backed contestant whose measured usage summed to ZERO (e.g. every
// call errored after billing, or an adapter that under-reported) must NOT be
// marked free just because it has no tokens. Cost-known-ness keys on the model
// id, not token presence: an unpriced zero-usage model stays CostKnown=false, a
// priced one reports an honest $0 — neither masquerades as a free agent.
func TestBuildRunRecord_ModelBackedZeroUsageIsNotFree(t *testing.T) {
	zero := usage.Usage{}
	matches := []MatchResult{mkMatch("ghost", "heuristic", 5, 5, zero, zero)}
	header := RunHeader{Timestamp: "2026-07-06T00:00:00Z", Contestants: []string{"ghost", "heuristic"}}

	// Unpriced: must be cost-unknown, not free.
	unpriced := BuildRunRecord(header, matches, map[string]string{"ghost": "some-model"}, nil, map[string]usage.Pricing{})
	for _, c := range unpriced.Contestants {
		if c.Name == "ghost" && c.CostKnown {
			t.Fatal("model-backed zero-usage contestant with no price must be CostKnown=false, not free")
		}
		if c.Name == "heuristic" && (!c.CostKnown || c.CostUSD != 0) {
			t.Fatal("deterministic agent must stay free+known")
		}
	}

	// Priced: known cost of exactly $0 (honest), still distinct from a free agent
	// because it carries a model id.
	priced := BuildRunRecord(header, matches, map[string]string{"ghost": "some-model"}, nil,
		map[string]usage.Pricing{"some-model": {Input: 1, Output: 5}})
	for _, c := range priced.Contestants {
		if c.Name == "ghost" && (!c.CostKnown || c.CostUSD != 0) {
			t.Fatalf("priced zero-usage model should be CostKnown=true, $0, got known=%v cost=%v", c.CostKnown, c.CostUSD)
		}
	}
}

// A multi-team run records a per-team Elo breakdown in team order; a single
// team run omits it (no cross-team story to tell).
func TestBuildRunRecord_PerTeamBreakdown(t *testing.T) {
	free := usage.Usage{}
	// Two teams; on team "Alpha" a beats b, on "Bravo" b beats a — the exact
	// team-dependence the breakdown exists to surface.
	alpha := mkMatch("a", "b", 8, 2, free, free)
	alpha.Team = "Alpha"
	for i := range alpha.Games {
		alpha.Games[i].Team = "Alpha"
	}
	bravo := mkMatch("a", "b", 2, 8, free, free)
	bravo.Team = "Bravo"
	for i := range bravo.Games {
		bravo.Games[i].Team = "Bravo"
	}

	header := RunHeader{Timestamp: "2026-07-03T15:30:00Z", Contestants: []string{"a", "b"}}
	rec := BuildRunRecord(header, []MatchResult{alpha, bravo}, nil, nil, nil)

	if len(rec.PerTeam) != 2 {
		t.Fatalf("want 2 per-team rankings, got %d", len(rec.PerTeam))
	}
	if rec.PerTeam[0].Team != "Alpha" || rec.PerTeam[1].Team != "Bravo" {
		t.Fatalf("per-team not in appearance order: %+v", rec.PerTeam)
	}
	if rec.PerTeam[0].Ranks[0].Name != "a" {
		t.Fatalf("Alpha should rank a first, got %+v", rec.PerTeam[0].Ranks)
	}
	if rec.PerTeam[1].Ranks[0].Name != "b" {
		t.Fatalf("Bravo should rank b first, got %+v", rec.PerTeam[1].Ranks)
	}

	// Single-team run: no per-team breakdown.
	solo := BuildRunRecord(header, []MatchResult{mkMatch("a", "b", 5, 5, free, free)}, nil, nil, nil)
	if solo.PerTeam != nil {
		t.Fatalf("single-team run should omit per-team, got %+v", solo.PerTeam)
	}
}

func TestSaveRun_AndLoadIndex(t *testing.T) {
	dir := t.TempDir()
	perGame := usage.Usage{InputTokens: 1000, OutputTokens: 100}
	matches := []MatchResult{mkMatch("llm", "heuristic", 6, 4, perGame, usage.Usage{})}
	header := RunHeader{Timestamp: "2026-07-03T15:30:00Z", EngineRevision: "abc", DataSimVersion: "0.10.9", TeamLibrary: "v1", Contestants: []string{"llm", "heuristic"}, GamesPerPairing: 5}
	rec := BuildRunRecord(header, matches, map[string]string{"llm": "claude-haiku-4-5-20251001"}, nil, map[string]usage.Pricing{"claude-haiku-4-5-20251001": {Input: 1, Output: 5}})

	path, err := SaveRun(dir, rec)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("run file %q not in %q", path, dir)
	}

	// Second run appends, so the index has two chronological entries.
	if _, err := SaveRun(dir, rec); err != nil {
		t.Fatalf("SaveRun 2: %v", err)
	}
	entries, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("index has %d entries, want 2", len(entries))
	}
	e := entries[0]
	if e.RunID != rec.RunID || e.TeamLibrary != "v1" || len(e.Standings) != 2 {
		t.Fatalf("index entry did not round-trip: %+v", e)
	}
	if abs(e.TotalCostUSD-0.015) > 1e-9 {
		t.Fatalf("index total cost = %v, want 0.015", e.TotalCostUSD)
	}
}

// LoadIndex on a fresh dir is empty, not an error — no runs recorded yet.
func TestLoadIndex_MissingIsEmpty(t *testing.T) {
	entries, err := LoadIndex(t.TempDir())
	if err != nil {
		t.Fatalf("LoadIndex on empty dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 entries, got %d", len(entries))
	}
}

// LoadPricing reads the shipped table, skips the _note metadata key, and parses
// the underscore-keyed rate fields.
func TestLoadPricing_ShippedTable(t *testing.T) {
	table, err := LoadPricing("../../data/model-pricing.json")
	if err != nil {
		t.Fatalf("LoadPricing: %v", err)
	}
	if _, ok := table["_note"]; ok {
		t.Fatal("metadata key _note must be skipped")
	}
	haiku, ok := table["claude-haiku-4-5-20251001"]
	if !ok {
		t.Fatal("haiku pricing missing")
	}
	if haiku.CacheRead <= 0 || haiku.CacheRead >= haiku.Input {
		t.Fatalf("cache_read %.3f should be a positive discount vs input %.3f", haiku.CacheRead, haiku.Input)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
