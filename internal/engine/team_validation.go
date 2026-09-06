package engine

import (
	"github.com/shaumik/PokeArena/internal/domain"
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

// ValidateTeam reports whether a team is legal, as a plain error.
//
// The error is a *TeamReport carrying EVERY problem, not just the first —
// Error() renders them as one multi-line message, so a caller that only prints
// the error already shows the whole list. Callers wanting the structured
// findings, or the non-blocking warnings, should call CheckTeam instead and
// read the report directly.
//
// That includes the format clauses in clauses.go: Species, Item, Evasion and
// OHKO are team-build rules and are checked here; Sleep Clause is a
// battle-time rule and lives in inflictStatusFrom.
func ValidateTeam(picks []TeamPick, dex *domain.Dex) error {
	return ValidateTeamWithClauses(picks, dex, StandardClauses())
}

// ValidateTeamWithClauses is ValidateTeam with the format rules selectable.
// Basic legality — species exists, move is learnable, spread is inside the
// caps, gender is possible — is always enforced; only the clauses in
// clauses.go are gated. Use ValidateTeam unless you are deliberately playing
// a different format.
func ValidateTeamWithClauses(picks []TeamPick, dex *domain.Dex, c Clauses) error {
	rep := CheckTeamWithClauses(picks, dex, c)
	if rep.OK() {
		// Return a nil error, not a non-nil interface holding a nil-ish
		// report: every caller here tests `err != nil`, and a typed nil would
		// make a legal team read as rejected.
		return nil
	}
	return rep
}
