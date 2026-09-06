package eval

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shaumik/PokeArena/internal/usage"
)

// TestRenderHTMLReport checks the report is a self-contained page that carries
// the run's headline numbers, the abstract, and per-team breakdown — and pulls
// in no external assets.
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
		"<!DOCTYPE html>", rec.RunID, // run id still identifies the page (title)
		"simultaneous-move", "Bradley-Terry", "Wilson 95%", // the method abstract
		"llm", "heuristic", // contestants
		"Alpha", "Bravo", // per-team breakdown present
		"L50", "mirror match", // ruleset rendered as pills
		"github.com/shaumik/PokeArena", // link back to the source
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("report missing %q", want)
		}
	}
	// Self-contained: renders offline. A navigation <a href> is fine; what is
	// banned is anything that fetches an asset — stylesheet, script, font, image.
	for _, bad := range []string{"<script", "src=\"http", "src='http", "<link ", "@import", "url(http", "githubusercontent"} {
		if strings.Contains(html, bad) {
			t.Fatalf("report should be self-contained, found %q", bad)
		}
	}
	// The CI bar must be positioned from the interval, not hard-coded.
	if !strings.Contains(html, "width:") || !strings.Contains(html, "left:") {
		t.Fatal("report missing confidence-interval bar geometry")
	}
}

// TestBuildReportViewSamples checks a model-backed contestant's reconstructed
// replays are grouped into a per-team win/loss sample strip (ordered by team,
// win before loss), and that a baseline replay (not model-backed) is excluded.
func TestBuildReportViewSamples(t *testing.T) {
	rec := RunRecord{
		Contestants: []ContestantResult{
			{Name: "Claude Opus 4.8", Model: "claude-opus-4-8", Games: 10, Wins: 6},
			{Name: "heuristic", Model: "", Games: 0},
			{Name: "expectimax-d1", Model: "", Games: 10, Wins: 4},
		},
		Replays: []Replay{
			{Side0: "Claude Opus 4.8", Side1: "heuristic", Team: "Keystone", Winner: "Claude Opus 4.8"}, // win
			{Side0: "Claude Opus 4.8", Side1: "heuristic", Team: "Genesis", Winner: "heuristic"},        // loss
			{Side0: "Claude Opus 4.8", Side1: "heuristic", Team: "Genesis", Winner: "Claude Opus 4.8"},  // win
			{Side0: "expectimax-d1", Side1: "heuristic", Team: "Genesis", Winner: "expectimax-d1"},      // baseline: excluded
		},
	}

	v := buildReportView(rec)
	if !v.HasSamples {
		t.Fatal("expected HasSamples with model replays present")
	}
	var got map[string][]sampleChip
	if err := json.Unmarshal([]byte(v.SamplesJSON), &got); err != nil {
		t.Fatalf("samples JSON: %v", err)
	}
	if _, ok := got["expectimax-d1"]; ok {
		t.Error("baseline (non-model) contestant should not get a sample strip")
	}
	opus := got["Claude Opus 4.8"]
	if len(opus) != 3 {
		t.Fatalf("want 3 Opus samples, got %d: %+v", len(opus), opus)
	}
	// Ordered by team, then win before loss: Genesis W, Genesis L, Keystone W.
	want := []sampleChip{
		{Team: "Genesis", Outcome: "win", Replay: 2},
		{Team: "Genesis", Outcome: "loss", Replay: 1},
		{Team: "Keystone", Outcome: "win", Replay: 0},
	}
	for i, w := range want {
		if opus[i] != w {
			t.Errorf("sample[%d] = %+v, want %+v", i, opus[i], w)
		}
	}
}

