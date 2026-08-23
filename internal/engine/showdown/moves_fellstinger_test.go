//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/fellstinger.js. The upstream describe is spelled
// "Fell Stringer"; it is kept verbatim because it is half the ledger key.
//
// The case is not "Fell Stinger boosts on a KO" but "it still boosts when the
// KO happened on a target it was redirected onto", which needs an ally using
// Follow Me. Follow Me is not in this dataset and there is no slot to redirect
// from, so nothing of the case survives.

func TestMovesFellStinger(t *testing.T) {
	describe(t, "Fell Stringer", func(g *psg) {
		g.skip("should get a boost when KOing a Pokemon after redirection",
			"doubles: the case turns on an ally redirecting the move with Follow Me")
	})
}
