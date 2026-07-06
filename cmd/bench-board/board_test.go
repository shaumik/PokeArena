package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestWilson(t *testing.T) {
	// n=0 is a defined zero, not a divide-by-zero.
	if lo, hi := wilson(0, 0); lo != 0 || hi != 0 {
		t.Errorf("wilson(0,0) = (%v,%v), want (0,0)", lo, hi)
	}
	// A clean 50% (5/10) is centered near 0.5 and bounded in [0,1].
	lo, hi := wilson(5, 10)
	if lo < 0 || hi > 1 || !(lo < 0.5 && hi > 0.5) {
		t.Errorf("wilson(5,10) = [%.3f,%.3f], want a valid interval straddling 0.5", lo, hi)
	}
	// A perfect record clamps the upper bound at 1 and keeps a floor below 1.
	lo, hi = wilson(2, 2)
	if math.Abs(hi-1) > 1e-9 || lo <= 0 || lo >= 1 {
		t.Errorf("wilson(2,2) = [%.3f,%.3f], want hi=1 and 0<lo<1", lo, hi)
	}
}

func TestTallyResults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "results.txt")
	os.WriteFile(p, []byte(
		"g1 winner=0 -> AGENT (status=completed)\n"+
			"g2 winner=1 -> AI (status=completed)\n"+
			"g3 winner=0 -> AGENT\n"+
			"g4 winner=-1 -> unfinished (status=live)\n"+
			"noise line without a verdict\n"), 0o644)
	w, l, o := tallyResults(p)
	if w != 2 || l != 1 || o != 1 {
		t.Errorf("tally = (w=%d l=%d o=%d), want (2,1,1)", w, l, o)
	}
	// A missing file is empty, not an error.
	if w, l, o := tallyResults(filepath.Join(dir, "nope.txt")); w+l+o != 0 {
		t.Errorf("missing file should tally to zero, got (%d,%d,%d)", w, l, o)
	}
}

func TestParseAgentic_SkipsAllUnfinished(t *testing.T) {
	dir := t.TempDir()
	// One valid config and one entirely-unfinished config (a quota wall) for
	// the same harness. The walled one must be excluded, not booked as dnf.
	mk := func(name, body string) {
		d := filepath.Join(dir, name)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "results.txt"), []byte(body), 0o644)
	}
	mk("agy-gemini-Genesis", "g1 winner=0\ng2 winner=0\ng3 winner=1\n")             // 2-1
	mk("agy-gemini-Spectrum", "g1 winner=-1\ng2 winner=-1\ng3 winner=-1\n")         // all dnf
	mk("cc-haiku-Genesis", "g1 winner=1\ng2 winner=1\ng3 winner=0\ng4 winner=-1\n") // 1-2 (+1 real dnf)

	rows, err := parseAgentic(dir)
	if err != nil {
		t.Fatalf("parseAgentic: %v", err)
	}
	by := map[string]boardRow{}
	for _, r := range rows {
		by[r.Arm] = r
	}
	agy, ok := by["agentic-agy"]
	if !ok || agy.Wins != 2 || agy.Losses != 1 || agy.Unfinished != 0 {
		t.Errorf("agy = %+v, want 2-1 with 0 dnf (walled config excluded)", agy)
	}
	cc, ok := by["agentic-claude"]
	if !ok || cc.Wins != 1 || cc.Losses != 2 || cc.Unfinished != 1 {
		t.Errorf("claude = %+v, want 1-2 with its 1 real dnf kept", cc)
	}
}

// The committed provenance archive is flat "<config>.txt" files, not subdirs;
// the generator must read that layout too so the board regenerates from what's
// checked in — not only from a live run's directory tree.
func TestParseAgentic_FlatFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "agy-gemini-Genesis.txt"), []byte("g1 winner=0\ng2 winner=0\ng3 winner=1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "cc-haiku-Genesis.txt"), []byte("g1 winner=1\ng2 winner=0\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a config"), 0o644) // ignored

	rows, err := parseAgentic(dir)
	if err != nil {
		t.Fatalf("parseAgentic: %v", err)
	}
	by := map[string]boardRow{}
	for _, r := range rows {
		by[r.Arm] = r
	}
	if agy := by["agentic-agy"]; agy.Wins != 2 || agy.Losses != 1 {
		t.Errorf("agy from flat file = %+v, want 2-1", agy)
	}
	if cc := by["agentic-claude"]; cc.Wins != 1 || cc.Losses != 1 {
		t.Errorf("claude from flat file = %+v, want 1-1", cc)
	}
}

