//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/boosterenergy.js.
//
// The one case needs a Paradox Pokemon whose Quark Drive picks its best stat on
// switch-in, after Sticky Web has already cut its Speed. Iron Bundle and
// Ribombee are outside this 80-species dex with no stand-in, and nothing in it
// carries Quark Drive, so there is no body the case could be rebuilt on.
// (Booster Energy and Sticky Web are also absent from the item and move sets.)

func TestItemsBoosterEnergy(t *testing.T) {
	describe(t, "Booster Energy", func(g *psg) {
		g.skip("should not activate before Sticky Web when switching in",
			"Iron Bundle is not in this 80-species dex and Quark Drive is not modeled")
	})
}
