//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/judgment.js.
//
// Judgment's plate adaptation is a property of the move, not of Multitype, so
// the Arceus in the fixture is only a body holding a plate and Mew stands in
// for it. That is the same reading items_plates_test.go took for the one plate
// case that is not about Arceus itself.
//
// Neither Judgment nor the Spooky Plate is in this dataset, and both are kept
// rather than worked around: the run reports them by name, and that report is
// the finding. Spiritomb's stand-in Gengar keeps the Ghost typing, which is
// the whole discriminator here — a Normal-type Judgment cannot touch a Ghost,
// a Ghost-type one hits it for double.
//
// The Z-crystal case is a Z-move case and skips.

func TestMovesJudgment(t *testing.T) {
	describe(t, "Judgment", func(g *psg) {
		g.it("should adapt its type to a held Plate", func(p *ps) {
			p.battle(
				team{{
					Species: "Arceus", As: "Mew", Ability: "noability", Item: "spookyplate",
					Moves: mv("judgment"),
				}},
				team{{Species: "Spiritomb", Ability: "noability", Moves: mv("calmmind")}},
			)
			if p.state() == nil {
				return
			}
			p.hurts(p.foe(), func() { p.makeChoices("move judgment", "move calmmind") },
				"a Spooky Plate should make Judgment Ghost-type, which a Ghost cannot shrug off")
		})

		g.skip("should not adapt its type to a held Z Crystal", "Z-moves")
	})
}
