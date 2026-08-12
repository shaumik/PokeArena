package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// TeamPick is one slot in a submitted team: a species (by Pokédex number),
// the 1–4 moves the trainer chose from that species' learn list, and an
// optional ability slug. Wire shape — JSON tags are part of the picker-room
// protocol (docs/team-picker-room.md §4).
//
// Ability is optional: empty string means "use slot 0", preserving the
// pre-ability-picker behavior so older clients keep working without changes.
// When non-empty it must be one of the species' declared abilities.
//
// Item is optional: empty string means the Pokémon holds nothing. When
// non-empty it must name an item in the curated catalog (dex.Items). Unlike
// abilities, items aren't species-restricted — any catalog item is legal on
// any Pokémon.
//
// EVs, IVs, and Nature are the optional training spread. All three are
// absent-means-default, and the defaults reproduce the fixed spread every
// Pokémon had before spreads existed:
//
//	EVs    nil → no EVs at all
//	IVs    nil → perfect 31s
//	Nature ""  → neutral (no stat modified)
//
// EVs and IVs are pointers rather than values because their meaningful
// default is not the zero value — a team submitting `"ivs": {}` is asking
// for 0 IVs across the board, which is legal and terrible, and must not be
// confused with omitting the field. Nature gets away with a plain string
// because "" is not a nature slug.
type TeamPick struct {
	DexNo   int           `json:"dex_no"`
	MoveIDs []string      `json:"moves"`
	Ability string        `json:"ability,omitempty"`
	Item    string        `json:"item,omitempty"`
	EVs     *domain.Stats `json:"evs,omitempty"`
	IVs     *domain.Stats `json:"ivs,omitempty"`
	Nature  string        `json:"nature,omitempty"`
	// Gender is "male" / "female" / "genderless". Empty leaves it to the
	// battle: a species with a fixed gender gets that one, and anything else
	// is rolled once from the battle seed against its birth ratio. Pick it
	// when it matters — an all-male side is immune to its own Attract.
	Gender string `json:"gender,omitempty"`
}

// Clone returns a deep copy: the move slice and both spread pointers are
// freshly allocated, so the copy shares no mutable state with the original.
//
// This exists because a hand-written field-by-field copy silently drops
// whatever field was added last — TeamPool.Pick did exactly that to the
// spread fields the day they were introduced. Anything that needs an
// independent TeamPick should call this rather than rebuild the literal.
func (p TeamPick) Clone() TeamPick {
	out := p
	out.MoveIDs = append([]string(nil), p.MoveIDs...)
	if p.EVs != nil {
		evs := *p.EVs
		out.EVs = &evs
	}
	if p.IVs != nil {
		ivs := *p.IVs
		out.IVs = &ivs
	}
	return out
}

// ClonePicks deep-copies a whole roster.
func ClonePicks(picks []TeamPick) []TeamPick {
	out := make([]TeamPick, len(picks))
	for i, p := range picks {
		out[i] = p.Clone()
	}
	return out
}

// Team composition limits enforced by ValidateTeam. The numbers are
// load-bearing in the doc (§5), not knobs.
const (
	TeamSize = 6
	MovesMin = 1
	MovesMax = 4
)

// ValidateTeam enforces the picker-room rules in order — first failure
// short-circuits. That includes the format clauses in clauses.go: Species,
// Item, Evasion and OHKO are team-build rules and are checked here; Sleep
// Clause is a battle-time rule and lives in inflictStatusFrom. Error messages name the slot (1-indexed) and the
// offending field so the SPA can highlight directly.
//
// Order matters: cheaper, more-likely checks first so the typical user
// mistake produces the most helpful error.
func ValidateTeam(picks []TeamPick, dex *domain.Dex) error {
	return ValidateTeamWithClauses(picks, dex, StandardClauses())
}

// ValidateTeamWithClauses is ValidateTeam with the format rules selectable.
// Basic legality — species exists, move is learnable, spread is inside the
// caps, gender is possible — is always enforced; only the clauses in
// clauses.go are gated. Use ValidateTeam unless you are deliberately playing
// a different format.
func ValidateTeamWithClauses(picks []TeamPick, dex *domain.Dex, c Clauses) error {
	if len(picks) != TeamSize {
		return fmt.Errorf("team must have %d Pokémon, got %d", TeamSize, len(picks))
	}
	seenSpecies := make(map[int]bool, TeamSize)
	seenItems := make(map[string]int, TeamSize)
	for i, p := range picks {
		sp, ok := dex.Species[p.DexNo]
		if !ok {
			return fmt.Errorf("slot %d: unknown Pokédex number %d", i+1, p.DexNo)
		}
		if c.Species && seenSpecies[p.DexNo] {
			return fmt.Errorf("slot %d: %s already on team (Species Clause)", i+1, sp.Name)
		}
		seenSpecies[p.DexNo] = true
		if err := validateMovesForSpecies(i+1, sp, p.MoveIDs, dex); err != nil {
			return err
		}
		if err := validateAbilityForSpecies(i+1, sp, p.Ability); err != nil {
			return err
		}
		if err := validateItem(i+1, sp, p.Item, dex); err != nil {
			return err
		}
		if err := validateGender(i+1, sp, p.Gender); err != nil {
			return err
		}
		// Format clauses (see clauses.go). Checked after basic legality so a
		// team with a typo'd move is told about the typo, not about a clause.
		if err := validateClauseMoves(i+1, sp, p.MoveIDs, dex, c); err != nil {
			return err
		}
		if c.Item {
			if err := validateItemClause(i+1, sp, p.Item, seenItems); err != nil {
				return err
			}
		}
		if err := validateSpread(i+1, sp, p, dex); err != nil {
			return err
		}
	}
	return nil
}

