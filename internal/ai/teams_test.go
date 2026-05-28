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
	for _, tier := range []string{"easy", "hard"} {
		rng := rand.New(rand.NewSource(1))
		picks, err := p.Pick(tier, rng)
		if err != nil {
			t.Fatalf("pick %s: %v", tier, err)
		}
		if len(picks) != engine.TeamSize {
			t.Fatalf("%s picks: got %d, want %d", tier, len(picks), engine.TeamSize)
		}
		if err := engine.ValidateTeam(picks, d); err != nil {
			t.Fatalf("%s picks failed validator: %v", tier, err)
		}
	}
}

func TestPick_UnknownDifficulty(t *testing.T) {
	d := loadDex(t)
	p, err := LoadTeamPool(d, "../../data/ai-teams.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := p.Pick("medium", rand.New(rand.NewSource(1))); err == nil {
		t.Fatalf("expected error for unknown difficulty 'medium'")
	}
}

func TestPick_IsADeepCopy(t *testing.T) {
	d := loadDex(t)
	p, err := LoadTeamPool(d, "../../data/ai-teams.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	a, _ := p.Pick("hard", rand.New(rand.NewSource(7)))
	b, _ := p.Pick("hard", rand.New(rand.NewSource(7)))
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("picks empty")
	}
	// Mutating one must not affect the other.
	a[0].MoveIDs[0] = "GARBAGE-ID"
	if b[0].MoveIDs[0] == "GARBAGE-ID" {
		t.Fatal("Pick returned a shared move slice — mutations leak across picks")
	}
}
