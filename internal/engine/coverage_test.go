package engine

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
)

// updateCoverage rewrites the committed move-coverage fixture instead of
// failing on diff. Run:
//
//	go test ./internal/engine -run TestMoveCoverage -update-coverage
//
// after expanding SupportedFlags/Volatiles (engine grew a feature) or
// after pulling new upstream data — review the diff before committing.
var updateCoverage = flag.Bool("update-coverage", false, "rewrite testdata/move_coverage.json")

const (
	upstreamMovesPath = "../../tools/data-sync/upstream/moves.json"
	curatedMovesPath  = "../../data/moves.json"
	coverageFixture   = "testdata/move_coverage.json"
)

// TestMoveCoverage is the static guardrail for "what's broken at the
// declarative level". The committed fixture is the snapshot of moves whose
// upstream Showdown definition asks for engine behavior we haven't built
// yet. Any time it changes — either direction — review:
//
//   - Set shrank: engine gained a feature, fewer moves are stuck. Good.
//   - Set grew: someone added a curated move whose semantics aren't
//     modeled, or someone removed engine support. Investigate.
//
// JS-callback behavior (Moonlight's weather-aware heal, Stored Power's
// dynamic basePower) is intentionally invisible to this audit — see
// coverage.go's MoveGap doc. Those need targeted unit tests.
func TestMoveCoverage(t *testing.T) {
	gaps, err := AuditUpstream(upstreamMovesPath, curatedMovesPath)
	if err != nil {
		t.Fatalf("AuditUpstream: %v", err)
	}

	got, err := json.MarshalIndent(gaps, "", "  ")
	if err != nil {
		t.Fatalf("marshal gaps: %v", err)
	}
	got = append(got, '\n')

	if *updateCoverage {
		if err := os.WriteFile(coverageFixture, got, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		t.Logf("rewrote %s (%d gaps)", coverageFixture, len(gaps))
		return
	}

	want, err := os.ReadFile(coverageFixture)
	if err != nil {
		t.Fatalf("read fixture: %v (run with -update-coverage to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("move coverage report changed (%d gaps now). "+
			"Run:\n  go test ./internal/engine -run TestMoveCoverage -update-coverage\n"+
			"then review the diff in %s before committing.", len(gaps), coverageFixture)
	}
}
