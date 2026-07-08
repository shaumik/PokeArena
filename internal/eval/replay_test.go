package eval

import (
	"strings"
	"testing"

	"pokearena/internal/ai"
	"pokearena/internal/usage"
)

// TestRunGameCaptured_ProducesCoherentFrames plays a real deterministic game and
// checks the captured replay is coherent: a lead frame, one frame per resolved
// turn, valid HP on every board, a party tray per side, and at least one turn
// that recorded engine log lines.
func TestRunGameCaptured_ProducesCoherentFrames(t *testing.T) {
	d := loadDex(t)
	const seed = 3
	agents := [2]ai.Agent{ai.NewHeuristicAgent(d), ai.NewRandomAgent(seed ^ sideSalt)}
	res, rep, err := RunGameCaptured(d, agents, mirrorTeams(t, d), seed, 0)
	if err != nil {
		t.Fatalf("RunGameCaptured: %v", err)
	}

	if len(rep.Frames) < 2 {
		t.Fatalf("expected a lead frame plus turn frames, got %d", len(rep.Frames))
	}
	if rep.Frames[0].Phase != "lead" {
		t.Errorf("first frame phase = %q, want \"lead\"", rep.Frames[0].Phase)
	}
	if rep.Turns != res.Turns {
		t.Errorf("replay turns %d disagree with game result %d", rep.Turns, res.Turns)
	}

	sawLog := false
	for i, f := range rep.Frames {
		for side := 0; side < 2; side++ {
			sd := f.Sides[side]
			if len(sd.Tray) == 0 {
				t.Errorf("frame %d side %d has an empty party tray", i, side)
			}
			m := sd.Active
			if m.MaxHP > 0 && (m.HP < 0 || m.HP > m.MaxHP) {
				t.Errorf("frame %d side %d HP %d out of range [0,%d]", i, side, m.HP, m.MaxHP)
			}
		}
		if len(f.Log) > 0 {
			sawLog = true
		}
	}
	if !sawLog {
		t.Error("no frame captured any engine log lines — the turn log was dropped")
	}
}

// TestSelectHighlights_PicksDistinctStandouts checks the selection surfaces the
// longest game, the biggest Elo upset, a draw, and the quickest decisive game —
// and never lists the same game twice.
func TestSelectHighlights_PicksDistinctStandouts(t *testing.T) {
	mk := func(s0, s1, winner string, seed uint64, turns int) GameRecord {
		return GameRecord{
			Match: s0 + "-vs-" + s1, Team: "Alpha", Seed: seed,
			Side0: s0, Side1: s1, Winner: winner,
			Result: GameResult{Winner: 0, Turns: turns},
		}
	}
	matches := []MatchResult{{Games: []GameRecord{
		mk("strong", "weak", "strong", 1, 60), // longest (favorite won — not an upset)
		mk("strong", "weak", "weak", 2, 25),   // biggest upset: weak(800) beats strong(1200), margin 400
		mk("mid", "strong", "mid", 3, 20),     // smaller upset: mid(1000) beats strong(1200), margin 200
		mk("strong", "weak", "draw", 4, 15),   // a draw
		mk("strong", "weak", "strong", 5, 4),  // quickest decisive
	}}}
	standings := []ContestantResult{
		{Name: "strong", Elo: 1200}, {Name: "mid", Elo: 1000}, {Name: "weak", Elo: 800},
	}

	picks := selectHighlights(matches, standings)
	find := func(sub string) (highlight, bool) {
		for _, p := range picks {
			if strings.Contains(p.Title, sub) {
				return p, true
			}
		}
		return highlight{}, false
	}

	if p, ok := find("Longest"); !ok || p.Game.Result.Turns != 60 {
		t.Errorf("longest pick wrong: %+v (ok=%v)", p.Title, ok)
	}
	if p, ok := find("upset"); !ok || p.Game.Winner != "weak" || p.Game.Result.Turns != 25 {
		t.Errorf("biggest upset should be weak beating strong in 25t, got %+v (ok=%v)", p.Game, ok)
	}
	if p, ok := find("draw"); !ok || p.Game.Winner != "draw" {
		t.Errorf("draw pick wrong: %+v (ok=%v)", p.Game, ok)
	}
	if p, ok := find("Quickest"); !ok || p.Game.Result.Turns != 4 {
		t.Errorf("quickest pick wrong: %+v (ok=%v)", p.Game, ok)
	}

	seen := map[string]bool{}
	for _, p := range picks {
		k := gameKey(p.Game)
		if seen[k] {
			t.Errorf("game %s picked twice", k)
		}
		seen[k] = true
	}
}

