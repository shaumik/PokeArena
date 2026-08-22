//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/terapagos.js.
//
// The one case stacks four things this engine does not have: Terapagos and its
// formes, the Tera Shift forme change, Terastallization, and Transform. The
// assertion is which forme is out at the end, so there is nothing left once
// those are removed.

func TestMiscTerapagos(t *testing.T) {
	describe(t, "Terapagos", func(g *psg) {
		g.skip("[Hackmons] should not cause Terapagos-Terastal to become Terapagos-Stellar if the user is Transformed",
			"Terastallization: Terapagos is not in this 80-species dex, and formes, Tera Shift and Transform are all unmodeled")
	})
}
