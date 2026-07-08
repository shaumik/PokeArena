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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"pokearena/internal/eval"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("[bench-report] ")

	runPath := flag.String("run", "", "path to a specific run JSON (default: newest in -runs)")
	runsDir := flag.String("runs", "runs", "run store directory to pick the newest run from when -run is unset")
	outPath := flag.String("out", "report.html", "HTML output path (\"-\" for stdout)")
	flag.Parse()

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
