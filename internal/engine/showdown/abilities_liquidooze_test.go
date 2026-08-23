//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/liquidooze.js.
//
// Liquid Ooze and Tentacruel are both in this dataset, so the two Gen 9 cases
// port literally. Serperior is not in the dex and has no stand-in row; it is
// the drainer here and nothing turns on more than its being a Grass-type
// attacker, which Tangela is. Sleep Talk is not in this dataset and is only
// filler, so Splash takes its place.
//
// The Gen 4 describe block skips as a block: there is no gen-mod layer.

func TestAbilitiesLiquidOoze(t *testing.T) {
	describe(t, "Liquid Ooze", func(g *psg) {
		g.it("should damage the target after it uses a draining move", func(p *ps) {
			p.battle(
				team{{Species: "tentacruel", Ability: "liquidooze", Moves: mv("splash")}},
				team{{Species: "serperior", As: "Tangela", Moves: mv("gigadrain")}},
			)
			p.turn()
			p.damaged(p.foe(), "draining a Liquid Ooze holder should hurt the drainer")
		})

		g.it(`should damage the target after taking damage from Leech Seed`, func(p *ps) {
			p.battle(
				team{{Species: "tentacruel", Ability: "liquidooze", Moves: mv("splash")}},
				team{{Species: "serperior", As: "Tangela", Ability: "noguard", Moves: mv("leechseed")}},
			)
			p.turn()
			p.damaged(p.foe(), "Leech Seed drains, so Liquid Ooze answers it too")
		})
	})

	describe(t, "Liquid Ooze [Gen 4]", func(g *psg) {
		g.skip("should damage the target after it uses a draining move", "gen 4 mechanics")
		g.skip(`should damage the target after taking damage from leech seed`, "gen 4 mechanics")
		g.skip("should not damage the target if the target used Dream Eater", "gen 4 mechanics")
	})
}
