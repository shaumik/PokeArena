//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/knockoff.js.
//
// The Gen 4 describe block in the original is not ported: this engine models
// one generation, and Gen 4 Knock Off's "cannot gain a new item afterwards"
// rule is a different move.

func TestKnockOff(t *testing.T) {
	describe(t, "Knock Off", func(g *psg) {
		g.it("should remove most items", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Ability: "synchronize", Moves: mv("knockoff")}},
				team{{Species: "Blissey", Ability: "naturalcure", Item: "shedshell", Moves: mv("softboiled")}},
			)
			p.makeChoices("move knockoff", "move softboiled")
			p.equal(p.foe().Item, "", "Shed Shell should have been knocked off")
		})

		g.it("should not remove items when hitting Sub", func(p *ps) {
			// Upstream uses Ninjask for the Substitute body. Scyther stands in
			// (bug/flying, comparably fast); what the case needs is only that
			// the Substitute is up before Knock Off lands, which the speed tier
			// gives.
			p.battle(
				team{{Species: "Mew", Ability: "noability", Moves: mv("knockoff")}},
				team{{Species: "Ninjask", Ability: "noability", Item: "shedshell", Moves: mv("substitute")}},
			)
			p.turn()
			p.equal(p.foe().Item, "shedshell", "Knock Off should not reach an item through a Substitute")
		})

		g.skip("should not remove plates from Arceus",
			"Arceus is not in this 80-species dex and Multitype is not modelled")
		g.skip("should not remove drives from Genesect",
			"Genesect is not in this dex and drives are not in the item set")
		g.skip("should not remove correctly held mega stones",
			"mega evolution is not modelled and no mega stone is in the item set")
		g.skip("should remove wrong mega stones",
			"mega evolution is not modelled and no mega stone is in the item set")

		g.it("should not remove items if the user faints mid-move", func(p *ps) {
			// Upstream kills the Knock Off user with Iron Barbs on a Wonder
			// Guard Shedinja. Neither ability is modelled here, so the same
			// shape is built from Rocky Helmet on a Pokémon whose attacker is
			// already at 1 HP: the contact recoil kills the user during its own
			// move, which is the timing the case is about.
			p.battle(
				team{{Species: "Machamp", Ability: "noability", Moves: mv("knockoff"), HP: 1}},
				team{{Species: "Golem", Ability: "sturdy", Item: "rockyhelmet", Moves: mv("curse")}},
			)
			p.makeChoices("move knockoff", "move curse")
			p.fainted(p.mine(), "Rocky Helmet should have killed the Knock Off user")
			p.equal(p.foe().Item, "rockyhelmet", "a user that faints mid-move should not take the item")
		})
	})
}
