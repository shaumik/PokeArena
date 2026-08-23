//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/mefirst.js.
//
// Nothing crosses. Me First was cut in Gen 8, so both cases build gen 7
// battles and this engine has no gen-mod layer. (The move is also absent from
// the dataset, which is the same fact from the other side.)

func TestMovesMeFirst(t *testing.T) {
	describe(t, "Me First", func(g *psg) {
		g.skip("should be selectable even if the user is Taunted or holds Assault Vest",
			"gen 7 mechanics")
		g.skip("should not copy recharge turns from moves like Hyper Beam", "gen 7 mechanics")
	})
}
