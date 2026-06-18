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

// TestChoiceSpecsBoostsSpecial: Choice Specs multiplies special damage by 1.5
// and leaves physical damage untouched (the mirror of Choice Band).
func TestChoiceSpecsBoostsSpecial(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[143]) // Snorlax
	def := buildPokemon(d, d.Species[143])
	phys := d.Moves["tackle"]    // Normal physical
	spec := d.Moves["water-gun"] // Water special

	basePhys := ExpectedDamage(d, &atk, &def, phys, nil, nil, nil)
	baseSpec := ExpectedDamage(d, &atk, &def, spec, nil, nil, nil)

	atk.Item = ItemChoiceSpecs
	specsPhys := ExpectedDamage(d, &atk, &def, phys, nil, nil, nil)
	specsSpec := ExpectedDamage(d, &atk, &def, spec, nil, nil, nil)

	if specsSpec*100 < baseSpec*145 || specsSpec*100 > baseSpec*155 {
		t.Errorf("Choice Specs special: %d → %d, want ~1.5× (base*3/2 = %d)", baseSpec, specsSpec, baseSpec*3/2)
	}
	if specsPhys != basePhys {
		t.Errorf("Choice Specs changed physical damage: %d → %d, want unchanged", basePhys, specsPhys)
	}
}

// TestChoiceSpecsLocks: Specs reuses the shared choice-lock — the first move
// commits the holder until it switches out.
func TestChoiceSpecsLocks(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Item = ItemChoiceSpecs
	s.Active(0).Moves = []MoveSlot{{MoveID: "water-gun", PP: 25, MaxPP: 25}, {MoveID: "splash", PP: 40, MaxPP: 40}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if got := s.Active(0).Volatiles.ChoiceLockMoveID; got != "water-gun" {
		t.Fatalf("ChoiceLockMoveID = %q, want water-gun", got)
	}
}

// TestChoiceScarfBoostsSpeed: Choice Scarf raises effective speed 1.5×.
func TestChoiceScarfBoostsSpeed(t *testing.T) {
	d := loadDex(t)
	p := buildPokemon(d, d.Species[143]) // Snorlax
	base := effectiveSpeed(&p, nil)
	p.Item = ItemChoiceScarf
	scarfed := effectiveSpeed(&p, nil)
	if scarfed != int(float64(base)*1.5) {
		t.Errorf("Choice Scarf speed: %d → %d, want %d (1.5×)", base, scarfed, int(float64(base)*1.5))
	}
}

// TestChoiceScarfFlipsTurnOrder: a slower holder with Choice Scarf outspeeds a
// faster foe, and the scarf still locks the holder into its move.
func TestChoiceScarfFlipsTurnOrder(t *testing.T) {
	d := loadDex(t)
	// Articuno (144, Spe 85) is naturally slower than Aerodactyl (142, Spe 130).
	s, err := NewBattle(d, "b", "P1", []int{144}, "P2", []int{142}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	slow, fast := s.Active(0), s.Active(1)
	if effectiveSpeed(slow, nil) >= effectiveSpeed(fast, nil) {
		t.Skip("fixture assumption broken: side 0 is not the slower mon")
	}
	slow.Item = ItemChoiceScarf
	if effectiveSpeed(slow, nil) <= effectiveSpeed(fast, nil) {
		t.Errorf("scarfed speed %d should exceed foe %d", effectiveSpeed(slow, nil), effectiveSpeed(fast, nil))
	}

	slow.Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}, {MoveID: "splash", PP: 40, MaxPP: 40}}
	fast.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if got := s.Active(0).Volatiles.ChoiceLockMoveID; got != "tackle" {
		t.Errorf("Choice Scarf lock = %q, want tackle", got)
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

// TestLifeOrbBoostsDamage: Life Orb multiplies all damaging moves by 1.3×
// (physical and special alike).
func TestLifeOrbBoostsDamage(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[143])
	def := buildPokemon(d, d.Species[143])
	for _, mv := range []string{"tackle", "water-gun"} {
		m := d.Moves[mv]
		base := ExpectedDamage(d, &atk, &def, m, nil, nil, nil)
		atk.Item = ItemLifeOrb
		boosted := ExpectedDamage(d, &atk, &def, m, nil, nil, nil)
		atk.Item = ItemNone
		if boosted*100 < base*125 || boosted*100 > base*135 {
			t.Errorf("Life Orb %s: %d → %d, want ~1.3× (base*13/10 = %d)", mv, base, boosted, base*13/10)
		}
	}
}

// lifeOrbBattle: side 0's lead holds Life Orb; side 1 acts harmlessly. The
// holder is set to full HP so the 1/10 recoil is exact.
func lifeOrbBattle(t *testing.T, ability AbilityKind, move string) (*domain.Dex, *BattleState) {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	h := s.Active(0)
	h.Item = ItemLifeOrb
	h.Ability = ability
	h.Moves = []MoveSlot{{MoveID: move, PP: 20, MaxPP: 20}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	return d, s
}

// TestLifeOrbRecoil: after a damaging move connects, the holder loses 1/10 of
// its max HP.
func TestLifeOrbRecoil(t *testing.T) {
	d, s := lifeOrbBattle(t, AbilityNone, "tackle")
	h := s.Active(0)
	want := h.MaxHP - h.MaxHP/10
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.Active(0).HP != want {
		t.Errorf("Life Orb recoil: holder HP = %d, want %d (full %d − 1/10)", s.Active(0).HP, want, h.MaxHP)
	}
}

// TestLifeOrbSheerForceNoRecoil: a Sheer Force holder using a secondary-effect
// move (Sheer-Force-boosted) takes NO Life Orb recoil — the canonical quirk.
func TestLifeOrbSheerForceNoRecoil(t *testing.T) {
	d, s := lifeOrbBattle(t, "sheer-force", "thunderbolt") // thunderbolt has a secondary
	full := s.Active(0).MaxHP
	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.Active(0).HP != full {
		t.Errorf("Sheer Force + Life Orb took recoil: HP = %d, want %d (no recoil)", s.Active(0).HP, full)
	}
	if logHas(log, "Life Orb") {
		t.Errorf("Life Orb recoil logged under Sheer Force: %v", logTexts(log))
	}
}

// TestLifeOrbSheerForceNoSecondaryStillRecoils: a Sheer Force holder using a
// move WITHOUT a secondary isn't Sheer-Force-boosted, so Life Orb recoil
// applies normally — the precise boundary of the quirk.
func TestLifeOrbSheerForceNoSecondaryStillRecoils(t *testing.T) {
	d, s := lifeOrbBattle(t, "sheer-force", "tackle") // tackle has no secondary
	want := s.Active(0).MaxHP - s.Active(0).MaxHP/10
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.Active(0).HP != want {
		t.Errorf("Sheer Force + no-secondary move: HP = %d, want %d (recoil should apply)", s.Active(0).HP, want)
	}
}

// TestLifeOrbMagicGuardNoRecoil: Magic Guard blocks Life Orb recoil (indirect
// damage) while keeping the damage boost.
func TestLifeOrbMagicGuardNoRecoil(t *testing.T) {
	d, s := lifeOrbBattle(t, "magic-guard", "tackle")
	full := s.Active(0).MaxHP
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.Active(0).HP != full {
		t.Errorf("Magic Guard + Life Orb took recoil: HP = %d, want %d", s.Active(0).HP, full)
	}
}

// focusSashBattle: side 1 (defender) holds Focus Sash; side 0 lands a
// guaranteed OHKO — the defender's SpD is pinned to 1 so any special hit far
// exceeds its HP, removing roll variance. Side 0 uses Surf; side 1 splashes.
func focusSashBattle(t *testing.T) (*domain.Dex, *BattleState) {
	t.Helper()
	d := loadDex(t)
	s, err := NewBattle(d, "b", "P1", []int{143}, "P2", []int{143}, 1)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Moves = []MoveSlot{{MoveID: "surf", PP: 15, MaxPP: 15}}
	def := s.Active(1)
	def.Item = ItemFocusSash
	def.Stats.SpD = 1
	def.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	return d, s
}

// TestFocusSashSurvivesLethal: a full-HP holder survives an OHKO at 1 HP and
// the sash is consumed.
func TestFocusSashSurvivesLethal(t *testing.T) {
	d, s := focusSashBattle(t)
	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})

	def := s.Active(1)
	if def.Fainted {
		t.Fatal("Focus Sash holder fainted; should have survived at 1 HP")
	}
	if def.HP != 1 {
		t.Errorf("survivor HP = %d, want 1", def.HP)
	}
	if def.Item != ItemNone {
		t.Errorf("sash not consumed: item = %q", def.Item)
	}
	if !logHas(log, "Focus Sash") {
		t.Errorf("expected Focus Sash log, got %v", logTexts(log))
	}
}