// TestReportSpritesInline checks a revealed roster's sprites are embedded as
// base64 data: URIs (so the report shows them while fetching nothing), and that
// the sprite-bearing report never reaches out to the network for an asset.
func TestReportSpritesInline(t *testing.T) {
	rec := RunRecord{
		Contestants: []ContestantResult{
			{Name: "Claude Opus 4.8", Model: "claude-opus-4-8", Games: 1, Wins: 1},
			{Name: "heuristic", Model: ""},
		},
		Rosters: []TeamRoster{{
			Name:    "Keystone",
			Members: []RosterMon{{Name: "Mewtwo", Types: "Psychic", BST: 680, DexNo: 150}},
		}},
	}

	v := buildReportView(rec)
	if !v.HasSprites {
		t.Fatal("expected HasSprites for a roster with a vendored dex number")
	}
	if !strings.Contains(string(v.SpritesJSON), "data:image/png;base64,") {
		t.Fatalf("sprite should be an inlined data URI, got %q", v.SpritesJSON)
	}

	var sb strings.Builder
	if err := RenderHTMLReport(&sb, rec); err != nil {
		t.Fatalf("RenderHTMLReport: %v", err)
	}
	html := sb.String()
	if !strings.Contains(html, "data:image/png;base64,") {
		t.Fatal("rendered report should carry the inlined sprite")
	}
	for _, bad := range []string{"src=\"http", "src='http", "<link ", "@import", "url(http", "githubusercontent"} {
		if strings.Contains(html, bad) {
			t.Fatalf("sprite report should be self-contained, found %q", bad)
		}
	}
}

// TestReportDecisionQuality checks the decision-quality section renders from a
// record's ModelStats: the models sort cleanest-first (lowest blunder rate),
// the headline is tagged, and the rates are shown as percentages. It also guards
// that the section stays self-contained (adds no fetched asset or script).
func TestReportDecisionQuality(t *testing.T) {
	rec := RunRecord{
		DecisionQuality: []ModelStats{
			// Deliberately out of order to prove buildReportView sorts by blunder rate.
			{Model: "Claude Haiku 4.5", Games: 12, WinRate: 0.08, Decisions: 200, BlunderRate: 0.29, MatchRate: 0.32, MedianRegret: 91},
			{Model: "Gemini 3.1 Pro", Games: 12, WinRate: 0.67, Decisions: 273, BlunderRate: 0.21, MatchRate: 0.32, MedianRegret: 47},
		},
	}

	v := buildReportView(rec)
	if !v.HasDecisionQuality || len(v.DQRows) != 2 {
		t.Fatalf("expected 2 decision-quality rows, got has=%v n=%d", v.HasDecisionQuality, len(v.DQRows))
	}
	// Lowest blunder rate first, and only that row is flagged the cleanest.
	if v.DQRows[0].Model != "Gemini 3.1 Pro" || !v.DQRows[0].Best {
		t.Fatalf("cleanest row = %q best=%v, want Gemini best", v.DQRows[0].Model, v.DQRows[0].Best)
	}
	if v.DQRows[1].Best {
		t.Fatal("only the lowest-blunder row should be marked best")
	}
	if v.DQRows[0].BlunderRate != "21%" || v.DQRows[0].WinRate != "67%" {
		t.Fatalf("Gemini formatted rates = blunder %q win %q, want 21%% / 67%%", v.DQRows[0].BlunderRate, v.DQRows[0].WinRate)
	}

	var sb strings.Builder
	if err := RenderHTMLReport(&sb, rec); err != nil {
		t.Fatalf("RenderHTMLReport: %v", err)
	}
	html := sb.String()
	for _, want := range []string{"Decision quality", "blunder rate", "Gemini 3.1 Pro", "cleanest", "21%"} {
		if !strings.Contains(html, want) {
			t.Fatalf("decision-quality section missing %q", want)
		}
	}
	for _, bad := range []string{"src=\"http", "src='http", "<link ", "@import", "url(http", "githubusercontent"} {
		if strings.Contains(html, bad) {
			t.Fatalf("decision-quality report should be self-contained, found %q", bad)
		}
	}
}

func tagTeam(games []GameRecord, team string) []GameRecord {
	for i := range games {
		games[i].Team = team
	}
	return games
}
