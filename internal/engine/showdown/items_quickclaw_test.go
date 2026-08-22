//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/quickclaw.js.
//
// Both cases in the file are built with common.gen(2) / common.gen(3) and turn
// on how the activation roll was shared between holders in those generations.
// There is no gen-mod layer here, so both skip as a block.

func TestItemsQuickClaw(t *testing.T) {
	describe(t, "Quick Claw", func(g *psg) {
		g.skip("[Gen 2] shares its activation roll with every holder on any given turn",
			"gen 2 mechanics")

		g.skip("[Gen 3] causes Speed ties with every holder when activated",
			"gen 3 mechanics")
	})
}
