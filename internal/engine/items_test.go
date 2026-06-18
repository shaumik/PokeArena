package engine

import (
	"testing"

	"pokearena/internal/domain"
)

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

// TestChoiceBandBoostsPhysical: Choice Band multiplies physical damage by 1.5
// and leaves special damage untouched.
func TestChoiceBandBoostsPhysical(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[143]) // Snorlax (Normal)
	def := buildPokemon(d, d.Species[143])
	phys := d.Moves["tackle"]    // Normal physical
	spec := d.Moves["water-gun"] // Water special

	basePhys := ExpectedDamage(d, &atk, &def, phys, nil, nil, nil)
	baseSpec := ExpectedDamage(d, &atk, &def, spec, nil, nil, nil)

	atk.Item = ItemChoiceBand
	bandPhys := ExpectedDamage(d, &atk, &def, phys, nil, nil, nil)
	bandSpec := ExpectedDamage(d, &atk, &def, spec, nil, nil, nil)

	// Physical: ~1.5× (allow integer-truncation slack around base*3/2).
	if bandPhys*100 < basePhys*145 || bandPhys*100 > basePhys*155 {
		t.Errorf("Choice Band physical: %d → %d, want ~1.5× (base*3/2 = %d)", basePhys, bandPhys, basePhys*3/2)
	}
	// Special: unchanged.
	if bandSpec != baseSpec {
		t.Errorf("Choice Band changed special damage: %d → %d, want unchanged", baseSpec, bandSpec)
	}
}

// choiceBandBattle: side 0's lead holds Choice Band with [tackle, splash];
// both sides otherwise act harmlessly. Side 0 has a bench mon to switch to.
func choiceBandBattle(t *testing.T) (*domain.Dex, *BattleState) {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143, 144}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Item = ItemChoiceBand
	s.Active(0).Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}, {MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	return d, s
}

// TestChoiceLockSetAndEnforced: the first move locks the holder in; afterwards
// LegalActions offers only that move (plus switches).
func TestChoiceLockSetAndEnforced(t *testing.T) {
	d, s := choiceBandBattle(t)
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	if got := s.Active(0).Volatiles.ChoiceLockMoveID; got != "tackle" {
		t.Fatalf("ChoiceLockMoveID = %q, want tackle", got)
	}
	moves := 0
	for _, a := range LegalActions(s, 0) {
		if a.Kind == ActionMove {
			moves++
			if a.Index != 0 {
				t.Errorf("locked holder offered move index %d, want only 0", a.Index)
			}
		}
	}
	if moves != 1 {
		t.Errorf("locked holder has %d move options, want 1", moves)
	}
}

// TestChoiceLockRedirectsSubmittedMove: once locked, submitting a different
// move index still resolves the locked move (PP proves it).
func TestChoiceLockRedirectsSubmittedMove(t *testing.T) {
	d, s := choiceBandBattle(t)
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	tackleBefore := s.Active(0).Moves[0].PP
	splashBefore := s.Active(0).Moves[1].PP
	// Submit splash (index 1) — the lock must redirect to tackle (index 0).
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 1}, {Kind: ActionMove, Index: 0}})

	if s.Active(0).Moves[0].PP != tackleBefore-1 {
		t.Errorf("tackle PP = %d, want %d (locked move should have fired)", s.Active(0).Moves[0].PP, tackleBefore-1)
	}
	if s.Active(0).Moves[1].PP != splashBefore {
		t.Errorf("splash PP = %d, want %d (submitted move should be ignored)", s.Active(0).Moves[1].PP, splashBefore)
	}
}

// TestChoiceLockClearsOnSwitch: switching out drops the lock (Volatiles wipe).
func TestChoiceLockClearsOnSwitch(t *testing.T) {
	d, s := choiceBandBattle(t)
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.Active(0).Volatiles.ChoiceLockMoveID == "" {
		t.Fatal("expected lock set after first move")
	}
	// Switch side 0 to its bench mon.
	ResolveTurn(d, s, [2]Action{{Kind: ActionSwitch, Index: 1}, {Kind: ActionMove, Index: 0}})
	if got := s.Sides[0].Team[0].Volatiles.ChoiceLockMoveID; got != "" {
		t.Errorf("lock survived switch-out: %q", got)
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
