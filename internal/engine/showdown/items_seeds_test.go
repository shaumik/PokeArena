//go:build showdown

package showdown

import "testing"

// Ported from test/sim/items/seeds.js.
//
// None of the four terrain seeds is in this item set and neither Electric Surge
// nor Grassy Surge is modeled, so the two cases that can be stated in singles
// are written out in full and the missing item is the finding. The species are
// substituted for their type only — Raichu for Tapu Koko keeps Electric,
// Victreebel for Tapu Bulu keeps Grass, and Hawlucha is nothing but a body that
// switches in holding a seed, so Hypno carries it.
//
// The terrain-setting abilities are kept rather than replaced by the equivalent
// moves: both cases turn on the terrain being up at the moment of a switch-in,
// which a move cannot arrange.

func TestItemsSeeds(t *testing.T) {
	describe(t, "Seeds", func(g *psg) {
		g.it("should activate even on a double-switch-in", func(p *ps) {
			p.battle(
				team{{
					Species: "Tapu Koko", As: "Raichu", Ability: "electricsurge", Item: "grassyseed",
					Moves: mv("protect"),
				}},
				team{{
					Species: "Tapu Bulu", As: "Victreebel", Ability: "grassysurge", Item: "electricseed",
					Moves: mv("protect"),
				}},
			)
			if p.state() == nil {
				return
			}
			p.noItem(p.mine(), "the Grassy Seed should have been eaten off the opposing Grassy Terrain")
			p.noItem(p.foe(), "the Electric Seed should have been eaten off the opposing Electric Terrain")
		})

		g.it("should not activate when Magic Room ends", func(p *ps) {
			p.battle(
				team{
					{Species: "Tapu Koko", As: "Raichu", Ability: "electricsurge", Moves: mv("protect")},
					{Species: "Hawlucha", As: "Hypno", Item: "electricseed", Moves: mv("protect")},
				},
				team{{Species: "Alakazam", Moves: mv("magicroom")}},
			)
			if p.state() == nil {
				return
			}
			p.makeChoices("move protect", "move magicroom")
			p.makeChoices("switch 2", "move magicroom")
			p.holdsItem(p.mine(), "a seed suppressed by Magic Room should not fire when the room ends")
		})

		g.skip("should activate on switching in after other entrance Abilities, at the same time as Primal reversion",
			"formes — Primal Reversion is a forme change, the Red Orb is not in the item set, "+
				"and the case reads switch-in ordering out of the debug log")

		g.skip("should not cause items passed by Symbiosis to be consumed arbitrarily", "doubles")
	})
}
