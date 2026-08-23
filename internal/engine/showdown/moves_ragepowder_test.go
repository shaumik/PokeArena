//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/ragepowder.js.
//
// Nothing crosses. Rage Powder pulls the moves aimed at the user's partners
// onto the user, so the mechanic needs a second Pokemon on the side to exist
// at all; this engine is singles, with Side.Active a single index, and the
// move is not in the dataset either. All three upstream cases are triple
// battles that count how many of three attacks landed on each of three
// allies, and there is no singles form of "the attack was pulled away from
// your ally".
//
// The third case is additionally a Gen 5 battle asserting that the powder
// immunities Gen 6 introduced do not exist yet, which this engine's single
// generation of data cannot express.

func TestMovesRagePowder(t *testing.T) {
	describe(t, "Rage Powder", func(g *psg) {
		g.skip("should redirect single-target moves towards it if it is a valid target", "triples")
		g.skip("should not affect Pokemon with Powder immunities", "triples")
		g.skip("should have no Powder immunities in Gen 5", "gen 5 mechanics")
	})
}
