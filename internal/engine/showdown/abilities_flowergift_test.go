//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/flowergift.js.
//
// Nothing here survives. The first two cases are doubles and turn on the
// boost Flower Gift gives an *ally*, which this engine has no slot for. The
// third is Gen 8 Dynamax.
//
// The fourth is singles and would otherwise be portable, but Cherrim is not in
// this dex and has no stand-in, Flower Gift is not in the ability set, and the
// case is stated as `assert.species(active, 'Cherrim')` — an assertion that
// the sun did *not* flip the forme. Forme changes are not modeled at all, so a
// substituted body would answer that trivially and record a pass for a reason
// that has nothing to do with Mold Breaker.

func TestAbilitiesFlowerGift(t *testing.T) {
	describe(t, "Flower Gift", func(g *psg) {
		g.skip(`should boost allies' Attack and Special Defense stats`,
			"doubles")
		g.skip(`should still work if Cherrim transforms into something with Flower Gift without originally having it`,
			"doubles")
		g.skip(`should not trigger if the Pokemon was KOed`,
			"Dynamax")
		g.skip(`should not trigger if dragged in by a Mold Breaker Pokemon`,
			"Cherrim is not in this 80-species dex and Flower Gift's forme change is not modeled")
	})
}
