// Command showdown-triage turns a run of the Showdown port into the ledger it
// should have been reconciled against.
//
// The port suite (internal/engine/showdown, docs/showdown-port.md) is expected
// to be partly red, and the file that keeps that honest is the `gaps` map in
// gaps_test.go: one row per case that does not pass, saying why. Writing two
// thousand of those by hand is not a plan, and neither is letting the suite
// print failures nobody classifies. This closes the loop:
//
//	make test-showdown-report            # writes showdown-report.json
//	go run ./cmd/showdown-triage -write  # folds the run into gaps_test.go
//
// What it does, and does not do:
//
//   - A case that failed while unlisted gets a new row, with a *provisional*
//     kind guessed from the failure text and the failure text kept as the
//     reason. The guess is a starting point for review, never the end of it —
//     the difference between "the engine is wrong" and "the translation is
//     wrong" is not visible in the message.
//   - A case that passed while listed has its row deleted. That is the whole
//     value of the stale check: the ledger prunes itself.
//   - A case that failed while listed keeps its row **verbatim**, including
//     any reason a human rewrote. Nothing this tool does overwrites a
//     considered reason with a machine-generated one.
//
// Without -write it prints the diff and exits non-zero if there is one, which
// is the form a CI check would want.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// outcome mirrors the record internal/engine/showdown writes to the report.
type outcome struct {
	Key    string   `json:"key"`
	Status string   `json:"status"`
	Kind   string   `json:"kind,omitempty"`
	Why    string   `json:"why,omitempty"`
	Detail []string `json:"detail,omitempty"`
}

// row is one ledger entry as it appears in the Go source.
type row struct {
	Key  string
	Kind string
	Why  string
}

func main() {
	report := flag.String("report", "showdown-report.json", "the JSON report written by PS_REPORT")
	ledger := flag.String("ledger", "internal/engine/showdown/gaps_test.go", "the Go file holding the gaps map")
	write := flag.Bool("write", false, "rewrite the ledger in place instead of printing the diff")
	flag.Parse()

	outs, err := readReport(*report)
	if err != nil {
		fatal("%v", err)
	}
	src, err := os.ReadFile(*ledger)
	if err != nil {
		fatal("read ledger: %v", err)
	}
	existing, start, end, err := parseLedger(string(src))
	if err != nil {
		fatal("parse ledger: %v", err)
	}

	next, added, removed := merge(existing, outs)

	if len(added) == 0 && len(removed) == 0 {
		fmt.Printf("ledger is current: %d rows, nothing to add or remove\n", len(next))
		return
	}

	for _, r := range added {
		fmt.Printf("+ [%s] %s\n      %s\n", r.Kind, r.Key, firstLine(r.Why))
	}
	for _, k := range removed {
		fmt.Printf("- %s  (passes now)\n", k)
	}
	fmt.Printf("\n%d rows to add, %d to remove, %d unchanged\n",
		len(added), len(removed), len(next)-len(added))

	if !*write {
		fmt.Fprintln(os.Stderr, "\nrun again with -write to apply, then review every added row: the kind is a guess")
		os.Exit(1)
	}

	out := string(src[:start]) + render(next) + string(src[end:])
	if err := os.WriteFile(*ledger, []byte(out), 0o644); err != nil {
		fatal("write ledger: %v", err)
	}
	fmt.Printf("\nwrote %s — now review every added row; the kind is a guess from the failure text\n", *ledger)
}

func readReport(path string) ([]outcome, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report: %w (run `make test-showdown-report` first)", err)
	}
	var outs []outcome
	if err := json.Unmarshal(b, &outs); err != nil {
		return nil, fmt.Errorf("parse report: %w", err)
	}
	if len(outs) == 0 {
		return nil, fmt.Errorf("%s holds no cases", path)
	}
	return outs, nil
}

// ledgerBounds finds the body of `var gaps = map[string]gap{ ... }`. The
// closing brace is located by depth rather than by pattern, so a reason string
// containing a brace cannot truncate the file.
var ledgerOpen = regexp.MustCompile(`(?m)^var gaps = map\[string\]gap\{`)

func parseLedger(src string) (rows map[string]row, start, end int, err error) {
	loc := ledgerOpen.FindStringIndex(src)
	if loc == nil {
		return nil, 0, 0, fmt.Errorf("no `var gaps = map[string]gap{` declaration found")
	}
	start = loc[1]
	depth := 1
	inStr, esc := false, false
	for i := start; i < len(src); i++ {
		c := src[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return parseRows(src[start:i]), start, i, nil
			}
		}
	}
	return nil, 0, 0, fmt.Errorf("the gaps map is not closed")
}

