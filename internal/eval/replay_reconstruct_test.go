package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shaumik/PokeArena/internal/engine"
)

// TestReplayFromStored checks a persisted battle round-trips into a watchable
// replay: the marshaled engine state (exactly what the live coordinator stores)
// unmarshals back and projects into frames with the trainer labels we supply,
// the mons' names and HP, and the winner — identical in shape to a simulated
// capture.
func TestReplayFromStored(t *testing.T) {
	dex := loadDex(t)
	st, err := engine.NewBattle(dex, "b", "Agent", []int{6, 9, 26}, "AI", []int{3, 65, 143}, 7)
	if err != nil {
		t.Fatalf("NewBattle: %v", err)
	}

	// Two "stored" turns: the state exactly as persisted (json.Marshal of the
	// live state), with a log line on the second.
	var turns []StoredTurn
	for i := 0; i < 2; i++ {
		state, err := json.Marshal(st)
		if err != nil {
			t.Fatalf("marshal state: %v", err)
		}
		logJSON, _ := json.Marshal([]engine.LogLine{{Type: "text", Text: "Charizard used Flamethrower!"}})
		turns = append(turns, StoredTurn{State: state, Log: logJSON})
	}

	rep, err := ReplayFromStored(7, "Claude Sonnet 4.6", "heuristic", "Claude Sonnet 4.6", turns)
	if err != nil {
		t.Fatalf("ReplayFromStored: %v", err)
	}
	if len(rep.Frames) != 2 {
		t.Fatalf("want 2 frames, got %d", len(rep.Frames))
	}
	if rep.Side0 != "Claude Sonnet 4.6" || rep.Side1 != "heuristic" {
		t.Errorf("trainer labels not applied: %q vs %q", rep.Side0, rep.Side1)
	}
	// The projected frame carries the active mon's name and HP from the state.
	a := rep.Frames[0].Sides[0].Active
	if a.Name == "" || a.MaxHP == 0 {
		t.Errorf("frame lost the active mon: %+v", a)
	}
	if a.HP <= 0 || a.HP > a.MaxHP {
		t.Errorf("frame HP out of range: %d/%d", a.HP, a.MaxHP)
	}
	if len(rep.Frames[1].Log) == 0 {
		t.Error("frame dropped the turn log")
	}
}

// TestAttachAgenticReplays checks a reconstructed model-vs-reference replay is
// appended to the record and wired into the matrix cell for that matchup (both
// orientations), turning a replayless stat cell into a watchable one.
func TestAttachAgenticReplays(t *testing.T) {
	dir := t.TempDir()
	rep := Replay{
		Side0: "Claude Sonnet 4.6", Side1: "heuristic", Winner: "Claude Sonnet 4.6", Turns: 12,
		Frames: []ReplayFrame{{Phase: "turn", Turn: 12}},
	}
	data, _ := json.Marshal(rep)
	if err := os.WriteFile(filepath.Join(dir, "sonnet.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	rec := RunRecord{
		Replays: []Replay{{Side0: "expectimax-d1", Side1: "heuristic"}}, // one existing baseline replay at idx 0
		Matrix: &ReplayMatrix{
			Agents: []string{"heuristic", "Claude Sonnet 4.6"},
			Cells: []MatchupCell{
				{Row: 1, Col: 0, WinRate: 0.34, Games: 47, Replay: -1}, // Sonnet -> heuristic, no replay yet
				{Row: 0, Col: 1, WinRate: 0.66, Games: 47, Replay: -1}, // heuristic -> Sonnet
			},
		},
	}
	attachAgenticReplays(&rec, dir)

	if len(rec.Replays) != 2 {
		t.Fatalf("want 2 replays after attach, got %d", len(rec.Replays))
	}
	for _, c := range rec.Matrix.Cells {
		if c.Replay != 1 {
			t.Errorf("cell (%d,%d) should point at the appended replay (idx 1), got %d", c.Row, c.Col, c.Replay)
		}
	}
}
