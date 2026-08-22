//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/stancechange.js.
//
// The assertion is that a forme change is announced, so the case is about the
// forme layer itself rather than about anything a substitution could carry.

func TestAbilitiesStanceChange(t *testing.T) {
	describe(t, "Stance change", func(g *psg) {
		g.skip("should change formes when Sleep Talk calls a move",
			"formes — Aegislash is not in this 80-species dex, Stance Change is not "+
				"in the ability set, and Sleep Talk is not in this dataset")
	})
}
