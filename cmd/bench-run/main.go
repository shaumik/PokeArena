// Command bench-run plays a whole benchmark matrix and can be re-run safely.
//
// One command, any size, resumable. It enumerates every game the config calls
// for, skips the ones already on disk, and plays the rest through the per-game
// runner (scripts/bench/play-live.sh), which drives an agent CLI over the
// PokéArena MCP tools.
//
// Three properties are the point:
//
//   - **Resumable.** The plan is a pure function of the config and each game's
//     result lands in its own file named from its coordinates. A run killed
//     overnight resumes by rebuilding the same plan and skipping what exists —
//     there is no central ledger to lose.
//   - **Balanced under interruption.** Games are interleaved across entrants,
//     so stopping early leaves every entrant with the same number of games
//     rather than the first one complete and the last with none.
//   - **Durable attribution.** Each game's entrant id is sent as the trainer
//     name, so which agent played which battle is a fact in Postgres, not a
//     mapping in a scratch directory. A previous batch was lost exactly that
//     way (docs/decision-quality-eval-handoff.md).
//
// Usage:
//
//	bench-run -config bench.json -out runs/2026-08-11          # play
//	bench-run -config bench.json -out runs/2026-08-11 -dry-run  # show the plan
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"pokearena/internal/eval"
)

// GameResult is the per-game record written to the output directory. Its
// presence is what marks a game complete for resume, so it is written only
// after the runner returns.
type GameResult struct {
	Label     string `json:"label"`
	Entrant   string `json:"entrant"`
	Harness   string `json:"harness"`
	Model     string `json:"model"`
	Team      string `json:"team"`
	Index     int    `json:"index"`
	BattleID  string `json:"battle_id"`
	Winner    int    `json:"winner"` // 0 = entrant, 1 = opponent, -1 = unfinished
	Status    string `json:"status"`
	StartedAt string `json:"started_at"`
	Seconds   int    `json:"seconds"`
}

func main() {
	log.SetFlags(log.Ltime)
	log.SetPrefix("[bench-run] ")

	configPath := flag.String("config", "bench.json", "run configuration")
	outDir := flag.String("out", "", "output directory for results (required)")
	dryRun := flag.Bool("dry-run", false, "print the plan and what remains, then exit")
	scriptPath := flag.String("runner", "scripts/bench/play-live.sh", "per-game runner script")
	flag.Parse()

	if *outDir == "" {
		log.Fatal("-out is required")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config: %v", err)
	}

	plan := eval.Interleaved(eval.BuildPlan(cfg))
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", *outDir, err)
	}
	done, err := completedLabels(*outDir)
	if err != nil {
		log.Fatalf("scan %s: %v", *outDir, err)
	}

	s := eval.Summarize(plan, done)
	log.Printf("%d entrants x %d teams x %d games = %d total (%d per entrant)",
		s.Entrants, s.Teams, cfg.GamesPerTeam, s.Total, s.PerEntrant)
	log.Printf("%d already complete, %d remaining", s.Total-s.Remaining, s.Remaining)

	todo := eval.Remaining(plan, done)
	if *dryRun {
		for _, g := range todo {
			fmt.Printf("%s\t%s\t%s\t%s\n", g.Label, g.Entrant.Harness, g.Entrant.Model, g.Team)
		}
		return
	}
	if len(todo) == 0 {
		log.Print("nothing to do")
		return
	}

	// Save the config next to the results so a published number can be traced
	// to the matrix that produced it, even if the source config later changes.
	if err := writeJSON(filepath.Join(*outDir, "config.json"), cfg); err != nil {
		log.Fatalf("write config copy: %v", err)
	}

	conc := cfg.Concurrency
	if conc < 1 {
		conc = 1
	}
	log.Printf("playing %d games, concurrency %d", len(todo), conc)
	run(todo, *outDir, *scriptPath, conc)
}

