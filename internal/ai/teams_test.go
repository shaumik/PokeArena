package ai

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"pokearena/internal/engine"
)

// TestLoadTeamPool_ExplicitPicksUseTunedMoves proves the pool honors explicit
// "picks" verbatim instead of auto-deriving the learnset's first four. This is
// what lets the live AI field competitively-tuned rosters (e.g. Mewtwo with
// Psystrike/Recover) rather than whatever four moves happen to sort first — the
// difference between a real opponent and a lobotomized one.
func TestLoadTeamPool_ExplicitPicksUseTunedMoves(t *testing.T) {
	d := loadDex(t)
	const poolJSON = `{"teams":[{"name":"Tuned","picks":[
		{"dex_no":150,"moves":["psystrike","aura-sphere","ice-beam","recover"]},
		{"dex_no":149,"moves":["dragon-dance","outrage","earthquake","fire-punch"]},
		{"dex_no":143,"moves":["body-slam","earthquake","crunch","rest"]},
		{"dex_no":145,"moves":["thunderbolt","hurricane","roost","thunder-wave"]},
		{"dex_no":121,"moves":["surf","thunderbolt","ice-beam","recover"]},
		{"dex_no":112,"moves":["earthquake","stone-edge","megahorn","swords-dance"]}
	]}]}`
	path := filepath.Join(t.TempDir(), "tuned.json")
	if err := os.WriteFile(path, []byte(poolJSON), 0o644); err != nil {
		t.Fatalf("write pool: %v", err)
	}

	p, err := LoadTeamPool(d, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	picks, err := p.Pick(rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if picks[0].DexNo != 150 {
		t.Fatalf("first pick dex_no = %d, want 150", picks[0].DexNo)
	}
	got := picks[0].MoveIDs
	want := []string{"psystrike", "aura-sphere", "ice-beam", "recover"}
	if len(got) != len(want) {
		t.Fatalf("Mewtwo moves = %v, want the tuned set %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("move %d = %q, want %q — explicit picks must be used verbatim, not auto-derived", i, got[i], want[i])
		}
	}
}

// TestLoadTeamPool_RealDataLoads is the canary: the curated
// data/ai-teams.json must load against the real dataset and every
// team must pass engine.ValidateTeam. A future bad edit fails the
// test rather than crashing the gateway on startup.
func TestLoadTeamPool_RealDataLoads(t *testing.T) {
	d := loadDex(t)
	p, err := LoadTeamPool(d, "../../data/ai-teams.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rng := rand.New(rand.NewSource(1))
	picks, err := p.Pick(rng)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if len(picks) != engine.TeamSize {
		t.Fatalf("picks: got %d, want %d", len(picks), engine.TeamSize)
	}
	if err := engine.ValidateTeam(picks, d); err != nil {
		t.Fatalf("picks failed validator: %v", err)
	}
}

func TestPick_IsADeepCopy(t *testing.T) {
	d := loadDex(t)
	p, err := LoadTeamPool(d, "../../data/ai-teams.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	a, _ := p.Pick(rand.New(rand.NewSource(7)))
	b, _ := p.Pick(rand.New(rand.NewSource(7)))
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("picks empty")
	}
	// Mutating one must not affect the other.
	a[0].MoveIDs[0] = "GARBAGE-ID"
	if b[0].MoveIDs[0] == "GARBAGE-ID" {
		t.Fatal("Pick returned a shared move slice — mutations leak across picks")
	}
}
