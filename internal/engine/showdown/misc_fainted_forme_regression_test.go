//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/fainted-forme-regression.js.
//
// Nothing came across. The file exists to pin one rule — which forme a Pokémon
// is left in once it faints — and asks it fifteen times, of Mega Evolutions,
// Primal Reversion, Battle Bond Greninja, Arceus's plate formes, Disguise,
// Ice Face, Hunger Switch, Zero to Hero, Ogerpon's masks and Terapagos. This
// engine has no forme layer at all: a Pokémon is one dex entry for the whole
// battle, so there is no state for fainting to revert and no species to assert
// afterwards. None of the fifteen species involved is in the 80-species dex
// either, and a stand-in cannot help — the forme *is* the subject, which is the
// case the guide names as never substitutable.
//
// Three cases are additionally gen-modded (two on gen 7, one on gen 8), and
// several drive the reversion through Terastallization; the missing forme layer
// is the blocker that would remain in gen 9 without Tera, so that is what is
// recorded, except for the three cases whose subject is Mega Evolution itself.

func TestMiscFaintedFormeRegression(t *testing.T) {
	describe(t, "Fainted forme regression", func(g *psg) {
		g.skip("[Hackmons] should be able to revert between different mega evolutions", "mega evolution")
		g.skip("should revert Mega Evolutions", "mega evolution")
		g.skip("should revert Rayquaza-Mega", "mega evolution")
		g.skip("should revert Primal forms", "formes")
		g.skip("should revert Greninja-Ash and not allow it to transform again", "formes")
		g.skip("should not revert Arceus-forms to base Arceus", "formes")
		g.skip("should not revert Mimikyu-Busted to base Mimikyu", "formes")
		g.skip("Mimikyu should keep its disguise if it was not busted", "formes")
		g.skip("[Gen 8] should revert Mimikyu-Busted to base Mimikyu", "formes")
		g.skip("should not revert Eiscue-Noice to base Eiscue", "formes")
		g.skip("should revert Terastallized Morpeko-Hangry to base Morpeko", "formes")
		g.skip("should not revert Palafin-Hero to base Palafin", "formes")
		g.skip("should revert Ogerpon-Tera to base Ogerpon", "formes")
		g.skip("should not revert Terapagos-Terastal to base Terapagos", "formes")
		g.skip("should revert Terapagos-Stellar to base Terapagos", "formes")
	})
}
