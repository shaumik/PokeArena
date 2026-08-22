//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/rockyhelmet.js.
//
// Haxorus and Drampa are not in this dex and have no stand-in row. Dragonite is
// the dex's dragon and carries Outrage in the attacker's role; Snorlax is the
// helmet holder, which only has to be a body that takes a contact move and
// survives it. The assertion is a fraction of the attacker's own max HP, so the
// level difference does not enter into it.
//
// Sleep Talk is not in this dataset and is inert filler for the holder, so
// Splash stands in.

func TestItemsRockyHelmet(t *testing.T) {
	describe(t, "Rocky Helmet", func(g *psg) {
		g.it("should hurt attackers by 1/6 their max HP when this Pokemon is hit by a contact move", func(p *ps) {
			p.battle(
				team{{Species: "Dragonite", Moves: mv("outrage")}},
				team{{Species: "Snorlax", Item: "rockyhelmet", Moves: mv("splash")}},
			)
			p.makeChoices("move outrage", "move splash")
			attacker := p.mine()
			p.equal(attacker.HP, attacker.MaxHP-attacker.MaxHP/6,
				"the attacker should have lost exactly 1/6 of its max HP to the Rocky Helmet")
		})
	})
}
