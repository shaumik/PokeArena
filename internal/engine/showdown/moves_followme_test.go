//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/followme.js.
//
// Follow Me redirects the moves aimed at the user's partner onto the user, so
// the mechanic needs at least two Pokemon on a side to exist at all. This
// engine is singles — Side.Active is one index — and the move is not in the
// dataset either. Every case here is triples, doubles, free-for-all, or a gen 3
// doubles battle, and none of them has a singles form: with one active per side
// there is nothing to redirect away from.

func TestMovesFollowMe(t *testing.T) {
	describe(t, "Follow Me", func(g *psg) {
		g.skip("should redirect single-target moves towards it if it is a valid target", "triples")
		g.skip("should not redirect self-targeting moves", "doubles")
		g.skip("should allow redirection even if the user is the last slot of a double battle", "doubles")
		g.skip("should redirect single-target moves towards it if it is a valid target in FFA", "free-for-all")
		g.skip("[Gen 3] should continue to redirect moves after the user is knocked out and replaced",
			"gen 3 mechanics")
	})
}
