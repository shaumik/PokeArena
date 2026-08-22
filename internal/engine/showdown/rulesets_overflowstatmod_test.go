//go:build showdown

package showdown

import "testing"

// Ported from test/sim/rulesets/overflowstatmod.js.
//
// Nothing came across. Overflow Stat Mod reproduces the 16-bit wraparound the
// Gen 8 cartridges show on stats above 654, and the case measures it on
// Eternatus-Eternamax, whose 250 base Defense is the only way to reach the
// ceiling. This engine has no format-rule layer for the mod to be switched on
// in, Eternatus has no dex entry and no stand-in row, and stats here are
// computed at a fixed level 50, where nothing in the 80-species dex comes near
// 654 in the first place.

func TestRulesetsOverflowStatMod(t *testing.T) {
	describe(t, "Overflow Stat Mod", func(g *psg) {
		g.skip("should cap stats at 654 after a positive nature",
			"the Overflow Stat Mod format rule is not modeled")
	})
}
