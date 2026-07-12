package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBaselineMatches checks the round-robin trace parses into one MatchResult
// per pairing per team, with the A/B roles taken from the "A-vs-B" match name so
// the two orientations aggregate onto one record, and per-game turns preserved.
func TestBaselineMatches(t *testing.T) {
	dir := t.TempDir()
	trace := filepath.Join(dir, "arm1.jsonl")
	lines := []string{
		`{"type":"run","engine_revision":"x"}`,
		`{"type":"game","match":"random-vs-heuristic","team":"Genesis","seed":0,"side0":"random","side1":"heuristic","winner":"heuristic","turns":28}`,
		`{"type":"game","match":"random-vs-heuristic","team":"Genesis","seed":1,"side0":"heuristic","side1":"random","winner":"random","turns":40}`,
		`{"type":"game","match":"random-vs-heuristic","team":"Spectrum","seed":0,"side0":"random","side1":"heuristic","winner":"heuristic","turns":15}`,
	}
	if err := os.WriteFile(trace, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := baselineMatches(trace)
	if err != nil {
		t.Fatalf("baselineMatches: %v", err)
	}
	// Two records: the pairing on Genesis (two games) and on Spectrum (one game).
	if len(got) != 2 {
		t.Fatalf("want 2 match records (per team), got %d: %+v", len(got), got)
	}
	var genesis *MatchResult
	for i := range got {
		if got[i].Team == "Genesis" {
			genesis = &got[i]
		}
	}
	if genesis == nil {
		t.Fatal("no Genesis record")
	}
	// A=random, B=heuristic (from the match name); heuristic won once, random once.
	if genesis.A != "random" || genesis.B != "heuristic" {
		t.Errorf("roles: A=%q B=%q, want random/heuristic", genesis.A, genesis.B)
	}
	if genesis.AWins != 1 || genesis.BWins != 1 {
		t.Errorf("Genesis record want 1-1, got %d-%d", genesis.AWins, genesis.BWins)
	}
	if len(genesis.Games) != 2 {
		t.Fatalf("want 2 game records, got %d", len(genesis.Games))
	}
	if genesis.Games[0].Result.Turns != 28 {
		t.Errorf("turns not preserved: %d", genesis.Games[0].Result.Turns)
	}
}

// TestAgenticMatches checks each "<model>-<team>" results dir becomes one
// MatchResult vs the reference (decided games only) with the display-name -> id
// map populated, and an all-undecided cell dropped rather than booked 0-0.
func TestAgenticMatches(t *testing.T) {
	dir := t.TempDir()
	write := func(cell, body string) {
		d := filepath.Join(dir, cell)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "results.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cc-haiku-Genesis", "g1 winner=0 -> AGENT\ng2 winner=1 -> AI\ng3 winner=-1 -> abandoned\n")
	write("agy-gemini-Spectrum", "g1 winner=-1 -> abandoned\n") // decided nothing: dropped

	matches, models, err := agenticMatches(dir, "heuristic")
	if err != nil {
		t.Fatalf("agenticMatches: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("want 1 match (undecided cell dropped), got %d: %+v", len(matches), matches)
	}
	m := matches[0]
	if m.A != "Claude Haiku 4.5" || m.B != "heuristic" || m.Team != "Genesis" {
		t.Errorf("match = %+v, want Claude Haiku 4.5 vs heuristic on Genesis", m)
	}
	if m.AWins != 1 || m.BWins != 1 { // undecided game dropped
		t.Errorf("record want 1-1 (undecided dropped), got %d-%d", m.AWins, m.BWins)
	}
	if models["Claude Haiku 4.5"] != "claude-haiku-4-5" {
		t.Errorf("model id = %q", models["Claude Haiku 4.5"])
	}
}

// TestBuildVsReferenceRecord checks both arms fold into one full-featured record:
// baselines and models share one ladder (models marked model-backed), the
// head-to-head matrix and rosters are populated, and the record renders through
// the standard report as a self-contained page.
func TestBuildVsReferenceRecord(t *testing.T) {
	dex := loadDex(t)
	lib, err := LoadTeamLibrary("../../data/benchmark-teams.json", dex)
	if err != nil {
		t.Fatalf("load teams: %v", err)
	}
	team := lib.Teams[0].Name

	dir := t.TempDir()
	trace := filepath.Join(dir, "arm1.jsonl")
	lines := []string{
		`{"type":"game","match":"random-vs-heuristic","team":"` + team + `","seed":0,"side0":"random","side1":"heuristic","winner":"heuristic","turns":28}`,
		`{"type":"game","match":"expectimax-d1-vs-heuristic","team":"` + team + `","seed":0,"side0":"expectimax-d1","side1":"heuristic","winner":"expectimax-d1","turns":30}`,
		`{"type":"game","match":"expectimax-d1-vs-random","team":"` + team + `","seed":0,"side0":"expectimax-d1","side1":"random","winner":"expectimax-d1","turns":20}`,
	}
	if err := os.WriteFile(trace, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ag := filepath.Join(dir, "agentic")
	cell := filepath.Join(ag, "cc-haiku-"+team)
	if err := os.MkdirAll(cell, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cell, "results.txt"), []byte("g1 winner=0\ng2 winner=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := BuildVsReferenceRecord(dex, trace, ag, "heuristic", lib.Teams, RunHeader{Timestamp: "2026-07-08T00:00:00Z"})
	if err != nil {
		t.Fatalf("BuildVsReferenceRecord: %v", err)
	}

	byName := map[string]ContestantResult{}
	for _, c := range rec.Contestants {
		byName[c.Name] = c
	}
	for _, want := range []string{"heuristic", "expectimax-d1", "random", "Claude Haiku 4.5"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("contestant %q missing from ladder %v", want, keysOf(byName))
		}
	}
	// The model is marked model-backed; the baselines are deterministic.
	if byName["Claude Haiku 4.5"].Model != "claude-haiku-4-5" {
		t.Errorf("Claude Haiku model id = %q", byName["Claude Haiku 4.5"].Model)
	}
	if byName["heuristic"].Model != "" {
		t.Errorf("heuristic should be deterministic, got model %q", byName["heuristic"].Model)
	}
	// The win-rate bar is restated as head-to-head vs the reference: expectimax-d1
	// beat the heuristic once (its win over random does not count here), and the
	// reference itself is the 0-game 50% yardstick.
	if d1 := byName["expectimax-d1"]; d1.Games != 1 || d1.Wins != 1 || d1.WinRate != 1.0 {
		t.Errorf("expectimax-d1 vs heuristic want 1-0 (n=1, 100%%), got %d-%d n=%d wr=%.2f", d1.Wins, d1.Losses, d1.Games, d1.WinRate)
	}
	if h := byName["heuristic"]; h.Games != 0 || h.WinRate != 0.5 {
		t.Errorf("heuristic should be the 0-game 50%% reference, got n=%d wr=%.2f", h.Games, h.WinRate)
	}
	// Full-report features are populated.
	if rec.Matrix == nil || len(rec.Matrix.Agents) == 0 {
		t.Error("matrix not populated")
	}
	if len(rec.Rosters) == 0 {
		t.Error("rosters not populated")
	}

	var sb strings.Builder
	if err := RenderHTMLReport(&sb, rec); err != nil {
		t.Fatalf("RenderHTMLReport: %v", err)
	}
	for _, bad := range []string{"src=\"http", "src='http", "<link ", "@import", "url(http", "githubusercontent"} {
		if strings.Contains(sb.String(), bad) {
			t.Fatalf("report should be self-contained, found %q", bad)
		}
	}
}

// TestModelDisplay pins the config-key -> (display name, model id) mapping. It is
// the single source of truth shared by the report (contestant + matrix labels)
// and db-replay (a reconstructed replay's trainer name), so a drift here would
// silently stop a model's replay from attaching to its matrix cell.
func TestModelDisplay(t *testing.T) {
	cases := []struct {
		key, name, id string
	}{
		{"cc-haiku", "Claude Haiku 4.5", "claude-haiku-4-5"},
		{"cc-sonnet", "Claude Sonnet 4.6", "claude-sonnet-4-6"},
		{"cc-opus", "Claude Opus 4.8", "claude-opus-4-8"},
		{"agy-gemini", "Gemini 3.1 Pro", "gemini-3.1-pro"},
		{"heuristic", "heuristic", ""}, // a non-model key passes through with an empty id
	}
	for _, c := range cases {
		name, id := ModelDisplay(c.key)
		if name != c.name || id != c.id {
			t.Errorf("ModelDisplay(%q) = (%q, %q), want (%q, %q)", c.key, name, id, c.name, c.id)
		}
	}
}

func keysOf(m map[string]ContestantResult) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
