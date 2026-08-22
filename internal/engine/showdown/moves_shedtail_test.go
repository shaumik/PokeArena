//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/shedtail.js.
//
// Shed Tail is not in this dataset, so both cases report the missing move
// rather than the passed Substitute they are about.
//
// Cyclizar has no stand-in row and becomes Dragonite: the Dragon half carries
// over, the Normal half does not, and neither case reads a type. What it does
// need is a body with enough HP to pay Shed Tail's half. Magikarp takes its
// stand-in row (Seaking).
//
// Upstream reads `battle.requestState === 'switch'` and then chooses the
// replacement. This engine folds the pivot target into the move choice itself,
// so there is no second request to observe; the port asserts the outcome —
// which Pokemon is out, and whether it inherited the doll — instead.

func TestMovesShedTail(t *testing.T) {
	describe(t, "Shed Tail", func(g *psg) {
		g.it("should make the user switch out and pass a Substitute", func(p *ps) {
			p.battle(
				team{
					{Species: "Cyclizar", As: "Dragonite", Ability: "shedskin", Moves: mv("shedtail")},
					{Species: "Magikarp", Ability: "swiftswim", Moves: mv("splash")},
				},
				team{{Species: "Magikarp", Ability: "swiftswim", Moves: mv("splash")}},
			)
			p.makeChoices("move shedtail", "move splash")
			p.species(p.mine(), "Magikarp", "Shed Tail should have brought the teammate in")
			p.ok(p.mine().Volatiles.Substitute != nil,
				"the Pokemon Shed Tail brought in should be standing behind the Substitute")
		})

		g.it("should fail (and not set Substitute) if the user has no teammates", func(p *ps) {
			p.battle(
				team{{Species: "Cyclizar", As: "Dragonite", Ability: "shedskin", Moves: mv("shedtail")}},
				team{{Species: "Magikarp", Ability: "swiftswim", Moves: mv("splash")}},
			)
			p.makeChoices("move shedtail", "move splash")
			p.ok(p.mine().Volatiles.Substitute == nil,
				"with nobody to pass to, Shed Tail should fail outright rather than leave a doll behind")
		})
	})
}
