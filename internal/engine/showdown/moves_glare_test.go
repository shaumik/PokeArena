//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/glare.js.
//
// Everything crosses. Arbok and Gengar are both in this dex, No Guard and
// Blaze are both in the dataset, and Blaze is inert filler on Gengar upstream
// for the same reason it is here.
//
// The Gen 3 block skips as a block: it exists to pin the older rule that Glare
// respected the Ghost type's Normal immunity, and this engine models one
// generation.

func TestMovesGlare(t *testing.T) {
	describe(t, "Glare", func(g *psg) {
		g.it("should ignore natural type immunities", func(p *ps) {
			p.battle(
				team{{Species: "Arbok", Ability: "noguard", Moves: mv("glare")}},
				team{{Species: "Gengar", Ability: "blaze", Moves: mv("bulkup")}},
			)
			p.makeChoices("move glare", "move bulkup")
			p.hasStatus(p.foe(), "par", "Glare should paralyze a Ghost-type despite being Normal")
		})
	})

	describe(t, "Glare [Gen 3]", func(g *psg) {
		g.skip("should not ignore natural type immunities", "gen 3 mechanics")
	})
}
