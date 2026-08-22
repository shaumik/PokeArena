//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/haze.js.
//
// Nothing in this file survives translation. Every case upstream is a
// generation-mod battle — twelve under common.gen(1), one under common.gen(4)
// and one under common.gen(3) — and each is about a Haze that no longer
// exists: Gen 1 Haze cured status, cleared Focus Energy, Reflect, Light
// Screen, Leech Seed, confusion and Disable, and Gen 4 Haze still cleared
// Focus Energy. This engine has Gen 9 data and no gen-mod layer, so there is
// no honest way to ask any of these questions of it.
//
// The two upstream cases marked it.skip are carried over as skips too, so the
// ledger holds one row per `it` in the original file whatever its state
// upstream.
//
// What is *not* here is any test of modern Haze (clear every stat stage on
// both sides): upstream does not test it in this file, and a port does not
// invent cases.

func TestMovesHaze(t *testing.T) {
	describe(t, "[Gen 1] Haze", func(g *psg) {
		g.skip("should remove stat changes", "gen 1 mechanics")
		g.skip("should remove opponent's status", "gen 1 mechanics")
		g.skip("should not remove the user's status", "gen 1 mechanics")
		g.skip("should remove focus energy in gen 1", "gen 1 mechanics")
		g.skip("should remove reflect and light screen", "gen 1 mechanics")
		g.skip("should remove Leech Seed and confusion", "gen 1 mechanics")
		g.skip("should remove Disable", "gen 1 mechanics")
		g.skip("should still make previously disabled Pokemon (on the same turn) with 1 move use Struggle",
			"gen 1 mechanics")
		g.skip("should convert toxic poisoning to regular poisoning for the user and effectively reset the toxic counter",
			"gen 1 mechanics")
		g.skip("should not remove substitute from either side", "gen 1 mechanics")
		g.skip("should not allow a previously sleeping opponent to move on the same turn", "gen 1 mechanics")
		g.skip("should not allow a previously frozen opponent to move on the same turn", "gen 1 mechanics")
	})

	describe(t, "[Gen 4] Haze", func(g *psg) {
		g.skip("should remove focus energy in gen 4", "gen 4 mechanics")
		// Despite the block's name this one is a common.gen(3) battle.
		g.skip("should not remove focus energy in other gens", "gen 3 mechanics")
	})
}
