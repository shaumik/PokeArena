package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// clauses.go is the format layer: the rules that make a match a *format*
// rather than just a legal team. Basic legality (this species exists, it can
// learn this move, this spread is inside the caps) lives in
// team_validation.go; the clauses here are the competitive conventions
// layered on top, and they are the difference between a battle and a ranked
// battle.
//
// Five clauses, four checked at team build and one during the battle:
//
//	Species   no duplicate species          (team build — team_validation.go)
//	Item      no duplicate held items       (team build)
//	Evasion   no evasion-raising moves      (team build)
//	OHKO      no one-hit-KO moves           (team build)
//	Sleep     one Pokémon asleep per side   (battle — sleepClauseBlocks)
//
// ValidateTeam applies StandardClauses, so every team submitted through the
// picker gets all four build-time clauses. ValidateTeamWithClauses exists for
// the callers that genuinely need to relax one — the item-sweep integration
// test hands the same item to a whole side on purpose, because its subject is
// the item's mechanics and not the format.
//
// Sleep Clause is deliberately *not* in that struct. It is enforced during
// the battle, and a BattleState can be built in several places (tests, the AI
// view reconstruction) that would all have to remember to carry the flag; a
// zero value there would quietly turn the clause off in the simulation while
// leaving it on in the real match, which is the worst possible split. It is
// unconditional, and if a format ever needs it optional the flag belongs on
// BattleState with a default that survives a zero value.
//
// Species Clause predates this file and is still enforced inline with the
// rest of the per-slot walk, where its "already on team" bookkeeping belongs;
// the Clauses flag gates it there.
//
// Why each one exists, since a reader a year from now will want to know:
//
//   - Evasion turns a game of prediction into a game of dice. Double Team is
//     the canonical example of a move that is not strong but is unpleasant.
//   - OHKO moves are a 30% coin flip that ignores every stat on the board.
//   - Item Clause stops six Leftovers.
//   - Sleep Clause is the big one: sleep is rng.Range(2,4) turns of doing
//     nothing at all, and without a cap a team can legally be slept in its
//     entirety. Spore is 100% accurate.

// Clauses selects which format rules are in force. The zero value enforces
// nothing, which is why every entry point that isn't explicitly relaxing a
// rule should go through StandardClauses rather than build a Clauses literal.
type Clauses struct {
	Species bool // no two of the same species
	Item    bool // no two of the same held item
	Evasion bool // no moves that raise the user's evasion
	OHKO    bool // no one-hit-KO moves
}

// StandardClauses is the format this arena plays: every build-time clause on.
// Sleep Clause is not listed because it is not optional — see the file
// comment.
func StandardClauses() Clauses {
	return Clauses{Species: true, Item: true, Evasion: true, OHKO: true}
}

// moveRaisesEvasion reports whether m raises its user's evasion, in any of
// the three effect blocks. Foe-side evasion *drops* (Sweet Scent) are fine —
// the clause is about becoming hard to hit, not about the stat.
func moveRaisesEvasion(m domain.Move) bool {
	// A Primary block only lands on the user when the move targets self;
	// Sweet Scent's -1 evasion on the foe must not trip this.
	if m.Primary != nil && m.Target == domain.TargetSelf && m.Primary.Boosts["evasion"] > 0 {
		return true
	}
	if m.Self != nil && m.Self.Boosts["evasion"] > 0 {
		return true
	}
	// A secondary's boosts normally land on the target, where an evasion
	// raise would be a gift to the opponent rather than something the clause
	// is aimed at. A self-targeted one (Effect.Self) is the user's own boost
	// and counts. No curated move carries that shape today; it is read here
	// so the clause can't be walked around the day one does.
	for _, sec := range m.Secondaries {
		if sec.Self && sec.Boosts["evasion"] > 0 {
			return true
		}
	}
	return false
}

// validateClauseMoves enforces the Evasion and OHKO clauses over one slot's
// move list. Both are per-move properties read off the dataset, so neither
// needs a curated list that can drift from the data.
func validateClauseMoves(slot int, sp domain.Species, moveIDs []string, dex *domain.Dex, c Clauses) error {
	if !c.Evasion && !c.OHKO {
		return nil
	}
	for _, id := range moveIDs {
		m, ok := dex.Moves[id]
		if !ok {
			continue // unknown moves are the move validator's problem, not ours
		}
		if c.Evasion && moveRaisesEvasion(m) {
			return fmt.Errorf("slot %d (%s): %s raises evasion (Evasion Clause)", slot, sp.Name, m.Name)
		}
		if c.OHKO && m.OHKO != "" {
			return fmt.Errorf("slot %d (%s): %s is a one-hit KO move (OHKO Clause)", slot, sp.Name, m.Name)
		}
	}
	return nil
}

// validateItemClause refuses a second copy of a held item. Holding nothing is
// not an item, so any number of slots may be empty-handed.
func validateItemClause(slot int, sp domain.Species, item string, seen map[string]int) error {
	if item == "" {
		return nil
	}
	if first, dup := seen[item]; dup {
		return fmt.Errorf("slot %d (%s): %s is already held by slot %d (Item Clause)",
			slot, sp.Name, item, first)
	}
	seen[item] = slot
	return nil
}

// sleepClauseBlocks reports whether the Sleep Clause refuses a foe-induced
// sleep on side: it does when that side already has a Pokémon asleep,
// anywhere on the team, including the bench.
//
// Self-inflicted sleep is exempt and never reaches here — Rest goes through
// inflictStatus directly, while this is consulted from inflictStatusFrom,
// which by construction has a source on the other side. That split is the
// canonical exemption, and it is a property of the call graph rather than a
// flag anyone has to remember to pass.
//
// A Pokémon that is already asleep does not count itself: it is the one
// allowed sleeper, and re-sleeping it fails on the one-status rule anyway.
func sleepClauseBlocks(s *BattleState, side int, target *Pokemon) bool {
	if s == nil {
		return false
	}
	for i := range s.Sides[side].Team {
		p := &s.Sides[side].Team[i]
		if p == target || p.Fainted {
			continue
		}
		if p.Status == StatusSleep {
			return true
		}
	}
	return false
}
