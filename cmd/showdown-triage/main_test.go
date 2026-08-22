package main

import (
	"strings"
	"testing"
)

// The two things worth testing here are the two that destroy the ledger
// silently.
//
// parseLedger has to find the end of a Go map literal without a Go parser. A
// brace inside a reason string ("Wish { } heals") would end the map early for
// a naive scan, and the rewrite would then delete every row after it — a
// hundred quarantine entries gone, and the next run would report them all as
// fresh regressions.
//
// merge has to leave hand-written reasons alone. The whole point of the ledger
// is the sentence somebody wrote after diagnosing a failure; a merge that
// overwrites it with the failure text throws away the diagnosis and leaves the
// row looking triaged.

const sampleLedger = `//go:build showdown

package showdown

var gaps = map[string]gap{
	"Intimidate: should decrease Atk by 1 level": {Kind: gapBug, Why: "fires before the switch-in log, so the order is wrong"},
	"Wish: should heal": {Kind: gapMissing, Why: "no Wish { } in the dataset"},
	"Transform: should copy": {Kind: gapMissing, Why: "move \"transform\" is not in this dataset"},
}

func other() {}
`

func TestParseLedgerSurvivesBracesInReasons(t *testing.T) {
	rows, start, end, err := parseLedger(sampleLedger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("parsed %d rows, want 3: %+v", len(rows), rows)
	}
	if got := rows["Wish: should heal"]; got.Kind != "gapMissing" || !strings.Contains(got.Why, "{ }") {
		t.Errorf("the braced reason came back as %+v", got)
	}
	// The bounds must span exactly the map body, so the rewrite leaves
	// everything after it — here `func other()` — untouched.
	tail := sampleLedger[end:]
	if !strings.HasPrefix(tail, "}\n\nfunc other()") {
		t.Errorf("the map end was located at the wrong place; tail begins %q", truncate(tail, 40))
	}
	if !strings.HasSuffix(sampleLedger[:start], "gap{") {
		t.Errorf("the map start was located at the wrong place")
	}
}

