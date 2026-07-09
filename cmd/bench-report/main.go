// Command bench-report turns a saved run record into a standalone HTML report:
// a leaderboard with confidence-interval bars, the per-team Elo breakdown, cost,
// and full provenance — one self-contained file, no network or assets.
//
// It reads only the persisted run JSON, so the report can never disagree with
// the numbers that were saved.
//
// Usage:
//
//	bench-report                          # newest run in ./runs -> report.html
//	bench-report -run runs/<id>.json      # a specific run
//	bench-report -runs runs -out r.html   # newest run in a store, to r.html
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"pokearena/internal/domain"
	"pokearena/internal/eval"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[bench-report] ")

	runPath := flag.String("run", "", "path to a specific run JSON (default: newest in -runs)")
	runsDir := flag.String("runs", "runs", "run store directory to pick the newest run from when -run is unset")
	outPath := flag.String("out", "report.html", "HTML output path (\"-\" for stdout)")
	baseline := flag.String("baseline", "", "benchmark mode: baseline round-robin JSONL trace, scored vs -ref")
	agentic := flag.String("agentic", "", "benchmark mode: directory of agentic results (subdirs with results.txt)")
	ref := flag.String("ref", "heuristic", "benchmark mode: the one opponent every contestant is scored against")
	dataDir := flag.String("data", "data", "benchmark mode: dataset directory (for replay re-simulation)")
	teamsPath := flag.String("teams", "data/benchmark-teams.json", "benchmark mode: team library the baseline replays re-simulate on")
	flag.Parse()

	// Benchmark mode: fold both arms into one "vs the reference" ladder and
	// render it through the standard report — same leaderboard, same replays.
	if *baseline != "" || *agentic != "" {
		if err := renderBenchmark(*baseline, *agentic, *ref, *dataDir, *teamsPath, *outPath); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}

	path := *runPath
	if path == "" {
		p, err := newestRun(*runsDir)
		if err != nil {
			log.Fatalf("%v", err)
		}
		path = p
	}

	rec, err := loadRecord(path)
	if err != nil {
		log.Fatalf("%v", err)
	}

	out := os.Stdout
	if *outPath != "-" {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatalf("create %s: %v", *outPath, err)
		}
		defer f.Close()
		out = f
	}
	if err := eval.RenderHTMLReport(out, rec); err != nil {
		log.Fatalf("render: %v", err)
	}
	if *outPath != "-" {
		log.Printf("wrote report for run %s to %s", rec.RunID, *outPath)
	}
}

// renderBenchmark builds the vs-reference RunRecord from the two benchmark arms
// and writes it through the standard report renderer.
func renderBenchmark(baselinePath, agenticDir, ref, dataDir, teamsPath, outPath string) error {
	dex, err := domain.LoadDex(dataDir, "bench")
	if err != nil {
		return fmt.Errorf("load dex: %w", err)
	}
	lib, err := eval.LoadTeamLibrary(teamsPath, dex)
	if err != nil {
		return fmt.Errorf("load teams: %w", err)
	}

	// Reuse the trace's own header for provenance (engine revision, ruleset,
	// dataset), but null the round-robin game count — this report's game total is
	// per-contestant, shown in the leaderboard, not derivable from that formula.
	header, err := readTraceHeader(baselinePath)
	if err != nil {
		return fmt.Errorf("read trace header: %w", err)
	}
	header.GamesPerPairing = 0

	rec, err := eval.BuildVsReferenceRecord(dex, baselinePath, agenticDir, ref, lib.Teams, header)
	if err != nil {
		return fmt.Errorf("build vs-reference record: %w", err)
	}

	// Contestant names for the masthead count (the reference is already dropped).
	header.Contestants = header.Contestants[:0]
	for _, c := range rec.Contestants {
		header.Contestants = append(header.Contestants, c.Name)
	}
	rec.Header = header

	w := os.Stdout
	if outPath != "-" {
		f, err := os.Create(outPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", outPath, err)
		}
		defer f.Close()
		w = f
	}
	if err := eval.RenderHTMLReport(w, rec); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	if outPath != "-" {
		log.Printf("wrote vs-%s report (%d contestants, %d replays) to %s", ref, len(rec.Contestants), len(rec.Replays), outPath)
	}
	return nil
}

// readTraceHeader reads the first JSONL line of a round-robin trace as the run
// header, so the benchmark report inherits the run's provenance.
func readTraceHeader(path string) (eval.RunHeader, error) {
	f, err := os.Open(path)
	if err != nil {
		return eval.RunHeader{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	if !sc.Scan() {
		return eval.RunHeader{}, fmt.Errorf("empty trace %s", path)
	}
	var h eval.RunHeader
	if err := json.Unmarshal(sc.Bytes(), &h); err != nil {
		return eval.RunHeader{}, fmt.Errorf("parse header: %w", err)
	}
	return h, nil
}

func loadRecord(path string) (eval.RunRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return eval.RunRecord{}, fmt.Errorf("read run %s: %w", path, err)
	}
	var rec eval.RunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return eval.RunRecord{}, fmt.Errorf("parse run %s: %w", path, err)
	}
	return rec, nil
}

// newestRun resolves the most recent run in a store using the index (the last
// appended line), falling back to lexically-latest <id>.json if there is no
// index — run_ids lead with a UTC timestamp, so lexical order is time order.
func newestRun(dir string) (string, error) {
	entries, err := eval.LoadIndex(dir)
	if err != nil {
		return "", err
	}
	if len(entries) > 0 {
		eval.SortIndexByTime(entries)
		return filepath.Join(dir, entries[len(entries)-1].RunID+".json"), nil
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no runs found in %s (run bench first, or pass -run)", dir)
	}
	newest := matches[0]
	for _, m := range matches[1:] {
		if m > newest {
			newest = m
		}
	}
	return newest, nil
}
