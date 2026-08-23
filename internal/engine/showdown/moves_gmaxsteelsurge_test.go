//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/gmaxsteelsurge.js.
//
// Nothing crosses. The single case Dynamaxes a Gigantamax Copperajah in a gen 8
// battle to lay G-Max Steelsurge, then reads the entry damage it does to Ice
// Face and Disguise. Dynamax, the gen-mod layer, both abilities and every
// species named are outside this engine.

func TestMovesGMaxSteelsurge(t *testing.T) {
	describe(t, "G-Max Steelsurge", func(g *psg) {
		g.skip("should deal 2x damage to Eiscue and Mimikyu", "Dynamax")
	})
}
