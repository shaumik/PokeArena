//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/illusion.js.
//
// None of these cases survive. Zoroark is not in this 80-species dex and has
// no stand-in — Illusion is a species-identity ability in the same way
// Transform is Ditto's, since the whole mechanic is "this Pokemon is displayed
// as a different one". Illusion is also absent from the ability set, and every
// assertion upstream reads a `|-end|p1a: Zoroark|Illusion` protocol line, for
// which this engine emits nothing at all.
//
// Five of the six cases are additionally out of scope on their own: four are
// Gen 8 Dynamax / G-Max and one is a Gen 7 Z-move.

func TestAbilitiesIllusion(t *testing.T) {
	describe(t, "Illusion", func(g *psg) {
		g.skip("should not wear off when switching out",
			"Zoroark is not in this 80-species dex and Illusion is not modeled")
		g.skip(`should not instantly wear off before Dynamaxing`,
			"Dynamax")
		g.skip(`should prevent the user from Dynamaxed when Illusioning as a Pokemon that cannot Dynamax`,
			"Dynamax")
		g.skip(`should be able to wear off normally while Dynamaxed`,
			"Dynamax")
		g.skip(`should Illusion as the regular Dynamax version of G-Max Pokemon while Dynamaxed`,
			"Dynamax")
		g.skip(`should instantly wear off before using a Z-move`,
			"Z-moves")
	})
}
