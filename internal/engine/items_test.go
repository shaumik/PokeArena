package engine

import "testing"

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
	p := buildPokemonFromPick(d, d.Species[143], d.Species[143].Moves[:1], "", "leftovers")
	if itemOf(&p) != nil {
		t.Error("expected leftovers to be inert (no registry entry yet)")
	}
	none := buildPokemonFromPick(d, d.Species[143], d.Species[143].Moves[:1], "", "")
	if itemOf(&none) != nil {
		t.Error("a Pokémon holding nothing must have a nil item record")
	}
}
