//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/megasol.js.
//
// Nothing is ported. Mega Sol is the ability of a mega forme, both cases mega
// evolve mid-turn ("move weatherball mega"), and the mega stones they hold —
// Meganiumite, Tyranitarite — are not in this item set. Mega evolution is on
// this engine's not-modeled list, and Meganium, Pelipper and Tyranitar are none
// of them in the dex.

func TestAbilitiesMegaSol(t *testing.T) {
	describe(t, "Mega Sol", func(g *psg) {
		g.skip("should apply Sunny Day damage boosts", "mega evolution")
		g.skip("should bypass weather defensive boosts", "mega evolution")
	})
}