func run(todo []eval.PlannedGame, outDir, script string, conc int) {
	// Canceled only when run returns, so any child process still alive at
	// teardown is killed rather than orphaned. Interrupt deliberately does NOT
	// cancel it — see below, in-flight games are allowed to finish writing.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ctrl-C stops scheduling new games but lets in-flight ones finish writing,
	// so an interrupted run leaves no half-written result file to be mistaken
	// for a completed game on resume.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	stopped := make(chan struct{})
	go func() {
		<-stop
		log.Print("interrupt: finishing in-flight games, then stopping (re-run to resume)")
		close(stopped)
	}()

	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var mu sync.Mutex
	played, failed := 0, 0

	for _, g := range todo {
		select {
		case <-stopped:
			wg.Wait()
			log.Printf("stopped: %d played, %d failed this session", played, failed)
			return
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(g eval.PlannedGame) {
			defer wg.Done()
			defer func() { <-sem }()

			res, err := playGame(ctx, g, outDir, script)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				log.Printf("%s: FAILED: %v", g.Label, err)
				return
			}
			played++
			log.Printf("%s: winner=%d (%s) in %ds", g.Label, res.Winner, res.Status, res.Seconds)
		}(g)
	}
	wg.Wait()
	log.Printf("done: %d played, %d failed this session", played, failed)
}

// playGame runs one game and records it. The result file is written only on a
// clean run: a game that errored is left absent so re-running retries it rather
// than baking a failure into the dataset.
func playGame(ctx context.Context, g eval.PlannedGame, outDir, script string) (GameResult, error) {
	started := time.Now()
	// The entrant id travels as the trainer name, so attribution is recorded in
	// the battle row rather than inferred later from a scratch file.
	//
	// No timeout on this context: the per-game wall-clock cap lives in the
	// runner script, which also kills the CLI's own children. This context
	// exists only so a straggler dies at teardown instead of being orphaned.
	cmd := exec.CommandContext(ctx, "bash", script,
		g.Entrant.Harness, g.Entrant.Model, g.Team, g.Label, outDir, g.Entrant.ID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return GameResult{}, fmt.Errorf("%s: %w", lastLine(string(out)), err)
	}

	bid, winner, status := parseRunnerOutput(string(out))
	if bid == "" {
		return GameResult{}, fmt.Errorf("runner printed no bid=: %s", lastLine(string(out)))
	}

	res := GameResult{
		Label: g.Label, Entrant: g.Entrant.ID, Harness: g.Entrant.Harness,
		Model: g.Entrant.Model, Team: g.Team, Index: g.Index,
		BattleID: bid, Winner: winner, Status: status,
		StartedAt: started.UTC().Format(time.RFC3339), Seconds: int(time.Since(started).Seconds()),
	}
	return res, writeJSON(filepath.Join(outDir, g.Label+".json"), res)
}

// parseRunnerOutput reads the runner's authoritative result line, which comes
// from the gateway rather than the agent's self-report:
//
//	<label> winner=0 -> AGENT (status=completed) bid=<uuid>
func parseRunnerOutput(out string) (bid string, winner int, status string) {
	winner = -1
	for _, line := range strings.Split(out, "\n") {
		for _, f := range strings.Fields(line) {
			switch {
			case strings.HasPrefix(f, "bid="):
				bid = strings.TrimPrefix(f, "bid=")
			case strings.HasPrefix(f, "winner="):
				// A field we cannot parse leaves winner at -1 (unfinished),
				// which is the safe reading — better an under-counted result
				// than a fabricated win.
				if n, convErr := strconv.Atoi(strings.TrimPrefix(f, "winner=")); convErr == nil {
					winner = n
				}
			case strings.HasPrefix(f, "(status="):
				status = strings.TrimSuffix(strings.TrimPrefix(f, "(status="), ")")
			}
		}
	}
	return bid, winner, status
}

// completedLabels reads which games already have a result. This is the resume
// mechanism, and it deliberately derives from the files themselves rather than
// from an index that could drift out of sync with them.
func completedLabels(dir string) (map[string]bool, error) {
	done := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return done, nil
		}
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || name == "config.json" {
			continue
		}
		done[strings.TrimSuffix(name, ".json")] = true
	}
	return done, nil
}

func loadConfig(path string) (eval.BenchConfig, error) {
	var c eval.BenchConfig
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}
