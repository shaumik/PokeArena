package engine

import (
	"fmt"

	"github.com/shaumik/PokeArena/internal/domain"
)

// team_warnings.go: the checks that do not reject a team.
//
// Legality and soundness are different questions. A Timid Machamp is a legal
// team member and a bad one — the nature costs it 10% of the stat every one of
// its moves scales off, and until now nothing said so. The validator answered
// "can I submit this", never "did I mean this", so a builder learned about the
// difference by losing.
//
// These rules are deliberately few. A warning is only worth emitting when it is
// almost certainly a slip rather than a plan; anything arguable belongs in a
// strategy guide, not in a submit response. Both rules here already existed as
// curation guards over the shipped team library (internal/eval/library_test.go),
// which is the evidence that they catch real mistakes: they were written
// because someone made these mistakes in data review.

// statForCategory maps a damaging move's category to the stat it scales off.
// Status moves are absent: they scale off nothing, so no nature can hurt them.
var statForCategory = map[domain.Category]string{
	domain.CatPhysical: "atk",
	domain.CatSpecial:  "spatk",
}

// warnTeamPick runs every soundness check over one slot.
func warnTeamPick(slot int, sp domain.Species, p TeamPick, dex *domain.Dex, rep *TeamReport) {
	warnNatureFightsMoves(slot, sp, p, dex, rep)
	warnNoDamagingMove(slot, sp, p, dex, rep)
}

// warnNatureFightsMoves catches the mistake a spread makes easiest: a nature
// that lowers the very stat its holder attacks with.
//
// Fixed-damage moves are exempt on purpose. Seismic Toss deals damage equal to
// the user's level whatever its Attack is, which is exactly why a Chansey can
// run a minus-Attack nature and pay nothing for it — flagging that would train
// builders to ignore the warning.
func warnNatureFightsMoves(slot int, sp domain.Species, p TeamPick, dex *domain.Dex, rep *TeamReport) {
	if p.Nature == "" {
		return
	}
	nature, ok := dex.Natures[p.Nature]
	if !ok || nature.Minus == "" {
		// Unknown natures are a problem, already reported; neutral natures
		// lower nothing.
		return
	}
	for _, mid := range p.MoveIDs {
		m, ok := dex.Moves[mid]
		if !ok {
			continue // unknown move: the move check owns it
		}
		stat, damaging := statForCategory[m.Category]
		if !damaging || m.HasFlag("fixed-damage-level") {
			continue
		}
		if stat != nature.Minus {
			continue
		}
		rep.addWarning(Warning{
			Slot: slot, Species: sp.Name, Field: "nature",
			Message: fmt.Sprintf(
				"slot %d (%s): %s lowers %s by 10%%, but %s is a %s move that attacks with %s — legal, just weaker than intended",
				slot, sp.Name, nature.Name, nature.Minus, m.Name, m.Category, stat),
		})
		return // one warning per slot; the point lands the first time
	}
}

// warnNoDamagingMove catches a moveset that cannot win. An all-status set is
// legal and occasionally deliberate (a dedicated staller), so this warns rather
// than rejects — but on a six-slot team it is far more often an oversight.
func warnNoDamagingMove(slot int, sp domain.Species, p TeamPick, dex *domain.Dex, rep *TeamReport) {
	if len(p.MoveIDs) == 0 {
		return // the move check owns an empty set
	}
	known := 0
	for _, mid := range p.MoveIDs {
		m, ok := dex.Moves[mid]
		if !ok {
			continue
		}
		known++
		if _, damaging := statForCategory[m.Category]; damaging {
			return
		}
	}
	if known == 0 {
		return // nothing resolvable to judge; the move check owns it
	}
	rep.addWarning(Warning{
		Slot: slot, Species: sp.Name, Field: "moves",
		Message: fmt.Sprintf(
			"slot %d (%s): every move is a status move, so this Pokémon cannot deal damage — legal, but it can never take a KO",
			slot, sp.Name),
	})
}
