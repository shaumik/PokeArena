//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/dynamax.js.
//
// Nothing came across. Dynamax is not modeled: there is no `dynamax` suffix in
// the choice grammar, no HP doubling, no three-turn volatile to revert, and no
// Max Move layer that rewrites a chosen move into its Max version. Every case
// in the file turns on one of those — Max Move side effects, Max Move base
// power and category, which base moves a Dynamaxed Pokémon may still select,
// and when the volatile ends.
//
// The whole file is built on `common.gen(8).createBattle`, and one case is also
// doubles, but Dynamax is the blocker in every one, so that is the reason
// recorded.
//
// `should not remove the variable to Dynamax on forced switches` is `it.skip`
// upstream. It is kept here as a skip like the rest so the ledger carries the
// name; if the upstream case is ever re-enabled the port already has a row for
// it.
//
// The nested `Hacked Max Moves` describe is flattened into a sibling block,
// keeping its own name as the ledger key.

func TestMiscDynamax(t *testing.T) {
	describe(t, "Dynamax", func(g *psg) {
		g.skip("Max Move effects should not be suppressed by Sheer Force", "Dynamax")
		g.skip("Max Move versions of disabled moves should not be disabled, except by Assault Vest", "Dynamax")
		g.skip("Max Move weather activates even if foe faints", "Dynamax")
		g.skip("Max Move weather activates before Sand Spit", "Dynamax")
		g.skip("makes Liquid Voice stop working", "Dynamax")
		g.skip("should execute in order of updated speed when 2 or more Pokemon are Dynamaxing", "Dynamax")
		g.skip("should revert before the start of the 4th turn, not as an end-of-turn effect on the 3rd turn", "Dynamax")
		g.skip("should be impossible to Dynamax when all the base moves are disabled", "Dynamax")
		g.skip("should not allow the user to select max moves with 0 base PP remaining", "Dynamax")
		g.skip("should force the user to use Struggle if certain effects are disabling all of its base moves", "Dynamax")
		g.skip("should not remove the variable to Dynamax on forced switches", "Dynamax")
	})

	describe(t, "Hacked Max Moves", func(g *psg) {
		g.skip("should not activate Max Move side effects when used without Dynamaxing", "Dynamax")
		g.skip("should treat Max Moves as 0 BP when used without Dynamaxing", "Dynamax")
		g.skip("should treat Max Moves as physical moves when used without Dynamaxing", "Dynamax")
		g.skip("should prevent effects that affect regular Max Moves, like Sleep Talk and Instruct", "Dynamax")
	})
}
