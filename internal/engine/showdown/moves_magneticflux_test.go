//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/magneticflux.js.
//
// Nothing crosses. Magnetic Flux boosts every active ally carrying Plus or
// Minus, so the single case is a gen 5 triple battle whose subject is which of
// the five other actives were reached. With one active per side there is no
// ally to reach and nothing left to measure.

func TestMovesMagneticFlux(t *testing.T) {
	describe(t, "Magnetic Flux", func(g *psg) {
		g.skip("should boost the Defense and Special Defense of all active allies with Plus or Minus",
			"triples")
	})
}
