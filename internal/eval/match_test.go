package eval

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"pokearena/internal/ai"
)

func randomC(name string) Contestant {
	return Contestant{Name: name, New: func(seed uint64) ai.Agent { return ai.NewRandomAgent(seed) }}
}

// TestRunMatch_CountsAndOrientations: every seed yields two games (both
// orientations), wins/draws sum to the game count, and each contestant is
// seated on side 0 exactly once per seed.
func TestRunMatch_CountsAndOrientations(t *testing.T) {
	d := loadDex(t)
	a := Contestant{Name: "heur", New: func(uint64) ai.Agent { return ai.NewHeuristicAgent(d) }}
	b := randomC("rand")

	seeds := SeedRange(4)
	mr, err := RunMatch(d, a, b, "test", mirrorTeams(t, d), seeds, 0)
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}

	wantGames := len(seeds) * 2
	if len(mr.Games) != wantGames {
		t.Fatalf("got %d games, want %d", len(mr.Games), wantGames)
	}
	if mr.AWins+mr.BWins+mr.Draws != wantGames {
		t.Fatalf("wins %d + %d + draws %d != %d", mr.AWins, mr.BWins, mr.Draws, wantGames)
	}

	// Per seed: A on side 0 once, B on side 0 once.
	side0Count := map[string]int{}
	for _, g := range mr.Games {
		side0Count[g.Side0]++
		if g.Winner != a.Name && g.Winner != b.Name && g.Winner != "draw" {
			t.Fatalf("unexpected winner %q", g.Winner)
		}
	}
	if side0Count[a.Name] != len(seeds) || side0Count[b.Name] != len(seeds) {
		t.Fatalf("orientation imbalance: %v", side0Count)
	}
}

// TestResolvedGame_SaltsSideOneAndResolvesWinner pins the two invariants the
// shared seat core centralizes for both RunMatch and TeamTournament: side 0 is
// seeded with the raw seed and side 1 with seed^sideSalt (so a mirror doesn't
// move in lockstep), and the board-side winner is resolved to the seat's name
// ("" for a draw) — never leaked as a raw side index.
func TestResolvedGame_SaltsSideOneAndResolvesWinner(t *testing.T) {
	d := loadDex(t)
	teams := mirrorTeams(t, d)

	var s0Seed, s1Seed uint64
	s0 := seat{name: "alpha", picks: teams[0], newAgent: func(seed uint64) ai.Agent {
		s0Seed = seed
		return ai.NewHeuristicAgent(d)
	}}
	s1 := seat{name: "beta", picks: teams[1], newAgent: func(seed uint64) ai.Agent {
		s1Seed = seed
		return ai.NewRandomAgent(seed)
	}}

	const seed = 7
	oc, err := resolvedGame(d, s0, s1, seed, 0)
	if err != nil {
		t.Fatalf("resolvedGame: %v", err)
	}

	if s0Seed != seed {
		t.Errorf("side 0 seed = %d, want raw seed %d", s0Seed, seed)
	}
	if s1Seed != seed^sideSalt {
		t.Errorf("side 1 seed = %d, want salted %d", s1Seed, uint64(seed)^sideSalt)
	}
	if oc.S0 != "alpha" || oc.S1 != "beta" {
		t.Errorf("seat names not carried through: S0=%q S1=%q", oc.S0, oc.S1)
	}
	if oc.Winner != "alpha" && oc.Winner != "beta" && oc.Winner != "" {
		t.Errorf("winner %q is neither seat name nor the draw sentinel", oc.Winner)
	}
	if oc.Turns != oc.Result.Turns {
		t.Errorf("outcome turns %d disagree with the game trace %d", oc.Turns, oc.Result.Turns)
	}
}

// TestRunMatch_RejectsSameName: two contestants sharing a name would collapse
// win attribution (every win books to A) and corrupt the Elo fit, so RunMatch
// must refuse rather than silently produce wrong numbers.
func TestRunMatch_RejectsSameName(t *testing.T) {
	d := loadDex(t)
	a := randomC("clone")
	b := randomC("clone")
	if _, err := RunMatch(d, a, b, "test", mirrorTeams(t, d), SeedRange(1), 0); err == nil {
		t.Fatal("RunMatch must reject two contestants with the same name")
	}
}

// TestRunMatch_Reproducible: the same contestants and seeds produce identical
// aggregate outcomes across two independent runs — the property that lets a
// published standings table be re-derived from the CLI.
func TestRunMatch_Reproducible(t *testing.T) {
	d := loadDex(t)
	mk := func() MatchResult {
		a := Contestant{Name: "heur", New: func(uint64) ai.Agent { return ai.NewHeuristicAgent(d) }}
		b := randomC("rand")
		mr, err := RunMatch(d, a, b, "test", mirrorTeams(t, d), SeedRange(4), 0)
		if err != nil {
			t.Fatalf("RunMatch: %v", err)
		}
		return mr
	}
	x, y := mk(), mk()
	if x.AWins != y.AWins || x.BWins != y.BWins || x.Draws != y.Draws {
		t.Fatalf("non-reproducible aggregates: %+v vs %+v", x, y)
	}
	for i := range x.Games {
		if x.Games[i].Winner != y.Games[i].Winner {
			t.Fatalf("game %d winner differs: %q vs %q", i, x.Games[i].Winner, y.Games[i].Winner)
		}
	}
}

// TestWriteMatch_JSONLShape: every emitted line is valid JSON tagged as either
// a game or decision row, decision rows outnumber game rows, and the count of
// game rows matches the games played.
func TestWriteMatch_JSONLShape(t *testing.T) {
	d := loadDex(t)
	a := Contestant{Name: "heur", New: func(uint64) ai.Agent { return ai.NewHeuristicAgent(d) }}
	b := randomC("rand")
	mr, err := RunMatch(d, a, b, "test", mirrorTeams(t, d), SeedRange(2), 0)
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteMatch(&buf, mr); err != nil {
		t.Fatalf("WriteMatch: %v", err)
	}

	var gameRows, decisionRows int
	sc := bufio.NewScanner(&buf)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Fatalf("invalid JSON line: %v\n%s", err, line)
		}
		switch probe.Type {
		case "game":
			gameRows++
		case "decision":
			decisionRows++
		default:
			t.Fatalf("unexpected row type %q", probe.Type)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if gameRows != len(mr.Games) {
		t.Fatalf("got %d game rows, want %d", gameRows, len(mr.Games))
	}
	if decisionRows <= gameRows {
		t.Fatalf("expected more decision rows than game rows, got %d vs %d", decisionRows, gameRows)
	}
}