// TestParseLedgerSurvivesQuotesInReasons is the sharper half of the same
// worry. summarize quotes the thing that went wrong, so most generated reasons
// contain an escaped quote; a row pattern that stops at the first one parses a
// fragment, fails to unquote it, and drops the row silently. The next -write
// then deletes it, and the case comes back as a fresh regression with the
// diagnosis gone.
func TestParseLedgerSurvivesQuotesInReasons(t *testing.T) {
	rows, _, _, err := parseLedger(sampleLedger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, ok := rows["Transform: should copy"]
	if !ok {
		t.Fatalf("the row whose reason contains quotes was dropped; parsed keys: %v", keysOf(rows))
	}
	if got.Why != `move "transform" is not in this dataset` {
		t.Errorf("reason came back as %q", got.Why)
	}
}

func keysOf(rows map[string]row) []string {
	out := make([]string, 0, len(rows))
	for k := range rows {
		out = append(out, k)
	}
	return out
}

func TestParseLedgerRefusesAnUnclosedMap(t *testing.T) {
	if _, _, _, err := parseLedger("var gaps = map[string]gap{\n\t\"a: b\": {Kind: gapBug, Why: \"x\"},\n"); err == nil {
		t.Fatal("an unterminated map parsed without error, which would truncate the file on write")
	}
	if _, _, _, err := parseLedger("package showdown\n"); err == nil {
		t.Fatal("a file with no gaps map parsed without error")
	}
}

func TestRoundTripIsStable(t *testing.T) {
	rows, start, end, err := parseLedger(sampleLedger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rebuilt := sampleLedger[:start] + render(rows) + sampleLedger[end:]
	again, _, _, err := parseLedger(rebuilt)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(again) != len(rows) {
		t.Fatalf("round trip lost rows: %d → %d", len(rows), len(again))
	}
	for k, v := range rows {
		if again[k] != v {
			t.Errorf("%q round-tripped as %+v, want %+v", k, again[k], v)
		}
	}
	// Rendering twice must be byte-identical, or every triage run produces a
	// diff whether or not anything changed.
	if render(rows) != render(again) {
		t.Error("render is not stable across runs")
	}
	// And a freshly generated reason — the common case, quotes and all — must
	// survive a render/parse cycle, since that is what -write does to it.
	generated := map[string]row{
		"X: y": {Key: "X: y", Kind: "gapMissing", Why: summarize([]string{`seed 1: move "transform" is not in this dataset`})},
	}
	cycled, _, _, err := parseLedger("var gaps = map[string]gap{" + render(generated) + "}\n")
	if err != nil {
		t.Fatalf("a generated row did not re-parse: %v", err)
	}
	if cycled["X: y"] != generated["X: y"] {
		t.Errorf("a generated row round-tripped as %+v, want %+v", cycled["X: y"], generated["X: y"])
	}
}

func TestMergeKeepsHandWrittenReasons(t *testing.T) {
	existing := map[string]row{
		"A: keeps its reason": {Key: "A: keeps its reason", Kind: "gapScope", Why: "singles has no ally"},
		"B: passes now":       {Key: "B: passes now", Kind: "gapBug", Why: "was wrong, now fixed"},
	}
	outs := []outcome{
		{Key: "A: keeps its reason", Status: "gap", Detail: []string{"seed 1: some raw failure text"}},
		{Key: "B: passes now", Status: "stale"},
		{Key: "C: newly failing", Status: "regress", Detail: []string{"seed 1: Snorlax is at 90/100 HP, want full"}},
		{Key: "D: fine", Status: "pass"},
	}

	next, added, removed := merge(existing, outs)

	if got := next["A: keeps its reason"].Why; got != "singles has no ally" {
		t.Errorf("a listed-and-still-failing row was rewritten to %q", got)
	}
	if _, still := next["B: passes now"]; still {
		t.Error("a row whose case now passes was not removed")
	}
	if len(removed) != 1 || removed[0] != "B: passes now" {
		t.Errorf("removed = %v", removed)
	}
	if len(added) != 1 || added[0].Key != "C: newly failing" {
		t.Fatalf("added = %+v", added)
	}
	if w := added[0].Why; strings.HasPrefix(w, "seed ") {
		t.Errorf("the seed prefix survived into the reason: %q", w)
	}
	if _, in := next["D: fine"]; in {
		t.Error("a passing case was added to the ledger")
	}
}

func TestGuessKindSeparatesWhatItCan(t *testing.T) {
	cases := []struct {
		detail, want string
	}{
		{`move "transform" is not in this dataset`, "gapMissing"},
		{`item "eviolite" is not in this dataset`, "gapMissing"},
		{`p1 slot 1: ability "normalize" — the engine has no record of this ability at all`, "gapMissing"},
		{`p1 slot 1: item "mail" — the engine models no behavior for this item`, "gapMissing"},
		{`ability "unnerve" — registered but inert: needs the foe's berries suppressed`, "gapMissing"},
		{`species "Iron Valiant" is not in this dex and has no stand-in`, "gapPort"},
		{`Snorlax does not know "earthquake" (it knows splash)`, "gapPort"},
		{`makeChoices("move x", "") after the battle already ended`, "gapPort"},
		{`Gengar is at 90/100 HP, want full`, "gapBug"},
		{`panic: runtime error: index out of range`, "gapBug"},
		{``, "gapBug"},
	}
	for _, c := range cases {
		if got := guessKind([]string{c.detail}); got != c.want {
			t.Errorf("guessKind(%q) = %s, want %s", truncate(c.detail, 50), got, c.want)
		}
	}
}

func TestSummarizeTrimsToOneScannableLine(t *testing.T) {
	got := summarize([]string{"seed 3: Snorlax is at 90/100 HP, want full\nwith a second line", "another failure"})
	if strings.Contains(got, "\n") {
		t.Errorf("the reason spans lines: %q", got)
	}
	if strings.HasPrefix(got, "seed ") {
		t.Errorf("the seed prefix survived: %q", got)
	}
	if !strings.Contains(got, "(+1 more)") {
		t.Errorf("the extra failures were not counted: %q", got)
	}
	long := summarize([]string{strings.Repeat("x", 400)})
	if len(long) > 170 {
		t.Errorf("a long reason was not truncated: %d chars", len(long))
	}
	if got := summarize(nil); got == "" {
		t.Error("a failure with no detail produced an empty reason")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