// validateGender refuses a gender the species can't be — a female Nidoking,
// a male Chansey, a Magnemite of any gender at all. Absent is legal and
// means "let the battle decide" (see rollGenders).
//
// A species carrying no gender list at all is data from before genders
// existed; it accepts nothing but the default, rather than pretending any
// pick is fine.
func validateGender(slot int, sp domain.Species, gender string) error {
	if gender == "" {
		return nil
	}
	if sp.CanBeGender(gender) {
		return nil
	}
	return fmt.Errorf("slot %d (%s): gender %q is not possible for this species (%v)",
		slot, sp.Name, gender, sp.Genders)
}

// validateSpread enforces the training-spread rules: EVs 0..252 per stat and
// 510 in total, IVs 0..31 per stat, and a nature slug that exists in the
// dataset. Absent fields are legal — they mean the default spread.
//
// The per-stat EV cap is checked before the total so the more specific error
// wins: a team with 300 EVs in one stat gets told which stat is illegal, not
// that its budget is over.
func validateSpread(slot int, sp domain.Species, p TeamPick, dex *domain.Dex) error {
	if p.EVs != nil {
		for _, key := range domain.StatKeys {
			v, _ := p.EVs.Get(key)
			if v < 0 || v > MaxEVPerStat {
				return fmt.Errorf("slot %d (%s): %s EVs %d out of range 0–%d",
					slot, sp.Name, key, v, MaxEVPerStat)
			}
		}
		if total := p.EVs.Total(); total > MaxEVTotal {
			return fmt.Errorf("slot %d (%s): EVs total %d, over the %d budget",
				slot, sp.Name, total, MaxEVTotal)
		}
	}
	if p.IVs != nil {
		for _, key := range domain.StatKeys {
			v, _ := p.IVs.Get(key)
			if v < 0 || v > MaxIV {
				return fmt.Errorf("slot %d (%s): %s IV %d out of range 0–%d",
					slot, sp.Name, key, v, MaxIV)
			}
		}
	}
	if p.Nature != "" {
		if _, ok := dex.Natures[p.Nature]; !ok {
			return fmt.Errorf("slot %d (%s): nature %q is not a known nature", slot, sp.Name, p.Nature)
		}
	}
	return nil
}

// validateItem allows empty (no held item) and otherwise requires the slug to
// be in the curated catalog. Items are not species-restricted, so the only
// check is catalog membership.
func validateItem(slot int, sp domain.Species, item string, dex *domain.Dex) error {
	if item == "" {
		return nil
	}
	if _, ok := dex.Items[item]; !ok {
		return fmt.Errorf("slot %d (%s): item %q is not in the catalog", slot, sp.Name, item)
	}
	return nil
}

// validateAbilityForSpecies allows empty (slot 0 default) and otherwise
// requires the slug to appear in sp.Abilities. Slots are not numbered in the
// wire format — the agent picks by slug, which matches how species data
// reads.
func validateAbilityForSpecies(slot int, sp domain.Species, ability string) error {
	if ability == "" {
		return nil
	}
	for _, a := range sp.Abilities {
		if a == ability {
			return nil
		}
	}
	return fmt.Errorf("slot %d (%s): ability %q is not in this species' list %v",
		slot, sp.Name, ability, sp.Abilities)
}

func validateMovesForSpecies(slot int, sp domain.Species, moveIDs []string, dex *domain.Dex) error {
	if len(moveIDs) < MovesMin || len(moveIDs) > MovesMax {
		return fmt.Errorf("slot %d (%s): must pick %d–%d moves, got %d",
			slot, sp.Name, MovesMin, MovesMax, len(moveIDs))
	}
	learn := make(map[string]bool, len(sp.Moves))
	for _, id := range sp.Moves {
		learn[id] = true
	}
	seen := make(map[string]bool, len(moveIDs))
	for _, mid := range moveIDs {
		if seen[mid] {
			return fmt.Errorf("slot %d (%s): move %q listed twice", slot, sp.Name, mid)
		}
		seen[mid] = true
		if _, ok := dex.Moves[mid]; !ok {
			return fmt.Errorf("slot %d (%s): unknown move %q", slot, sp.Name, mid)
		}
		if !learn[mid] {
			return fmt.Errorf("slot %d (%s): cannot learn %q", slot, sp.Name, mid)
		}
	}
	return nil
}
