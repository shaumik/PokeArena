//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/choiceitem.js.
//
// The file's one case is about the Choice lock surviving a Dynamax, built with
// common.gen(8). Dynamax is not modeled and there is no gen-mod layer, so it
// skips.

func TestItemsChoiceItem(t *testing.T) {
	describe(t, "Choice Items", func(g *psg) {
		g.skip("should restore the same Choice lock after dynamax ends", "Dynamax")
	})
}
