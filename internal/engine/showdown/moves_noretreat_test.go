//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/noretreat.js.
//
// No Retreat is not in this dataset, so both cases report the missing move
// rather than the boost they are about. They are written out in full so they
// measure the right thing the day it lands.
//
// Wynaut and Caterpie both take their stand-in rows (Hypno and Butterfree).
// Butterfree is the faster of the two, so Block lands before No Retreat in the
// second case, which is the ordering the case depends on: the user has to be
// trapped by something else *before* it moves for the second use to be legal.

func TestMovesNoRetreat(t *testing.T) {
	describe(t, "No Retreat", func(g *psg) {
		g.it("should not allow usage multiple times in a row normally", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("noretreat")}},
				team{{Species: "Caterpie", Moves: mv("splash")}},
			)
			p.turn()
			p.turn()
			p.statStage(p.mine(), "atk", 1,
				"the second No Retreat should fail against the trap the first one set")
		})

		g.it("should allow usage multiple times in a row normally if it has the trapped volatile", func(p *ps) {
			p.battle(
				team{{Species: "Wynaut", Moves: mv("noretreat")}},
				team{{Species: "Caterpie", Moves: mv("block")}},
			)
			p.turn()
			p.turn()
			p.statStage(p.mine(), "atk", 2,
				"a user already trapped by Block never gains No Retreat's own trap, so it may use it again")
		})
	})
}
