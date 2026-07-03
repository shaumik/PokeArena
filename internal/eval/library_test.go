package eval

import (
	"testing"

	"pokearena/internal/engine"
)

const libraryPath = "../../data/benchmark-teams.json"

// TestTeamLibrary_AllLegal loads the shipped library and asserts every team
// passes engine.ValidateTeam. This is the guarantee that no illegal team can
// ever reach a published run.
func TestTeamLibrary_AllLegal(t *testing.T) {
	d := loadDex(t)
	lib, err := LoadTeamLibrary(libraryPath, d)
	if err != nil {
		t.Fatalf("load/validate library: %v", err)
	}
	if len(lib.Teams) < 4 {
		t.Fatalf("want at least 4 teams, got %d", len(lib.Teams))
	}
	for _, team := range lib.Teams {
		if len(team.Picks) != engine.TeamSize {
			t.Fatalf("team %q has %d mons, want %d", team.Name, len(team.Picks), engine.TeamSize)
		}
	}
}

// TestTeamLibrary_EveryMonCanAttack is the anti-regression guard for the exact
// bug that motivated this library: "first 4 moves" handed mons like Snorlax and
// Mewtwo sets with no real attacking move, turning games into noise. Assert
// every Pokemon on every team carries at least one non-status move.
//
// Non-status, not power>0: fixed-damage moves like Seismic Toss have Power 0
// but are the whole point of a wall like Chansey — they attack, so they count.
func TestTeamLibrary_EveryMonCanAttack(t *testing.T) {
	d := loadDex(t)
	lib, err := LoadTeamLibrary(libraryPath, d)
	if err != nil {
		t.Fatalf("load library: %v", err)
	}
	for _, team := range lib.Teams {
		for _, p := range team.Picks {
			sp := d.Species[p.DexNo]
			canAttack := false
			for _, mid := range p.MoveIDs {
				if d.Moves[mid].Category != "status" {
					canAttack = true
					break
				}
			}
			if !canAttack {
				t.Fatalf("team %q: %s has only status moves (%v)", team.Name, sp.Name, p.MoveIDs)
			}
		}
	}
}
