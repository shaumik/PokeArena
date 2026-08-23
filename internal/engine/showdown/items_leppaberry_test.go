//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/leppaberry.js.
//
// Sleep Talk is not in this dataset and is inert filler on both sides here (its
// users are awake, so it fails). Calm Mind and Splash stand in as the two moves
// whose PP the case reads: the first is used a turn before the second, so both
// have PP missing when the thrown berry is eaten, and the berry should top up
// the first and leave the second alone. Upstream's 16 and 64 are level-100
// PP-up totals, so the port states the same claim against each slot's own MaxPP.

func TestItemsLeppaBerry(t *testing.T) {
	describe(t, "Leppa Berry", func(g *psg) {
		g.it("should restore PP to the first move with any PP missing when eaten forcibly", func(p *ps) {
			p.battle(
				team{{Species: "Gyarados", Ability: "moxie", Moves: mv("calmmind", "splash")}},
				team{{Species: "Geodude", Ability: "sturdy", Item: "leppaberry", Moves: mv("splash", "fling")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move calmmind", "move splash")
			p.makeChoices("move splash", "move fling")

			first, second := p.mine().Moves[0], p.mine().Moves[1]
			p.equal(first.PP, first.MaxPP,
				"the thrown Leppa Berry should have restored the first move with PP missing")
			p.notEqual(second.PP, second.MaxPP,
				"the restore should not have gone to the second move")
		})
	})
}
