//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/megaevolution.js.
//
// Nothing came across. Mega Evolution is not modeled: there is no `mega`
// suffix in the choice grammar, no mega stone in the 128-item set, and no
// forme layer for the swap to land on. Every case here is about what the swap
// does — which ability the mega forme ends up with, that it may only happen
// once per battle, whether the new Speed and the new ability count for the turn
// the swap happens on, and whether the swap is allowed at all.
//
// The nested `Mega Rayquaza` describe is flattened into a sibling block, keeping
// its own name as the ledger key. Its four cases are additionally about format
// rule tables — Mega Rayquaza Clause, `gen6ubers` versus `gen9nationaldexag`,
// the `@@@-mega` custom-rule syntax — which is a subsystem this engine does not
// have; its only clauses are Species, Item, Evasion, OHKO and Sleep.
//
// Beyond that, none of Metagross, Wishiwashi, Charizard, Garchomp, Jirachi,
// Ninjask or Rayquaza reaches this port in a form that would answer the
// question even if the mechanic existed: the stand-ins that exist for some of
// them preserve typing, not formes.

func TestMiscMegaEvolution(t *testing.T) {
	describe(t, "Mega Evolution", func(g *psg) {
		g.skip("should overwrite normally immutable abilities", "mega evolution")
		g.skip("[Hackmons] should be able to override different formes but not same forme",
			"mega evolution")
		g.skip("should happen once", "mega evolution")
		g.skip("should modify speed/priority in gen 7+", "mega evolution")
		g.skip("should not break priority", "mega evolution")
	})

	describe(t, "Mega Rayquaza", func(g *psg) {
		g.skip("should be able to Mega Evolve if it knows Dragon Ascent",
			"mega evolution")
		g.skip("should be allowed to Mega Evolve in new gen formats allowing \"Past\" elements",
			"mega evolution — and format IDs are not a concept this engine has")
		g.skip("should not be allowed to Mega Evolve in formats that have the Mega Rayquaza Clause",
			"mega evolution — and Mega Rayquaza Clause is not a rule this engine has")
		g.skip("should implicitly add the Mega Rayquaza Clause when banned",
			"mega evolution — and format rule tables are not a concept this engine has")
	})
}
