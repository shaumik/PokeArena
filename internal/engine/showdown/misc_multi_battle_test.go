//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/multi-battle.js.
//
// The single case is a four-sided free-for-all that forfeits two of the
// players mid-battle and then checks the turn counter. This engine has exactly
// two sides and no forfeit call, so neither the setup nor the assertion has a
// counterpart: `battle.lose('p2')` on a two-sided battle is simply the end of
// the battle, which measures nothing about turn bookkeeping.

func TestMiscMultiBattle(t *testing.T) {
	describe(t, "Free-for-all", func(g *psg) {
		g.skip("should support forfeiting",
			"free-for-all: this engine is singles, with one active slot and two sides, and has no forfeit action")
	})
}
