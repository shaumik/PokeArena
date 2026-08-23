//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/orderup.js.
//
// Order Up only does anything when a Tatsugiri is inside the user via
// Commander, which is a doubles-only ability and needs a second active slot.
// Tatsugiri, Dondozo and Order Up are all absent here besides.

func TestMovesOrderUp(t *testing.T) {
	describe(t, "Order Up", func(g *psg) {
		g.skip("should boost Dondozo's stat even if Sheer Force-boosted",
			"doubles: Commander needs an ally slot, and neither Tatsugiri nor Order Up exists in this dataset")
	})
}
