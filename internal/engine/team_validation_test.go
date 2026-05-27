package engine

import (
	"strings"
	"testing"

	"pokearena/internal/domain"
)

// validPicks builds a 6-pick team straight from the first six species in
// the dex, each with up to 4 moves taken from its learnset. Robust to
// dataset shuffles — no hard-coded move IDs to drift. Each test case
// mutates the baseline to exercise exactly one rule.
func validPicks(t *testing.T, d *domain.Dex) []TeamPick {
	t.Helper()
	all := d.AllSpecies()
	if len(all) < TeamSize {
		t.Fatalf("test fixture: dex only has %d species, need %d", len(all), TeamSize)
	}
	picks := make([]TeamPick, TeamSize)
	for i := 0; i < TeamSize; i++ {
		sp := all[i]
		if len(sp.Moves) == 0 {
			t.Fatalf("test fixture: %s has no learnset moves", sp.Name)
		}
		m := sp.Moves
		if len(m) > MovesMax {
			m = m[:MovesMax]
		}
		picks[i] = TeamPick{DexNo: sp.DexNo, MoveIDs: append([]string(nil), m...)}
	}
	return picks
}

// findIllegalMove returns a move that exists in the dex but is not in
// sp's learnset — i.e. the canonical "learnset" failure case.
func findIllegalMove(t *testing.T, d *domain.Dex, sp domain.Species) string {
	t.Helper()
	in := map[string]bool{}
	for _, id := range sp.Moves {
		in[id] = true
	}
	for id := range d.Moves {
		if !in[id] {
			return id
		}
	}
	t.Fatalf("test fixture: species %s learns every move in the dex", sp.Name)
	return ""
}

func TestValidateTeam_Happy(t *testing.T) {
	d := loadDex(t)
	if err := ValidateTeam(validPicks(t, d), d); err != nil {
		t.Fatalf("happy path: %v", err)
	}
}

func TestValidateTeam_WrongSize(t *testing.T) {
	d := loadDex(t)
	if err := ValidateTeam(validPicks(t, d)[:5], d); err == nil {
		t.Fatalf("5-slot team accepted; want size error")
	}
	// To exceed TeamSize we need a 7th species not already in validPicks
	// — grab one beyond the first six from the dex.
	all := d.AllSpecies()
	if len(all) < TeamSize+1 {
		t.Skipf("dex has only %d species; cannot build oversized team", len(all))
	}
	extra := all[TeamSize]
	tooMany := append(validPicks(t, d), TeamPick{DexNo: extra.DexNo, MoveIDs: []string{extra.Moves[0]}})
	if err := ValidateTeam(tooMany, d); err == nil {
		t.Fatalf("7-slot team accepted; want size error")
	}
}

func TestValidateTeam_UnknownSpecies(t *testing.T) {
	d := loadDex(t)
	p := validPicks(t, d)
	p[2].DexNo = 9999
	err := ValidateTeam(p, d)
	if err == nil || !strings.Contains(err.Error(), "9999") {
		t.Fatalf("want unknown-species error mentioning 9999, got %v", err)
	}
}

func TestValidateTeam_DuplicateSpecies(t *testing.T) {
	d := loadDex(t)
	p := validPicks(t, d)
	p[5] = p[0] // copy slot 0 into slot 5 — same DexNo, legal moves
	err := ValidateTeam(p, d)
	if err == nil || !strings.Contains(err.Error(), "Species Clause") {
		t.Fatalf("want Species Clause error, got %v", err)
	}
}

func TestValidateTeam_TooFewMoves(t *testing.T) {
	d := loadDex(t)
	p := validPicks(t, d)
	p[0].MoveIDs = nil
	if err := ValidateTeam(p, d); err == nil {
		t.Fatalf("zero moves accepted; want move-count error")
	}
}

func TestValidateTeam_TooManyMoves(t *testing.T) {
	d := loadDex(t)
	p := validPicks(t, d)
	// Pad slot 0's moves to 5 entries with any legal additional move.
	sp := d.Species[p[0].DexNo]
	for _, mid := range sp.Moves {
		alreadyIn := false
		for _, have := range p[0].MoveIDs {
			if have == mid {
				alreadyIn = true
				break
			}
		}
		if !alreadyIn {
			p[0].MoveIDs = append(p[0].MoveIDs, mid)
			if len(p[0].MoveIDs) > MovesMax {
				break
			}
		}
	}
	if len(p[0].MoveIDs) <= MovesMax {
		t.Skipf("species %s only has %d learnable moves; cannot exceed max", sp.Name, len(p[0].MoveIDs))
	}
	if err := ValidateTeam(p, d); err == nil {
		t.Fatalf("%d moves accepted; want move-count error", len(p[0].MoveIDs))
	}
}

func TestValidateTeam_DuplicateMove(t *testing.T) {
	d := loadDex(t)
	p := validPicks(t, d)
	if len(p[0].MoveIDs) < 2 {
		t.Skip("slot 0's species needs ≥2 moves to test duplicates")
	}
	p[0].MoveIDs[1] = p[0].MoveIDs[0]
	err := ValidateTeam(p, d)
	if err == nil || !strings.Contains(err.Error(), "listed twice") {
		t.Fatalf("want duplicate-move error, got %v", err)
	}
}

func TestValidateTeam_UnknownMove(t *testing.T) {
	d := loadDex(t)
	p := validPicks(t, d)
	p[0].MoveIDs[0] = "blast-burn-of-doom"
	err := ValidateTeam(p, d)
	if err == nil || !strings.Contains(err.Error(), "unknown move") {
		t.Fatalf("want unknown-move error, got %v", err)
	}
}

func TestValidateTeam_NotInLearnset(t *testing.T) {
	d := loadDex(t)
	p := validPicks(t, d)
	p[0].MoveIDs[0] = findIllegalMove(t, d, d.Species[p[0].DexNo])
	err := ValidateTeam(p, d)
	if err == nil || !strings.Contains(err.Error(), "cannot learn") {
		t.Fatalf("want learnset error, got %v", err)
	}
}
