//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/moxie.js.
//
// Krookodile is not in the dex and has no stand-in row; Gyarados is built
// instead, which carries Moxie as one of its own abilities and can throw the
// Crunch the case needs.
//
// Shedinja is used upstream purely as something that dies to the first hit,
// not for Wonder Guard, so it is a Hypno started at 1 HP — a body Crunch is
// guaranteed to take out on every seed, which is the only property the case
// reads. Magikarp's stand-in Seaking is the second body that keeps the KO from
// being the last one.

func TestAbilitiesMoxie(t *testing.T) {
	describe(t, "Moxie", func(g *psg) {
		g.it("should boost Attack when its user KOs a Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Krookodile", As: "Gyarados", Ability: "moxie", Moves: mv("crunch")}},
				team{
					{Species: "Shedinja", As: "Hypno", Ability: "noability", Moves: mv("splash"), HP: 1},
					{Species: "Magikarp", Ability: "noability", Moves: mv("splash")},
				},
			)
			p.makeChoices("move crunch", "move splash")
			p.fainted(p.foe(), "Crunch should have taken the 1 HP body out")
			p.statStage(p.mine(), "atk", 1, "Moxie should have raised Attack on the knockout")
			p.logHas("Moxie raised its Attack", "the boost should be announced")
		})

		g.it("should not boost Attack when its user KOs the last Pokemon", func(p *ps) {
			p.battle(
				team{{Species: "Krookodile", As: "Gyarados", Ability: "moxie", Moves: mv("crunch")}},
				team{{Species: "Shedinja", As: "Hypno", Ability: "noability", Moves: mv("splash"), HP: 1}},
			)
			p.makeChoices("move crunch", "move splash")
			p.fainted(p.foe(), "Crunch should have taken the 1 HP body out")
			p.statStage(p.mine(), "atk", 0, "the last knockout ends the battle, so Moxie has nothing to boost for")
		})

		g.skip("should not boost Attack when its user KOs several last Pokemon", "doubles")
	})
}
