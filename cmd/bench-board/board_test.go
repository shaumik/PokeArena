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
