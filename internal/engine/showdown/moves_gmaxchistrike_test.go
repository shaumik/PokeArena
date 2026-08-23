//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/gmaxchistrike.js.
//
// Nothing crosses. G-Max Chi Strike is the Gigantamax form of Machamp's
// Fighting move, and every case builds a Gen 8 battle, Gigantamaxes a Machamp
// and then reads the crit-rate stage the G-Max move leaves on its side. This
// engine has no Dynamax layer and no gen-mod layer, so the setup cannot be
// stated at all — and the assertions themselves live in a
// `battle.onEvent('ModifyCritRatio')` hook, which has no counterpart here
// either: the harness can watch a battle's state and log, not the modifier
// pipeline behind one damage calculation.
//
// The first two cases are doubles on top of that, and the fourth spends three
// turns on Max Guard waiting out the Dynamax timer.

func TestMovesGMaxChiStrike(t *testing.T) {
	describe(t, "G-Max Chi Strike", func(g *psg) {
		g.skip("should boost the user and its ally's critical hit rate by 1 stage", "Dynamax")
		g.skip("should provide a crit boost independent of Focus Energy", "Dynamax")
		g.skip("should be copied by Psych Up", "Dynamax")
		g.skip("should not be passed by Baton Pass", "Dynamax")
	})
}