// rowPattern matches one rendered row: two Go string literals and a bare
// identifier for the kind, on one line, which is how render writes them.
//
// The string sub-pattern has to understand escapes. Reasons routinely quote
// the thing that went wrong (`move "transform" is not in this dataset`), and a
// lazy `".*?"` stops at the backslash-escaped quote inside — leaving an
// unparseable fragment, dropping the row on the floor, and letting the next
// rewrite delete it. See TestParseLedgerSurvivesQuotesInReasons.
const goString = `"(?:[^"\\]|\\.)*"`

var rowPattern = regexp.MustCompile(
	`(?m)^\s*(` + goString + `):\s*\{Kind:\s*(\w+),\s*Why:\s*(` + goString + `)\},\s*$`)

func parseRows(body string) map[string]row {
	rows := map[string]row{}
	for _, m := range rowPattern.FindAllStringSubmatch(body, -1) {
		key, err1 := strconv.Unquote(m[1])
		why, err2 := strconv.Unquote(m[3])
		if err1 != nil || err2 != nil {
			continue
		}
		rows[key] = row{Key: key, Kind: m[2], Why: why}
	}
	return rows
}

// merge folds a run into the ledger. Existing rows win over generated ones —
// a reason somebody wrote by hand is worth more than the failure text.
func merge(existing map[string]row, outs []outcome) (next map[string]row, added []row, removed []string) {
	next = map[string]row{}
	for k, v := range existing {
		next[k] = v
	}
	for _, o := range outs {
		switch o.Status {
		case "regress":
			if _, have := next[o.Key]; have {
				continue
			}
			r := row{Key: o.Key, Kind: guessKind(o.Detail), Why: summarize(o.Detail)}
			next[o.Key] = r
			added = append(added, r)
		case "stale":
			if _, have := next[o.Key]; have {
				delete(next, o.Key)
				removed = append(removed, o.Key)
			}
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Key < added[j].Key })
	sort.Strings(removed)
	return next, added, removed
}

// guessKind reads the failure text for the two things it can actually tell
// apart, and defaults to gapBug for everything else.
//
// The default is deliberate and is the conservative one for a conformance
// suite: an unexplained disagreement with Showdown is assumed to be this
// engine's fault until somebody shows otherwise. Filing it as a scope decision
// by default would let real bugs settle into the ledger unexamined.
func guessKind(detail []string) string {
	joined := strings.ToLower(strings.Join(detail, "\n"))
	switch {
	case strings.Contains(joined, "is not in this dataset"),
		strings.Contains(joined, "no record of this ability"),
		strings.Contains(joined, "models no behavior for this item"),
		strings.Contains(joined, "registered but inert"),
		strings.Contains(joined, "inert by design"):
		// The port named a move, item or ability the engine does not model.
		// That is the mechanic being absent, not the engine getting it wrong,
		// and the harness says which in the message.
		return "gapMissing"
	case strings.Contains(joined, "has no stand-in"),
		strings.Contains(joined, "does not know"),
		strings.Contains(joined, "is out of range"),
		strings.Contains(joined, "not a choice this harness understands"),
		strings.Contains(joined, "exceeds its max"),
		strings.Contains(joined, "after the battle already ended"):
		// The fixture does not describe a battle this engine can set up. That
		// is the translation's problem, and these rows should not survive a
		// triage pass.
		return "gapPort"
	default:
		return "gapBug"
	}
}

// summarize turns the recorded failures into a one-line reason. The full text
// stays in the report; the ledger wants something a reader can scan.
func summarize(detail []string) string {
	if len(detail) == 0 {
		return "failed with no recorded detail"
	}
	s := firstLine(detail[0])
	// The seed prefix is noise in the ledger — every row has one.
	if _, rest, ok := strings.Cut(s, ": "); ok && strings.HasPrefix(s, "seed ") {
		s = rest
	}
	const max = 160
	if len(s) > max {
		s = s[:max-1] + "…"
	}
	if n := len(detail); n > 1 {
		s += fmt.Sprintf(" (+%d more)", n-1)
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// render writes the map body back out, sorted, so the file diffs cleanly
// between runs.
func render(rows map[string]row) string {
	if len(rows) == 0 {
		return ""
	}
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('\n')
	for _, k := range keys {
		r := rows[k]
		fmt.Fprintf(&b, "\t%s: {Kind: %s, Why: %s},\n", strconv.Quote(k), r.Kind, strconv.Quote(r.Why))
	}
	return b.String()
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "showdown-triage: "+format+"\n", args...)
	os.Exit(2)
}
