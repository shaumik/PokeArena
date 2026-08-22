//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/speed.js.
//
// The file holds one case and upstream itself has it disabled (`it.skip`). It
// is a doubles fixture — the Speed modifier chain it wants comes from Slow
// Start, an Iron Ball and a Grass Pledge/Water Pledge combo resolved between
// two allies in the same turn — and the pledge moves are not in this dataset
// either. Kept as a skip so the case still appears in the ledger.

func TestMiscSpeed(t *testing.T) {
	describe(t, "Speed", func(g *psg) {
		g.skip("should cap chained Speed modifiers at 410 as a lower bound",
			"doubles: the modifier chain needs an ally's Pledge combo, and upstream has this case disabled as well")
	})
}
