//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/gearup.js.
//
// Nothing crosses. Gear Up boosts every active ally carrying Plus or Minus, so
// the single case is a gen 5 triple battle whose subject is which of the five
// other actives were reached. With one active per side there is no ally to
// reach and nothing left to measure.

func TestMovesGearUp(t *testing.T) {
	describe(t, "Gear Up", func(g *psg) {
		g.skip("should boost the Attack and Special Attack of all active allies with Plus or Minus",
			"triples")
	})
}
