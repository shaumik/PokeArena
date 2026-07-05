// Command bench-board renders the PokéArena benchmark board: a single
// SWE-bench-style horizontal bar chart where every contestant is scored on one
// common axis — win rate against the reference expectimax AI — sorted best to
// worst, colored by the harness that drove it.
//
// It fuses two data sources onto that one axis:
//
//   - the reproducible baseline run (a JSONL trace): each deterministic agent's
//     head-to-head record against the reference agent;
//   - the agentic showcase (a directory of results.txt tallies): each live
//     harness's record against the in-engine AI.
//
// The output is a standalone, script-free HTML file (one external dependency: a
// web font and the PokéAPI sprite CDN, for the theme). Open it anywhere.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[bench-board] ")
	var (
		baselinePath = flag.String("baseline", "runs/arm1-baseline.jsonl", "baseline JSONL trace (deterministic round-robin)")
		ref          = flag.String("ref", "expectimax-d2", "reference agent everyone is scored against")
		agenticDir   = flag.String("agentic", "/tmp/pk-agentic", "directory of agentic results (subdirs with results.txt)")
		outPath      = flag.String("out", "board.html", "output HTML path")
		title        = flag.String("title", "PokéArena Benchmark", "board title")
	)
	flag.Parse()

	var rows []boardRow
	if *baselinePath != "" {
		br, err := parseBaselineVsRef(*baselinePath, *ref)
		if err != nil {
			log.Printf("baseline: %v (skipping)", err)
		}
		rows = append(rows, br...)
	}
	if *agenticDir != "" {
		ar, err := parseAgentic(*agenticDir)
		if err != nil {
			log.Printf("agentic: %v (skipping)", err)
		}
		rows = append(rows, ar...)
	}
	if len(rows) == 0 {
		log.Fatalf("no data: give -baseline and/or -agentic with results")
	}

	// Sort best win rate first; ties broken by more games (tighter estimate).
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].WinRate != rows[j].WinRate {
			return rows[i].WinRate > rows[j].WinRate
		}
		return rows[i].N > rows[j].N
	})

	view := boardView{
		Title:       *title,
		Ref:         *ref,
		GeneratedAt: time.Now().UTC().Format("2006-01-02 15:04 MST"),
		Rows:        rows,
		Legend:      legendFor(rows),
	}
	f, err := os.Create(*outPath)
	if err != nil {
		log.Fatalf("create %s: %v", *outPath, err)
	}
	defer f.Close()
	if err := boardTmpl.Execute(f, view); err != nil {
		log.Fatalf("render: %v", err)
	}
	log.Printf("wrote %s (%d contestants, ref=%s)", *outPath, len(rows), *ref)
}

// boardRow is one contestant on the board: its record against the reference,
// the win-rate geometry for the bar and CI whisker, and its theme.
type boardRow struct {
	Name       string
	Arm        string // key into arm metadata (baseline / agentic-claude / agentic-agy)
	ArmLabel   string
	Color      string
	Sprite     int // PokéAPI dex number for the mascot
	Wins       int
	Losses     int
	Unfinished int
	N          int // decided games = wins + losses
	WinRate    float64
	CILow      float64
	CIHigh     float64
}

func (r boardRow) Pct() string           { return fmt.Sprintf("%.0f%%", 100*r.WinRate) }
func (r boardRow) BarPct() float64       { return 100 * r.WinRate }
func (r boardRow) CILeftPct() float64    { return 100 * r.CILow }
func (r boardRow) CIWidthPct() float64   { return 100 * (r.CIHigh - r.CILow) }
func (r boardRow) CILowPctNum() float64  { return 100 * r.CILow }
func (r boardRow) CIHighPctNum() float64 { return 100 * r.CIHigh }
func (r boardRow) Record() string {
	s := fmt.Sprintf("%d–%d", r.Wins, r.Losses)
	if r.Unfinished > 0 {
		s += fmt.Sprintf(" (+%d dnf)", r.Unfinished)
	}
	return s
}
func (r boardRow) SpriteURL() string {
	return fmt.Sprintf("https://raw.githubusercontent.com/PokeAPI/sprites/master/sprites/pokemon/%d.png", r.Sprite)
}

// --- contestant theming ------------------------------------------------------

type armMeta struct {
	arm, label, color string
	sprite            int
}

// theme resolves a contestant name to its arm, color, and mascot sprite. The
// mascots are a wink: Alakazam for the deepest search, Porygon (the artificial
// Pokémon) and Mewtwo (the lab-made psychic) for the two LLM harnesses.
func theme(name, arm string) armMeta {
	switch arm {
	case "agentic-claude":
		return armMeta{arm, "Claude Code (agentic)", "#d97757", 137} // Porygon
	case "agentic-agy":
		return armMeta{arm, "Antigravity (agentic)", "#4285f4", 150} // Mewtwo
	}
	// Baseline mascots by name. Expectimax agents are named "expectimax-dN";
	// deeper search gets a more evolved psychic.
	sprite := 66 // Machop, generic muscle
	switch {
	case strings.HasPrefix(name, "expectimax") && strings.HasSuffix(name, "3"):
		sprite = 65 // Alakazam
	case strings.HasPrefix(name, "expectimax") && strings.HasSuffix(name, "1"):
		sprite = 63 // Abra
	case strings.HasPrefix(name, "expectimax"):
		sprite = 64 // Kadabra
	case name == "random":
		sprite = 129 // Magikarp
	case name == "heuristic":
		sprite = 66 // Machop
	}
	return armMeta{"baseline", "Baseline (deterministic)", "#7c8aa5", sprite}
}

