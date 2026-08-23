//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/pledgemoves.js.
//
// A Pledge combo needs two of your own Pokemon to move in the same turn, so
// every case here is `gameType: 'doubles'` and none of them survives the
// crossing to a one-slot engine. The two later cases are additionally gen-7
// Z-move and gen-8 Max Move blocks.
//
// Independent of the game type, none of Grass Pledge, Fire Pledge or Water
// Pledge is in this dataset, so even a singles re-statement would have no move
// to make.

func TestMiscPledgeMoves(t *testing.T) {
	describe(t, "Pledge Moves", func(g *psg) {
		g.skip("should not combine if one of the users is forced to use a non-pledge move on its turn",
			"doubles: a Pledge combo needs two allies acting in the same turn, and the pledge moves are not in this dataset")

		g.skip("should not start a Pledge combo for Z-moves",
			"doubles and Z-moves: neither a second active slot nor a Z-move exists here")

		g.skip("should not start a Pledge combo for Max Moves",
			"doubles and Dynamax: neither a second active slot nor a Max Move exists here")
	})
}