// Different Claude models must land on their own rows, not collapse into one
// "claude" bar — otherwise a Sonnet win rate would silently pollute Haiku's.
func TestParseAgentic_SplitsClaudeModels(t *testing.T) {
	dir := t.TempDir()
	mk := func(name, body string) {
		d := filepath.Join(dir, name)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "results.txt"), []byte(body), 0o644)
	}
	mk("cc-haiku-Genesis", "g1 winner=1\ng2 winner=1\n")               // 0-2
	mk("cc-sonnet-Genesis", "g1 winner=0\ng2 winner=0\ng3 winner=1\n") // 2-1
	mk("cc-opus-Genesis", "g1 winner=0\ng2 winner=0\ng3 winner=0\n")   // 3-0
	mk("cc-sonnet-Keystone", "g1 winner=0\ng2 winner=1\n")             // +1-1 -> sonnet 3-2

	rows, err := parseAgentic(dir)
	if err != nil {
		t.Fatalf("parseAgentic: %v", err)
	}
	byName := map[string]boardRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 distinct claude models: %v", len(rows), byName)
	}
	if r := byName["claude · Haiku 4.5"]; r.Wins != 0 || r.Losses != 2 || r.Sprite != 137 {
		t.Errorf("haiku = %+v, want 0-2 sprite 137", r)
	}
	if r := byName["claude · Sonnet 4.6"]; r.Wins != 3 || r.Losses != 2 || r.Sprite != 233 {
		t.Errorf("sonnet = %+v, want 3-2 (Genesis+Keystone) sprite 233", r)
	}
	if r := byName["claude · Opus 4.8"]; r.Wins != 3 || r.Losses != 0 || r.Sprite != 474 {
		t.Errorf("opus = %+v, want 3-0 sprite 474", r)
	}
	// All three share one harness arm so the legend groups them under one swatch.
	for _, r := range rows {
		if r.Arm != "agentic-claude" {
			t.Errorf("%s has arm %q, want agentic-claude", r.Name, r.Arm)
		}
	}
}

func TestParseBaselineVsRef(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "trace.jsonl")
	// heuristic beats ref twice, loses once; random loses twice; a game not
	// involving ref is ignored; a draw is not counted in the denominator.
	os.WriteFile(p, []byte(
		`{"type":"run"}`+"\n"+
			`{"type":"game","side0":"heuristic","side1":"expectimax@2","winner":"heuristic"}`+"\n"+
			`{"type":"game","side0":"expectimax@2","side1":"heuristic","winner":"heuristic"}`+"\n"+
			`{"type":"game","side0":"heuristic","side1":"expectimax@2","winner":"expectimax@2"}`+"\n"+
			`{"type":"game","side0":"random","side1":"expectimax@2","winner":"expectimax@2"}`+"\n"+
			`{"type":"game","side0":"expectimax@2","side1":"random","winner":"expectimax@2"}`+"\n"+
			`{"type":"game","side0":"random","side1":"heuristic","winner":"heuristic"}`+"\n"+
			`{"type":"game","side0":"heuristic","side1":"expectimax@2","winner":""}`+"\n"), 0o644)

	rows, err := parseBaselineVsRef(p, "expectimax@2")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := map[string]boardRow{}
	for _, r := range rows {
		got[r.Name] = r
	}
	if h, ok := got["heuristic"]; !ok || h.Wins != 2 || h.Losses != 1 {
		t.Errorf("heuristic = %+v, want 2-1 vs ref", got["heuristic"])
	}
	if r, ok := got["random"]; !ok || r.Wins != 0 || r.Losses != 2 {
		t.Errorf("random = %+v, want 0-2 vs ref", got["random"])
	}
	// The reference itself is never a row.
	if _, ok := got["expectimax@2"]; ok {
		t.Errorf("reference should not appear as a contestant")
	}
}
