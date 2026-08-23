//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/griseousorb.js.
//
// A single Gen 4 describe block, built with common.gen(4). Griseous Orb's
// ability lock is a Gen 4-only rule and there is no gen-mod layer here, so the
// block skips whole; the orb is also outside this item set and Dhelmise outside
// this dex, either of which would sink the case on its own.

func TestItemsGriseousOrb(t *testing.T) {
	describe(t, "Griseous Orb [Gen 4]", func(g *psg) {
		g.skip("should prevent changing the holder's ability", "gen 4 mechanics")
	})
}
