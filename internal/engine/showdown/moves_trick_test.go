//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/trick.js.
//
// Only the first case survives translation. The other six are the same pair of
// questions asked twice each — can Trick take a Plate off Arceus, a Drive off
// Genesect, a Mega Stone off the Pokemon it belongs to — and none of the three
// species, none of the items and neither of the two mechanics (Multitype, Mega
// Evolution) is in this engine. They skip for the same reasons the Knock Off
// port skips its equivalents.
//
// Purugly is a plain Normal-type body holding a Berry; Persian is the Normal
// cat this dex does have, and nothing in the case turns on anything else about
// it. Upstream's `defiant` is kept even though Persian does not learn it — the
// ability registry is keyed on the slug rather than on the species, and Defiant
// only fires on a stat drop, which never happens here.
//
// The Z-Trick block is one case and is entirely about the Z-move version.

func TestMovesTrick(t *testing.T) {
	describe(t, "Trick", func(g *psg) {
		g.it("should exchange the items of the user and target", func(p *ps) {
			p.battle(
				team{{Species: "Mew", Ability: "synchronize", Item: "leftovers", Moves: mv("trick")}},
				team{{
					Species: "Purugly", As: "Persian", Ability: "defiant", Item: "sitrusberry",
					Moves: mv("rest"),
				}},
			)
			p.makeChoices("move trick", "move rest")
			p.equal(p.mine().Item, "sitrusberry", "Trick should have handed the user the target's Berry")
			p.equal(p.foe().Item, "leftovers", "Trick should have handed the target the user's Leftovers")
			p.logHas("switched items with", "the swap should be announced")
		})

		g.skip("should not take plates from Arceus",
			"Arceus is not in this 80-species dex, Multitype is not modeled and no Plate is in the item set")
		g.skip("should not cause Arceus to gain a plate",
			"Arceus is not in this 80-species dex, Multitype is not modeled and no Plate is in the item set")
		g.skip("should not remove drives from Genesect",
			"Genesect is not in this dex and drives are not in the item set")
		g.skip("should not cause Genesect to gain a drive",
			"Genesect is not in this dex and drives are not in the item set")
		g.skip("should not remove correctly held mega stones",
			"mega evolution is not modeled and no mega stone is in the item set")
		g.skip("should remove wrong mega stones",
			"mega evolution is not modeled and no mega stone is in the item set")
	})

	describe(t, "Z-Trick", func(g *psg) {
		g.skip("boost the user's Speed by 2 stages, but should fail to exchange the items", "Z-moves")
	})
}
