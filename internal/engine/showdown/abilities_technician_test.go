//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/technician.js.
//
// The file has no singles case. Each one asks whether Technician looks at the
// base power before or after some *ally's* power boost — Battery, Steely Spirit,
// Fairy Aura — so the second slot is the subject rather than scenery. The first
// is additionally built with common.gen(7), and there is no gen-mod layer here,
// which is the more fundamental blocker of the two.

func TestAbilitiesTechnician(t *testing.T) {
	describe(t, "Technician", func(g *psg) {
		g.skip("should not apply boost on a move boosted over 60 BP by Battery in Gen 7", "gen 7 mechanics")
		g.skip("should apply boost on a move boosted over 60 BP by Steely Spirit", "doubles")
		g.skip("should consider the BP before Aura boosts have been applied in Gen 8", "doubles")
	})
}
