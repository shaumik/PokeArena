//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/gmaxvolcalith.js.
//
// Nothing crosses. G-Max Volcalith is a Gigantamax move: every case builds a
// gen 8 doubles battle, Dynamaxes a Coalossal, and reads the residual the
// G-Max move leaves on the opposing side. This engine has no Dynamax layer, no
// second active slot and no gen-mod layer, and neither Coalossal nor the move
// is in the dataset. The whole file is recorded as skipped rather than half
// translated, since there is no part of the mechanic left once Dynamax is
// taken out of it.
//
// The third case is `it.skip` upstream as well; it is kept so the ledger has a
// row for it.

func TestMovesGMaxVolcalith(t *testing.T) {
	describe(t, "G-Max Volcalith", func(g *psg) {
		g.skip("should not damage Rock-types", "Dynamax")
		g.skip("should deal damage for four turns, including the fourth turn", "Dynamax")
		g.skip("should deal damage alongside Sea of Fire or G-Max Wildfire in the order those field effects were set",
			"Dynamax")
		g.skip("should damage Pokemon in order of Speed", "Dynamax")
		g.skip("should deal damage before Black Sludge recovery/damage", "Dynamax")
		g.skip("should deal damage before Grassy Terrain recovery", "Dynamax")
	})
}
