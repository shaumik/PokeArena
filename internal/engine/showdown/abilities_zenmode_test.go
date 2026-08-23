//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/zenmode.js.
//
// Nothing here is ported. Zen Mode is a forme change — the whole of it is
// Darmanitan swapping between Standard and Zen — and this engine models no
// formes. Darmanitan is not in the dex and has no stand-in row, so this is the
// case where the species' identity is the mechanic. The second case is a Gen 6
// battle on top of that, and Entrainment is not in this dataset either.

func TestAbilitiesZenMode(t *testing.T) {
	describe(t, "Zen Mode", func(g *psg) {
		g.skip("can't be overridden in Gen 7 or later",
			"Darmanitan is not in this 80-species dex and Zen Mode is a forme change, which is not modeled")
		g.skip("can be overridden in Gen 6 and earlier", "gen 6 mechanics")
	})
}
