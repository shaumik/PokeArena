package eval

import (
	"strings"
	"testing"

	"pokearena/internal/usage"
)

// TestRenderHTMLReport checks the report is a self-contained page that carries
// the run's headline numbers, provenance, and per-team breakdown — and pulls in
// no external assets.
func TestRenderHTMLReport(t *testing.T) {
	perGame := usage.Usage{InputTokens: 1000, OutputTokens: 100}
	alpha := mkMatch("llm", "heuristic", 7, 3, perGame, usage.Usage{})
	alpha.Team, alpha.Games = "Alpha", tagTeam(alpha.Games, "Alpha")
	bravo := mkMatch("llm", "heuristic", 4, 6, perGame, usage.Usage{})
	bravo.Team, bravo.Games = "Bravo", tagTeam(bravo.Games, "Bravo")

	header := RunHeader{
		Timestamp: "2026-07-03T15:30:00Z", EngineRevision: "abc123def456",
		DataSimVersion: "0.10.9", DataSourceGen: 9, DataCurationSHA: "c111917",
		Ruleset: Ruleset(), TeamLibrary: "v1", Teams: []string{"Alpha", "Bravo"},
		Contestants: []string{"llm", "heuristic"}, GamesPerPairing: 5, Orientations: 2, Seeds: "0..4",
	}
	rec := BuildRunRecord(header, []MatchResult{alpha, bravo},
		map[string]string{"llm": "claude-haiku-4-5-20251001"},
		map[string]string{"llm": "cot"},
		map[string]usage.Pricing{"claude-haiku-4-5-20251001": {Input: 1, Output: 5}})

	var sb strings.Builder
	if err := RenderHTMLReport(&sb, rec); err != nil {
		t.Fatalf("RenderHTMLReport: %v", err)
	}
	html := sb.String()

	for _, want := range []string{
		"<!DOCTYPE html>", rec.RunID, "abc123def456", // provenance
		"llm", "heuristic", // contestants
		"Alpha", "Bravo", // per-team breakdown present
		Ruleset(), "0..4", // ruleset + seeds
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("report missing %q", want)
		}
	}
	// Self-contained: no external stylesheet/script/font references.
	for _, bad := range []string{"http://", "https://", "<script", "src="} {
		if strings.Contains(html, bad) {
			t.Fatalf("report should be self-contained, found %q", bad)
		}
	}
	// The CI bar must be positioned from the interval, not hard-coded.
	if !strings.Contains(html, "width:") || !strings.Contains(html, "left:") {
		t.Fatal("report missing confidence-interval bar geometry")
	}
}

func tagTeam(games []GameRecord, team string) []GameRecord {
	for i := range games {
		games[i].Team = team
	}
	return games
}
