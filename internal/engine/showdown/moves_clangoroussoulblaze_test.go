//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/clangoroussoulblaze.js. The upstream describe
// block is named "Z-Moves", and the string is kept as written because it is
// half the ledger key.
//
// The first two cases fire a Z-move from a Kommonium Z, which is not modeled
// and whose crystal is not in the item set; the first also needs a second
// active slot to tell a protected target from an unprotected one.
//
// The third reaches Clangorous Soul Blaze directly, with no crystal, and does
// port — the move is not in this dataset, so it stops at "move
// clangoroussoulblaze is not in this dataset", which is the finding. Kommo-o
// is a Fighting body that gets its sound move stopped and Machamp is one;
// Regieleki is only there to be faster and land Throat Chop first, which
// Jolteon does. Overcoat stays because upstream set it, and neither it nor
// Kommo-o's Dragon half has anything to do with the case.

func TestMovesClangorousSoulBlaze(t *testing.T) {
	describe(t, "Z-Moves", func(g *psg) {
		g.skip("should deal reduced damage to only protected targets", "Z-moves")
		g.skip("should bypass Throat Chop's effect", "Z-moves")

		g.it("[Hackmons] should not bypass Throat Chop's effect if not boosted by a Z-crystal", func(p *ps) {
			p.battle(
				team{{Species: "Kommo-o", As: "Machamp", Ability: "overcoat", Moves: mv("clangoroussoulblaze")}},
				team{{Species: "Regieleki", As: "Jolteon", Moves: mv("throatchop")}},
			)
			p.makeChoices("move clangoroussoulblaze", "move throatchop")
			p.fullHP(p.foe(), "Throat Chop should have stopped an unboosted sound move outright")
		})
	})
}
