package engine

import "testing"

// leftoversBattle sets up a 1v1 where side 0 holds Leftovers and both sides
// have a harmless move, so end-of-turn residuals are the only HP change.
func leftoversBattle(t *testing.T) *BattleState {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1) // Snorlax mirror
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Item = ItemLeftovers
	s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	return s
}

// TestLeftoversHealsEndOfTurn: a hurt holder recovers 1/16 max HP after the
// turn resolves; the bare foe does not.
func TestLeftoversHealsEndOfTurn(t *testing.T) {
	d := loadDex(t)
	s := leftoversBattle(t)
	holder := s.Active(0)
	holder.HP = holder.MaxHP / 2
	want := holder.HP + holder.MaxHP/16
	foeBefore := s.Active(1).HP

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Active(0).HP != want {
		t.Errorf("Leftovers heal: HP = %d, want %d (1/16 of %d)", s.Active(0).HP, want, holder.MaxHP)
	}
	if s.Active(1).HP != foeBefore {
		t.Errorf("bare foe HP changed: %d → %d", foeBefore, s.Active(1).HP)
	}
}

// TestLeftoversNoOverheal: a full-HP holder neither heals nor logs.
func TestLeftoversNoOverheal(t *testing.T) {
	d := loadDex(t)
	s := leftoversBattle(t)
	full := s.Active(0).MaxHP

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if s.Active(0).HP != full {
		t.Errorf("full-HP holder HP = %d, want %d (no overheal)", s.Active(0).HP, full)
	}
	if logHas(log, "Leftovers") {
		t.Errorf("Leftovers logged a heal at full HP: %v", logTexts(log))
	}
}

// TestItemCatalogLoads: the curated catalog is parsed into the dex and carries
// the items the first phases will model.
func TestItemCatalogLoads(t *testing.T) {
	d := loadDex(t)
	if len(d.Items) == 0 {
		t.Fatal("item catalog is empty — items.json not loaded")
	}
	for _, id := range []string{"leftovers", "choice-band", "choice-specs", "choice-scarf", "life-orb", "focus-sash"} {
		if _, ok := d.Items[id]; !ok {
			t.Errorf("catalog missing %q", id)
		}
	}
}

// TestValidateItem: a catalog item is legal on any species; an unknown slug is
// rejected; an empty item (holds nothing) is fine.
func TestValidateItem(t *testing.T) {
	d := loadDex(t)

	picks := validPicks(t, d)
	picks[0].Item = "leftovers"
	if err := ValidateTeam(picks, d); err != nil {
		t.Errorf("valid item should pass, got %v", err)
	}

	picks[0].Item = "not-a-real-item"
	if err := ValidateTeam(picks, d); err == nil {
		t.Error("unknown item should be rejected")
	}

	picks[0].Item = ""
	if err := ValidateTeam(picks, d); err != nil {
		t.Errorf("empty item (holds nothing) should pass, got %v", err)
	}
}

// TestBuildPokemonFromPick_ItemAttached: the picked item rides onto the battle
// Pokémon; an empty item leaves it holding nothing.
func TestBuildPokemonFromPick_ItemAttached(t *testing.T) {
	d := loadDex(t)
	sp := d.Species[143] // Snorlax
	moves := sp.Moves[:1]

	held := buildPokemonFromPick(d, sp, moves, "", "leftovers")
	if held.Item != ItemKind("leftovers") {
		t.Errorf("held item = %q, want leftovers", held.Item)
	}

	bare := buildPokemonFromPick(d, sp, moves, "", "")
	if bare.Item != ItemNone {
		t.Errorf("bare Pokémon item = %q, want none", bare.Item)
	}
}

// TestItemOfInertUntilWired: items in the catalog with no registry entry are
// inert holds — itemOf returns nil so every (future) dispatcher no-ops. This is
// the plumbing contract: catalog membership ≠ engine behavior.
func TestItemOfInertUntilWired(t *testing.T) {
	d := loadDex(t)
	// focus-sash is in the catalog but not yet modeled — still an inert hold.
	p := buildPokemonFromPick(d, d.Species[143], d.Species[143].Moves[:1], "", "focus-sash")
	if itemOf(&p) != nil {
		t.Error("expected focus-sash to be inert (no registry entry yet)")
	}
	none := buildPokemonFromPick(d, d.Species[143], d.Species[143].Moves[:1], "", "")
	if itemOf(&none) != nil {
		t.Error("a Pokémon holding nothing must have a nil item record")
	}
}
