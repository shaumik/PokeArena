//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/serenegrace.js.
//
// Nothing in this file survives the format. All three cases are doubles built
// on Pledge Rainbow — two allies combining Water Pledge and Fire Pledge —
// which needs a second slot on the field and two moves this dataset does not
// have. Each also pins its answer with a battle.onEvent hook that rewrites
// secondary chances mid-battle, and there is no counterpart to that here.
//
// Serene Grace itself is modeled (Chansey carries it), so the doubling of a
// secondary's chance is testable — but not by any case in this file, and this
// port does not invent one.

func TestAbilitiesSereneGrace(t *testing.T) {
	describe(t, "Serene Grace", func(g *psg) {
		g.skip("should not stack with Pledge Rainbow for flinches", "doubles")
		g.skip("[Gen 8] should overflow when quadrupling a stat drop effect with Pledge Rainbow",
			"gen 8 mechanics")
		g.skip("should not overflow when quadrupling a status effect with Pledge Rainbow", "doubles")
	})
}
