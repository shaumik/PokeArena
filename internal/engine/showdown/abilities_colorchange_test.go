//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/colorchange.js.
//
// Color Change is not one of the abilities this engine models, so both cases
// report that. Kecleon is not in the dex and has no stand-in row; Lickitung is
// built instead, a plain normal-type body, which is what Kecleon is before its
// ability does anything. Paras resolves through the stand-in table to Parasect,
// which carries the Damp upstream gives it. Pure Power is likewise not modeled;
// upstream only wants Machamp hitting hard, and the Lagging Tail that makes it
// hit second is in this item set.
//
// There is no hasType helper in the harness, so the assertions read the two
// type slots directly.

func TestAbilitiesColorChange(t *testing.T) {
	describe(t, "Color Change", func(g *psg) {
		g.it("should change the user's type when struck by a move", func(p *ps) {
			p.battle(
				team{{Species: "Kecleon", As: "Lickitung", Ability: "colorchange", Moves: mv("recover")}},
				team{{Species: "Paras", Ability: "damp", Moves: mv("absorb")}},
			)
			ccMon := p.mine()
			p.makeChoices("move Recover", "move Absorb")
			p.ok(psID(string(ccMon.Type1)) == "grass" || psID(string(ccMon.Type2)) == "grass",
				"Color Change should have made the holder Grass after an Absorb")
		})

		g.it("should not change the user's type if it had a Substitute when hit", func(p *ps) {
			p.battle(
				team{{Species: "Kecleon", As: "Lickitung", Ability: "colorchange", Moves: mv("substitute")}},
				team{{Species: "Machamp", Ability: "purepower", Item: "laggingtail", Moves: mv("closecombat")}},
			)
			ccMon := p.mine()
			p.makeChoices("move Substitute", "move Closecombat")
			p.isFalse(psID(string(ccMon.Type1)) == "fighting" || psID(string(ccMon.Type2)) == "fighting",
				"a hit absorbed by a Substitute should not have reached Color Change")
		})
	})
}
