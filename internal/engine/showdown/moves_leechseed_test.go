//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/leechseed.js.
//
// The file's one case is about a seeded Pokemon ending up in the slot that is
// draining it, which takes an ally to Ally Switch with. Neither the second
// slot nor Ally Switch exists here.

func TestMovesLeechSeed(t *testing.T) {
	describe(t, "Leech Seed", func(g *psg) {
		g.skip("should heal and damage itself if it ends up in the same slot via Ally Switch",
			"doubles: the case turns on Ally Switch moving a seeded Pokemon into the draining slot")
	})
}
