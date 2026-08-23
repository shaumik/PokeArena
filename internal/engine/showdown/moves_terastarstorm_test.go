//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/terastarstorm.js.
//
// Nothing here survives. Every case terastallizes, and two of the three also
// need a second active slot to watch a spread move hit both foes. Terapagos
// has no entry in this dex and no stand-in either — its Stellar forme is the
// mechanic, not a body the case happens to use — and Tera Starstorm is not in
// the move set.

func TestMovesTeraStarstorm(t *testing.T) {
	describe(t, "Tera Starstorm", func(g *psg) {
		g.skip("should be a physical attack when terastallized with higher attack stat and the user is Terapagos-Stellar",
			"Terastallization is not modeled and Terapagos is not in this 80-species dex")
		g.skip("should be a spread move when the user is Terapagos-Stellar",
			"doubles (and Terastallization is not modeled)")
		g.skip("should only get its unique properties while the user is Terapagos-Stellar",
			"doubles (and Terastallization is not modeled)")
	})
}
