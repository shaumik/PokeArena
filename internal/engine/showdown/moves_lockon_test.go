//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/lockon.js.
//
// Gen 2 block, and it is a level case on top of that: it asks whether a
// locked-on OHKO move can hit a target above the user's level, with the user
// at level 1 and the target at level 100. This engine has no gen-mod layer and
// every Pokemon is level 50, so the level relationship the case measures
// cannot exist. Lock-On itself now ships, so the move is no longer part of the
// reason — the generation and the fixed level are the whole of it.

func TestMovesLockOn(t *testing.T) {
	describe(t, "Lock-On", func(g *psg) {
		g.skip("should not allow OHKO moves to hit a higher level in Gen 2",
			"gen 2 mechanics, and level is fixed at 50 so the level gap the case turns on cannot be set up")
	})
}
