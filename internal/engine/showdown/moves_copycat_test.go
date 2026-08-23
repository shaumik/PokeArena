//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/copycat.js.
//
// Copycat is not in this dataset, so the ported case reports the missing move
// rather than the copy it is about.
//
// Riolu becomes Machamp — Fighting, and slower than the other side, which is
// what the case needs so the move it copies has already been used this turn.
// Luxray becomes Raichu, an Electric body fast enough to move first; neither
// Rivalry nor Intimidate is preserved and nothing here reads them.
//
// The Gen 4 case skips: it exists to pin the older rule that Copycat could not
// copy a called move, and this engine models one generation.

func TestMovesCopycat(t *testing.T) {
	describe(t, "Copycat", func(g *psg) {
		g.it("should be able to copy called moves", func(p *ps) {
			p.battle(
				team{{Species: "riolu", As: "Machamp", Ability: "steadfast", Moves: mv("copycat")}},
				team{{Species: "luxray", As: "Raichu", Moves: mv("eerieimpulse", "roar")}},
			)
			p.turn()
			p.makeChoices("", "move roar")
			p.statStage(p.foe(), "spa", -4,
				"the second Copycat should have copied the Eerie Impulse the first one called")
		})

		g.skip("[Gen 4] should not be able to copy called moves", "gen 4 mechanics")
	})
}
