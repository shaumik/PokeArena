//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/slowstart.js.
//
// Regigigas is not in this dex, and unlike most bodies it cannot be stood in
// for: Slow Start is not in this dataset either, and every assertion in the file
// reads the protocol for the ability's own start/end messages, which this engine
// never emits and which have no state counterpart to assert instead. So there is
// nothing left of the first case to run. The other two are Gen 7 blocks — one of
// them a Z-move — on top of that.

func TestAbilitiesSlowStart(t *testing.T) {
	describe(t, "Slow Start", func(g *psg) {
		g.skip("should delay activation on switch-in, like Speed Boost",
			"Regigigas is not in this 80-species dex and Slow Start is not modeled")
		g.skip("[Gen 7] should halve the user's Special Attack when using a special Z-move", "Z-moves")
		g.skip("[Gen 7] should not halve the user's Attack when using physical Photon Geyser", "gen 7 mechanics")
	})
}
