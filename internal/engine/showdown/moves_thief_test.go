//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/thief.js.
//
// Everything crosses. Blissey takes its stand-in row (Chansey, the same
// species one stage down); Mew, Shed Shell, Focus Sash and Life Orb are all in
// the dataset.
//
// The third case's absolute HP figure is upstream's way of spelling "one tenth
// of max HP was lost", which is level-independent, so it ports as an exact
// hurtsBy rather than an equality against a level-100 number.

func TestMovesThief(t *testing.T) {
	describe(t, "Thief", func(g *psg) {
		g.it("should steal most items", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Ability: "synchronize", Moves: mv("thief")}},
				team{{Species: "Blissey", Ability: "naturalcure", Item: "shedshell",
					Moves: mv("softboiled")}},
			)
			p.makeChoices("move thief", "move softboiled")
			p.equal(p.mine().Item, "shedshell", "Thief should have taken the Shed Shell")
		})

		g.it("should not steal items if it is holding an item", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Ability: "synchronize", Item: "focussash", Moves: mv("thief")}},
				team{{Species: "Blissey", Ability: "naturalcure", Item: "shedshell",
					Moves: mv("softboiled")}},
			)
			p.makeChoices("move thief", "move softboiled")
			p.equal(p.foe().Item, "shedshell", "a Thief user that already holds an item takes nothing")
		})

		g.it("should take Life Orb damage from a stolen Life Orb", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Ability: "synchronize", Moves: mv("thief")}},
				team{{Species: "Blissey", Ability: "naturalcure", Item: "lifeorb",
					Moves: mv("softboiled")}},
			)
			mon := p.mine()
			p.hurtsBy(mon, mon.MaxHP/10, func() { p.makeChoices("move thief", "move softboiled") },
				"the stolen Life Orb should charge its recoil on the turn it changes hands")
		})
	})
}
