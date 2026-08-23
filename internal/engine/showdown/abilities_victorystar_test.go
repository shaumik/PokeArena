//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/victorystar.js.
//
// Every case is a doubles battle whose whole subject is the ally's copy of the
// ability stacking with the user's own, and every assertion is made through
// battle.onEvent('Accuracy', ...), a rigged-RNG hook this harness does not
// have. Victory Star is also not in this dataset. All three skip as doubles.

func TestAbilitiesVictoryStar(t *testing.T) {
	describe(t, "Victory Star", func(g *psg) {
		g.skip("can boost accuracy twice if both the user and ally have the ability", "doubles")
		g.skip("should not boost the accuracy of opponents", "doubles")
		g.skip("should boost accuracy even when used against allies", "doubles")
	})
}
