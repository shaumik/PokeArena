package ai

import (
	"math/rand"
	"testing"

	"pokearena/internal/engine"
)

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
