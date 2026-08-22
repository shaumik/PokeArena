//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/instruct.js.
//
// Instruct is not in this dataset, so both cases report the missing move
// rather than the repeated move they are about.
//
// Species, all chosen so the Instruct user moves second — Instruct has nothing
// to repeat unless its target has already gone. Cramorant becomes Gyarados,
// which is Water/Flying as Cramorant is and faster than the Instruct user;
// Oranguru becomes Hypno, a slower Psychic body. Swalot becomes Muk, the same
// Poison bulk one line over. Duskull becomes Snorlax purely for its Speed: the
// only Ghost in this dex is Gengar, which would outrun Muk and so never get to
// Instruct anything. Nothing in the second case reads Duskull's typing, though
// it does mean Spit Up now connects where upstream's Ghost was immune — which
// changes no boost either side is counting. The abilities are stripped so a
// substituted species' default cannot act where upstream's set had none.

func TestMovesInstruct(t *testing.T) {
	describe(t, "Instruct", func(g *psg) {
		g.it("should make the target reuse its last move", func(p *ps) {
			p.battle(
				team{{Species: "Cramorant", As: "Gyarados", Ability: "noability",
					Moves: mv("stockpile")}},
				team{{Species: "Oranguru", As: "Hypno", Ability: "noability", Moves: mv("instruct")}},
			)
			p.turn()
			p.statStage(p.mine(), "def", 2, "Instruct should have bought a second Stockpile")
		})

		g.it("should not trigger AfterMove effects of the instructed move for the Instruct user", func(p *ps) {
			p.battle(
				team{{Species: "Swalot", As: "Muk", Ability: "noability",
					Moves: mv("stockpile", "spitup")}},
				team{{Species: "Duskull", As: "Snorlax", Ability: "noability",
					Moves: mv("stockpile", "instruct")}},
			)
			p.turn()
			p.makeChoices("move spitup", "move instruct")
			p.statStage(p.foe(), "def", 1,
				"the instructed Spit Up should spend its target's stockpile, not the Instruct user's")
		})
	})
}
