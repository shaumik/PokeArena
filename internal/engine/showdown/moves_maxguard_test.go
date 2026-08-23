//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/maxguard.js.
//
// Max Guard is the Dynamax form of Protect: it only exists on a Dynamaxed
// Pokemon, and Dynamax is not modeled here. Every case in the file needs it,
// and the second and third also need a second active slot, so the block skips
// whole. The move itself is not in this dataset either.

func TestMovesMaxGuard(t *testing.T) {
	describe(t, "Max Guard", func(g *psg) {
		g.skip("should be disallowed by Taunt", "Dynamax")
		g.skip("should allow Feint to damage the user, but not break the protection effect", "Dynamax")
		g.skip("should block certain moves that bypass Protect", "Dynamax")
	})
}
