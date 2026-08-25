package eval

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
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

// TestTeamLibrary_NaturesDoNotHurt is the curation guard for the mistake a
// spread makes easiest: a nature that lowers the very stat its holder attacks
// with. A Timid physical attacker or an Adamant special one is a 10% penalty
// on the mon's whole job, and nothing in the engine or the validator objects
// — the team is perfectly legal, just quietly worse.
//
// Fixed-damage moves are exempt on purpose: Seismic Toss deals damage equal to
// the user's level regardless of Attack, which is exactly why Chansey can run
// a -Atk nature without paying for it.
func TestTeamLibrary_NaturesDoNotHurt(t *testing.T) {
	d := loadDex(t)
	lib, err := LoadTeamLibrary(libraryPath, d)
	if err != nil {
		t.Fatalf("load library: %v", err)
	}
	// Which derived stat each damaging move actually scales off.
	statForCategory := map[domain.Category]string{
		domain.CatPhysical: "atk",
		domain.CatSpecial:  "spatk",
	}
	checked := 0
	for _, team := range lib.Teams {
		for _, p := range team.Picks {
			if p.Nature == "" {
				continue
			}
			nature, ok := d.Natures[p.Nature]
			if !ok {
				t.Fatalf("team %q: unknown nature %q", team.Name, p.Nature)
			}
			checked++
			if nature.Minus == "" {
				continue
			}
			sp := d.Species[p.DexNo]
			for _, mid := range p.MoveIDs {
				m := d.Moves[mid]
				stat, damaging := statForCategory[m.Category]
				if !damaging || m.HasFlag("fixed-damage-level") {
					continue
				}
				if stat == nature.Minus {
					t.Errorf("team %q: %s is %s (-%s) but attacks with %s, a %s move",
						team.Name, sp.Name, nature.Name, nature.Minus, mid, m.Category)
				}
			}
		}
	}
	if checked == 0 {
		t.Error("no natured picks found — this guard is checking nothing")
	}
}