// TestFocusSashConsumedOnce: after the sash saves the holder, a second lethal
// hit (now below full HP, sash gone) KOs it.
func TestFocusSashConsumedOnce(t *testing.T) {
	d, s := focusSashBattle(t)
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if s.Active(1).Fainted {
		t.Fatal("holder should have survived the first hit")
	}
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !s.Active(1).Fainted {
		t.Error("holder should have fainted to the second hit (sash already spent)")
	}
}

// TestFocusSashRequiresFullHP: a holder below full HP isn't saved, and the sash
// stays unconsumed (it never fired).
func TestFocusSashRequiresFullHP(t *testing.T) {
	d, s := focusSashBattle(t)
	def := s.Active(1)
	def.HP = def.MaxHP - 1 // not full
	ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
	if !s.Active(1).Fainted {
		t.Error("below-full-HP holder should not be saved by Focus Sash")
	}
	if s.Active(1).Item != ItemFocusSash {
		t.Errorf("unfired sash should remain: item = %q", s.Active(1).Item)
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

// TestItemOfInertUntilWired: the plumbing contract that catalog membership ≠
// engine behavior. The whole curated catalog is now modeled (TestItemCoverage
// guards that), so this probes the contract with a slug the registry doesn't
// know — and with holding nothing — both of which must yield a nil record so
// every dispatcher no-ops.
func TestItemOfInertUntilWired(t *testing.T) {
	d := loadDex(t)
	unmodeled := buildPokemonFromPick(d, d.Species[143], d.Species[143].Moves[:1], "", "")
	unmodeled.Item = "some-future-item" // not in itemRegistry
	if itemOf(&unmodeled) != nil {
		t.Error("an unmodeled item slug must yield a nil record (inert hold)")
	}
	none := buildPokemonFromPick(d, d.Species[143], d.Species[143].Moves[:1], "", "")
	if itemOf(&none) != nil {
		t.Error("a Pokémon holding nothing must have a nil item record")
	}
}
