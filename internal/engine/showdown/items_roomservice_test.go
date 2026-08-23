//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/roomservice.js.
//
// Room Service is not in this item set; both ported cases are about it, so they
// keep it and the missing-item failure is the finding. Sleep Talk is not in the
// dataset either and is the holder's idle move, so Splash stands in for it.
//
// Slowpoke goes through the stand-in table to Slowbro. Whimsicott is only the
// Trick Room setter — Trick Room's -7 priority makes it move last whatever its
// Speed, so nothing here turns on Prankster or on Whimsicott's typing, and
// Clefable carries it.

func TestItemsRoomService(t *testing.T) {
	describe(t, "Room Service", func(g *psg) {
		g.it("should activate when Trick Room is set", func(p *ps) {
			p.battle(
				team{{Species: "slowpoke", Item: "roomservice", Moves: mv("splash")}},
				team{{Species: "whimsicott", As: "Clefable", Item: "roomservice", Moves: mv("trickroom")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.statStage(p.mine(), "spe", -1, "")
			p.statStage(p.foe(), "spe", -1, "")
		})

		g.skip("should activate after entrance Abilities",
			"Ditto is not in this 80-species dex and Transform/Imposter are not modeled")

		g.it("should not trigger Defiant", func(p *ps) {
			p.battle(
				team{{Species: "slowpoke", Ability: "defiant", Item: "roomservice", Moves: mv("splash")}},
				team{{Species: "whimsicott", As: "Clefable", Moves: mv("trickroom")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.statStage(p.mine(), "atk", 0, "Room Service is the holder's own drop, so Defiant should stay quiet")
			p.statStage(p.mine(), "spe", -1, "")
		})
	})
}
