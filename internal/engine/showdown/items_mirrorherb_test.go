//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/mirrorherb.js.
//
// Mirror Herb is not in this item set; the one case that is not doubles keeps it
// and the missing-item failure is the finding. Sleep Talk is not in this dataset
// and is inert filler for the Anger Point holder, so Splash stands in for it.
// Snorlax and Primeape are both in this dex, so the fixture is otherwise the
// upstream one.

func TestItemsMirrorHerb(t *testing.T) {
	describe(t, "Mirror Herb", func(g *psg) {
		g.it("should copy Anger Point", func(p *ps) {
			p.battle(
				team{{Species: "Snorlax", Item: "Mirror Herb", Moves: mv("stormthrow")}},
				team{{Species: "Primeape", Ability: "Anger Point", Moves: mv("splash")}},
			)
			if p.state() == nil {
				return
			}
			p.turn()
			p.statStage(p.mine(), "atk", 6, "Mirror Herb should copy the maxed Attack")
		})

		g.skip("should only copy the effective boost after the +6 cap", "doubles")

		g.skip("should copy all 'simultaneous' boosts from multiple opponents", "doubles")

		g.skip("should wait for most entrance abilities before copying all their (opposing) boosts", "doubles")
	})
}
