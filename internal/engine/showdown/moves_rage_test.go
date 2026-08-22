//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/rage.js.
//
// The whole describe is a Gen 1 block: it pins the Gen 1 Rage accuracy bug,
// reading a `rage` volatile's raw 255/127 accuracy byte. This engine has no
// gen-mod layer and its Rage carries no such counter.

func TestMovesRage(t *testing.T) {
	describe(t, "Rage [Gen 1]", func(g *psg) {
		g.skip("Rage accuracy bug", "gen 1 mechanics")
	})
}
