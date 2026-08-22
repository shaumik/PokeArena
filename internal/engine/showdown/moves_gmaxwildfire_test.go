//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/gmaxwildfire.js.
//
// Nothing crosses. G-Max Wildfire is a Gigantamax move: both cases build a
// gen 8 doubles battle, Dynamax a Gigantamax Charizard and read the residual
// it leaves on the opposing side. This engine has no Dynamax layer, no gen-mod
// layer and no second active slot, and there is no part of the mechanic left
// once Dynamax is taken out of it.

func TestMovesGMaxWildfire(t *testing.T) {
	describe(t, "G-Max Wildfire", func(g *psg) {
		g.skip("should not damage Fire-types", "Dynamax")
		g.skip("should deal damage for four turns, including the fourth turn", "Dynamax")
	})
}
