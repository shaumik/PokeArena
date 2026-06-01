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
type TeamPick struct {
	DexNo   int      `json:"dex_no"`
	MoveIDs []string `json:"moves"`
	Ability string   `json:"ability,omitempty"`
}

// Team composition limits enforced by ValidateTeam. The numbers are
// load-bearing in the doc (§5), not knobs.
const (
	TeamSize = 6
	MovesMin = 1
	MovesMax = 4
)

// ValidateTeam enforces the picker-room rules in order — first failure
// short-circuits. Error messages name the slot (1-indexed) and the
// offending field so the SPA can highlight directly.
//
// Order matters: cheaper, more-likely checks first so the typical user
// mistake produces the most helpful error.
func ValidateTeam(picks []TeamPick, dex *domain.Dex) error {
	if len(picks) != TeamSize {
		return fmt.Errorf("team must have %d Pokémon, got %d", TeamSize, len(picks))
	}
	seenSpecies := make(map[int]bool, TeamSize)
	for i, p := range picks {
		sp, ok := dex.Species[p.DexNo]
		if !ok {
			return fmt.Errorf("slot %d: unknown Pokédex number %d", i+1, p.DexNo)
		}
		if seenSpecies[p.DexNo] {
			return fmt.Errorf("slot %d: %s already on team (Species Clause)", i+1, sp.Name)
		}
		seenSpecies[p.DexNo] = true
		if err := validateMovesForSpecies(i+1, sp, p.MoveIDs, dex); err != nil {
			return err
		}
		if err := validateAbilityForSpecies(i+1, sp, p.Ability); err != nil {
			return err
		}
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
