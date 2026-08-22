//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/aurawheel.js.
//
// The case is the forme change and nothing else: Hunger Switch flips Morpeko
// between its two formes and Aura Wheel's type follows, so a Ground type takes
// nothing on the Electric turn and takes damage on the Dark one. Morpeko is
// not in this 80-species dex, it has no stand-in because its identity is the
// mechanic, formes are not modeled, and Aura Wheel is not in this dataset.

func TestMovesAuraWheel(t *testing.T) {
	describe(t, "Aura Wheel", func(g *psg) {
		g.skip("should change types based on Morpeko forme",
			"formes: Morpeko is not in this 80-species dex, Hunger Switch is not modeled, and Aura Wheel is not in this dataset")
	})
}
