//go:build showdown

package showdown

import "testing"

// Ported from test/sim/rulesets/endlessbattleclause.js.
//
// Nothing came across. Endless Battle Clause is a staleness tracker: from turn
// 100 on, Showdown marks a Pokémon stale when it recovers PP or HP from a
// source it recycled itself, distinguishes staleness it inflicted on itself
// from staleness a foe inflicted on it, and awards the win to the side that did
// not cause the loop. This engine has none of that machinery — its clauses are
// Species, Item, Evasion, OHKO and Sleep — so seven of the eight cases have no
// rule to measure.
//
// The eighth, `should allow for a maximum of 1000 turns`, is the closest
// call: turn.go does cap a battle, at 300 turns, and decides it on remaining
// team HP rather than declaring the loop a loss. That is a different rule with
// a different number and a different outcome, so translating the case would
// mean rewriting the only thing it asserts. Recorded here as a skip and flagged
// for triage instead.
//
// Upstream also reaches for `battle.endTurn()` to fast-forward past turn 100,
// for a level-1 Blissey, and for Sunkern, Furret and Ampharos, none of which
// this port has a counterpart for; Floral Healing and Entrainment are not in
// the 538-move set either. Those are secondary — the clause is the blocker in
// every case.

func TestRulesetsEndlessBattleClause(t *testing.T) {
	describe(t, "Endless Battle Clause (slow)", func(g *psg) {
		g.skip("should trigger on an infinite loop",
			"Endless Battle Clause is not one of this engine's clauses")
		g.skip("should not trigger by both Pokemon eating a Leppa Berry they started with",
			"Endless Battle Clause is not one of this engine's clauses")
		g.skip("should only cause the battle to end if either side cannot switch to a non-stale Pokemon and at least one staleness is externally inflicted",
			"Endless Battle Clause is not one of this engine's clauses")
		g.skip("Fling should cause externally inflicted staleness",
			"Endless Battle Clause is not one of this engine's clauses")
		g.skip("Entrainment should cause externally inflicted staleness",
			"Endless Battle Clause is not one of this engine's clauses")
		g.skip("Entrainment's externally inflicted staleness should go away on switch",
			"Endless Battle Clause is not one of this engine's clauses")
		g.skip("should allow for a maximum of 1000 turns",
			"Endless Battle Clause is not one of this engine's clauses; the turn cap here is 300 and is decided on team HP")
		g.skip("Skill Swap should remove the user's staleness",
			"Endless Battle Clause is not one of this engine's clauses")
	})
}