// TestCaptureMatchups_BuildsGridAndBattles runs a real 2-contestant match and
// checks the captured output: one battle per pairing, a matrix keyed in
// standings order whose cells reference that battle, and win rates that are
// perspective-correct (row agent's) and sum to 1 across a pairing.
func TestCaptureMatchups_BuildsGridAndBattles(t *testing.T) {
	d := loadDex(t)
	team := NamedTeam{Name: "Trio", Picks: mirrorTeams(t, d)[0]}
	a := Contestant{Name: "heur", New: func(uint64) ai.Agent { return ai.NewHeuristicAgent(d) }}
	b := randomC("rand")

	mr, err := RunMatch(d, a, b, team.Name, team.Mirror(), SeedRange(2), 0)
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	header := RunHeader{Ruleset: Ruleset(), Contestants: []string{"heur", "rand"}}
	rec := BuildRunRecord(header, []MatchResult{mr}, nil, nil, nil)

	replays, matrix := CaptureMatchups(d, []Contestant{a, b}, []NamedTeam{team}, []MatchResult{mr}, rec.Contestants, 0)

	if len(replays) != 1 {
		t.Fatalf("one pairing should yield one captured battle, got %d", len(replays))
	}
	if len(replays[0].Frames) < 2 {
		t.Errorf("captured battle has too few frames: %d", len(replays[0].Frames))
	}
	if len(matrix.Agents) != 2 {
		t.Fatalf("matrix should list both agents, got %v", matrix.Agents)
	}
	if len(matrix.Cells) != 2 {
		t.Fatalf("2 agents ⇒ 2 off-diagonal cells, got %d", len(matrix.Cells))
	}

	// Both cells reference the single captured battle, and the pair's win rates
	// (row agent's perspective) sum to 1.
	var sum float64
	for _, cl := range matrix.Cells {
		if cl.Replay != 0 {
			t.Errorf("cell (%d,%d) should reference replay 0, got %d", cl.Row, cl.Col, cl.Replay)
		}
		if cl.Games != 4 { // 2 seeds x 2 orientations
			t.Errorf("cell games = %d, want 4", cl.Games)
		}
		sum += cl.WinRate
	}
	if sum < 0.999 || sum > 1.001 {
		t.Errorf("a pairing's two win rates should sum to 1, got %.3f", sum)
	}

	// heuristic dominates random, and it sorts first in standings, so the
	// (0,1) cell — heuristic vs random — must be the high-win-rate one.
	for _, cl := range matrix.Cells {
		if cl.Row == 0 && cl.Col == 1 && cl.WinRate <= 0.5 {
			t.Errorf("top agent's win rate vs bottom should exceed 0.5, got %.2f", cl.WinRate)
		}
	}
}

// TestRenderHTMLReport_EmbedsReplays checks a run carrying replays renders a
// self-contained, watchable replay: the embedded battle data, the player
// scaffolding, and the captured names — with no external asset references (an
// inline <script> is fine; a network src= or URL is not).
func TestRenderHTMLReport_EmbedsReplays(t *testing.T) {
	side := func(trainer, mon string, hp, max int) ReplaySide {
		return ReplaySide{
			Trainer: trainer,
			Active:  ReplayMon{Name: mon, Types: "Fire/Flying", HP: hp, MaxHP: max},
			Tray:    []ReplaySlot{{Name: mon, HPPct: 100 * hp / max, Active: true}},
		}
	}
	rep := Replay{
		Title: "Longest game — 42 turns", Match: "heuristic-vs-random", Team: "Alpha",
		Side0: "heuristic", Side1: "random", Winner: "heuristic", Turns: 42,
		Frames: []ReplayFrame{
			{Phase: "lead", Turn: 0, Sides: [2]ReplaySide{side("P0", "Charizard", 192, 192), side("P1", "Vileplume", 200, 200)}},
			{
				Phase: "turn", Turn: 1, Actions: [2]string{"used Flamethrower", "used Sludge Bomb"},
				Sides: [2]ReplaySide{side("P0", "Charizard", 150, 192), side("P1", "Vileplume", 40, 200)},
				Log:   []string{"Charizard used Flamethrower!", "Vileplume fainted!"},
			},
		},
	}

	header := RunHeader{
		Timestamp: "2026-07-07T00:00:00Z", EngineRevision: "abc123",
		Ruleset: Ruleset(), TeamLibrary: "v1", Teams: []string{"Alpha"},
		Contestants: []string{"heuristic", "random"}, GamesPerPairing: 5, Orientations: 2, Seeds: "0..4",
	}
	rec := BuildRunRecord(header, []MatchResult{mkMatch("heuristic", "random", 3, 2, usage.Usage{}, usage.Usage{})},
		nil, nil, nil)
	rec.Replays = []Replay{rep}
	rec.Matrix = &ReplayMatrix{
		Agents: []string{"heuristic", "random"},
		Cells: []MatchupCell{
			{Row: 0, Col: 1, WinRate: 0.99, Games: 960, Replay: 0},
			{Row: 1, Col: 0, WinRate: 0.01, Games: 960, Replay: 0},
		},
	}
	rec.Rosters = []TeamRoster{{Name: "Alpha", Members: []RosterMon{{Name: "Snorlax", Types: "Normal", BST: 540}}}}

	var sb strings.Builder
	if err := RenderHTMLReport(&sb, rec); err != nil {
		t.Fatalf("RenderHTMLReport: %v", err)
	}
	html := sb.String()

	for _, want := range []string{
		"const REPLAYS =", "const MATRIX =", "const ROSTERS =", // embedded data
		"Longest game",           // caption
		"Charizard", "Vileplume", // captured mons
		"Flamethrower",           // captured log
		`id="c0"`, `id="rspark"`, // stage + momentum graph
		`class="lrow`,    // leaderboard rows are the picker
		"Snorlax",        // embedded team roster
		"Watch a battle", // section heading
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("report with replays missing %q", want)
		}
	}
	// Self-contained: no network fetches. Inline <script> is allowed; src= and
	// absolute URLs are not.
	for _, bad := range []string{"http://", "https://", "src="} {
		if strings.Contains(html, bad) {
			t.Fatalf("replay report should be self-contained, found %q", bad)
		}
	}
	// The embedded JSON must not break out of a <script> tag: open and close
	// tags stay balanced (a stray </script> from the data would unbalance them).
	if strings.Count(html, "<script") != strings.Count(html, "</script>") {
		t.Fatal("unbalanced <script> tags — possible breakout in embedded data")
	}
}
