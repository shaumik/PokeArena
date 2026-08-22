//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/steelyspirit.js.
//
// Both cases are doubles and both are about the ability reaching an ally's
// Steel moves — the second is specifically two copies of it stacking across the
// two slots. Steely Spirit is not in this dataset either. Both skip as doubles.

func TestAbilitiesSteelySpirit(t *testing.T) {
	describe(t, "Steely Spirit", func(g *psg) {
		g.skip("should boost Steel-type moves for its ally and itself", "doubles")
		g.skip("should stack with itself", "doubles")
	})
}
