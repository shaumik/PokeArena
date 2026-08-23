//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/leftovers.js.
//
// The file is a single Gen 2 describe block, built with common.gen(2) and
// turning on Gen 2's switch-in residual order and level-100 HP totals. There is
// no gen-mod layer here, so the block skips whole.

func TestItemsLeftovers(t *testing.T) {
	describe(t, "Leftovers [Gen 2]", func(g *psg) {
		g.skip("should heal after switch", "gen 2 mechanics")
	})
}