func legendFor(rows []boardRow) []legendEntry {
	seen := map[string]legendEntry{}
	var order []string
	for _, r := range rows {
		if _, ok := seen[r.Arm]; !ok {
			seen[r.Arm] = legendEntry{Label: r.ArmLabel, Color: r.Color}
			order = append(order, r.Arm)
		}
	}
	out := make([]legendEntry, 0, len(order))
	for _, k := range order {
		out = append(out, seen[k])
	}
	return out
}

type legendEntry struct{ Label, Color string }

// --- baseline: win rate vs the reference agent -------------------------------

type gameRow struct {
	Type   string `json:"type"`
	Side0  string `json:"side0"`
	Side1  string `json:"side1"`
	Winner string `json:"winner"`
}

// parseBaselineVsRef reads a round-robin JSONL trace and, for every game that
// involved the reference agent, credits the opponent with a win or loss. The
// result is each non-reference contestant's head-to-head record against the
// reference — the same "beat the bot" axis the agentic arm is measured on.
func parseBaselineVsRef(path, ref string) ([]boardRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	type wl struct{ w, l int }
	rec := map[string]*wl{}
	order := []string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"type":"game"`) {
			continue
		}
		var g gameRow
		if err := json.Unmarshal(line, &g); err != nil {
			continue
		}
		var opp string
		switch ref {
		case g.Side0:
			opp = g.Side1
		case g.Side1:
			opp = g.Side0
		default:
			continue // game did not involve the reference
		}
		if opp == "" || opp == ref {
			continue
		}
		if _, ok := rec[opp]; !ok {
			rec[opp] = &wl{}
			order = append(order, opp)
		}
		switch g.Winner {
		case opp:
			rec[opp].w++
		case ref:
			rec[opp].l++
		default:
			// draw or unresolved: not counted in the decided denominator
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	var rows []boardRow
	for _, name := range order {
		r := rec[name]
		rows = append(rows, mkRow(name, "baseline", r.w, r.l, 0))
	}
	return rows, nil
}

// --- agentic: win rate vs the in-engine AI -----------------------------------

// parseAgentic scans the agentic output directory for per-config results.txt
// tallies and aggregates them per harness (across teams). A config dir named
// "cc-haiku-Genesis" rolls into the Claude harness; "agy-gemini-Keystone" into
// the Antigravity harness. winner=0 is an agent win, winner=1 an AI win,
// anything else an unfinished battle.
func parseAgentic(dir string) ([]boardRow, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type tally struct{ w, l, o int }
	agg := map[string]*tally{} // arm -> tally
	label := map[string]string{
		"agentic-claude": "claude · haiku",
		"agentic-agy":    "agy · Gemini",
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var arm string
		switch {
		case strings.HasPrefix(e.Name(), "cc-haiku"), strings.HasPrefix(e.Name(), "cc-"):
			arm = "agentic-claude"
		case strings.HasPrefix(e.Name(), "agy-"):
			arm = "agentic-agy"
		default:
			continue
		}
		res := filepath.Join(dir, e.Name(), "results.txt")
		w, l, o := tallyResults(res)
		if w+l+o == 0 {
			continue
		}
		if _, ok := agg[arm]; !ok {
			agg[arm] = &tally{}
		}
		agg[arm].w += w
		agg[arm].l += l
		agg[arm].o += o
	}

	var rows []boardRow
	for arm, t := range agg {
		name := label[arm]
		if name == "" {
			name = arm
		}
		rows = append(rows, mkRow(name, arm, t.w, t.l, t.o))
	}
	return rows, nil
}

func tallyResults(path string) (w, l, o int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, "winner=0"):
			w++
		case strings.Contains(line, "winner=1"):
			l++
		case strings.Contains(line, "winner="):
			o++
		}
	}
	return w, l, o
}

// mkRow assembles a board row with its Wilson interval and theme.
func mkRow(name, arm string, w, l, o int) boardRow {
	n := w + l
	var rate float64
	if n > 0 {
		rate = float64(w) / float64(n)
	}
	lo, hi := wilson(w, n)
	m := theme(name, arm)
	return boardRow{
		Name:       name,
		Arm:        m.arm,
		ArmLabel:   m.label,
		Color:      m.color,
		Sprite:     m.sprite,
		Wins:       w,
		Losses:     l,
		Unfinished: o,
		N:          n,
		WinRate:    rate,
		CILow:      lo,
		CIHigh:     hi,
	}
}

// wilson returns the 95% Wilson score interval for w wins in n trials — the
// same interval the CLI reports, so bars and console agree.
func wilson(w, n int) (lo, hi float64) {
	if n == 0 {
		return 0, 0
	}
	const z = 1.96
	p := float64(w) / float64(n)
	nn := float64(n)
	denom := 1 + z*z/nn
	center := (p + z*z/(2*nn)) / denom
	half := z / denom * math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn))
	lo, hi = center-half, center+half
	if lo < 0 {
		lo = 0
	}
	if hi > 1 {
		hi = 1
	}
	return lo, hi
}

// --- view + template ---------------------------------------------------------

type boardView struct {
	Title       string
	Ref         string
	GeneratedAt string
	Rows        []boardRow
	Legend      []legendEntry
}

var boardTmpl = template.Must(template.New("board").
	Funcs(template.FuncMap{"add": func(a, b int) int { return a + b }}).
	Parse(boardHTML))
