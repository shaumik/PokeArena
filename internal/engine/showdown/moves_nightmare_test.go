//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/nightmare.js.
//
// Nothing crosses. The file's only describe is a Gen 2 block and both cases
// build gen 2 battles; this engine models one generation and has no gen-mod
// layer. Nightmare itself is in the dataset, so the current-generation
// behavior is not what is missing here — the two Gen 2 rules being pinned are.

func TestMovesNightmare(t *testing.T) {
	describe(t, "Nightmare [Gen 2]", func(g *psg) {
		g.skip("should not deal damage to the affected if the opponent is KOed", "gen 2 mechanics")
		g.skip("should continue dealing damage to the affected if it falls asleep while asleep",
			"gen 2 mechanics")
	})
}
