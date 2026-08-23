//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/hyperbeam.js.
//
// The modern case came across as written: Snorlax and Alakazam are both in
// this dex, and No Guard is set exactly as upstream sets it, because Hyper
// Beam is 90% accurate and a miss would leave nothing to recharge from.
//
// The `Hyper Beam [Gen 1]` block skips whole. Every case in it turns on a
// Gen 1 quirk — no recharge after a KO or a broken Substitute, partial
// trapping moves eating the recharge turn, the Wrap PP underflow to 63, the
// frozen recharge soft-lock — and this engine has gen 9 data with no gen-mod
// layer to express any of it.

func TestMovesHyperBeam(t *testing.T) {
	describe(t, "Hyper Beam", func(g *psg) {
		g.it("should always force a recharge turn", func(p *ps) {
			p.battle(
				team{{Species: "snorlax", Ability: "noguard", Moves: mv("hyperbeam", "tackle")}},
				team{{Species: "alakazam", Moves: mv("substitute")}},
			)
			p.turn()
			// Upstream wraps the choice that would throw in assert.cantMove;
			// here the recharge turn's choice set is read directly. The point
			// of the case is that the Substitute soaking the beam does not
			// excuse the recharge, so Tackle must be unselectable.
			p.cantMove(0, "tackle", "a recharging Snorlax should not be able to select Tackle")
		})
	})

	describe(t, "Hyper Beam [Gen 1]", func(g *psg) {
		g.skip("should not force a recharge turn after KOing a Pokemon", "gen 1 mechanics")
		g.skip("should not force a recharge turn after breaking a Substitute", "gen 1 mechanics")
		g.skip("should force a recharge turn after damaging, but not breaking a Substitute", "gen 1 mechanics")
		g.skip("Partial trapping moves negate recharge turns (recharging Pokemon is slower))", "gen 1 mechanics")
		g.skip("Partial trapping moves negate recharge turns (recharging Pokemon is faster)", "gen 1 mechanics")
		g.skip("Hyper Beam Wrap underflow glitch", "gen 1 mechanics")
		g.skip("Hyper Beam automatic selection glitch", "gen 1 mechanics")
		g.skip("should be soft-locked if it was frozen during the recharge turn", "gen 1 mechanics")
		g.skip("should be freed from soft-locked if thawed by a fire move during the recharge turn", "gen 1 mechanics")
		g.skip("should not be freed from soft-locked if unfrozen by Haze during the recharge turn", "gen 1 mechanics")
	})
}
