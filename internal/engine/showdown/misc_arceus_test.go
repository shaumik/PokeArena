//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/arceus.js.
//
// Nothing came across, and deliberately so: names_test.go withholds a stand-in
// for any species whose identity *is* the mechanic, and Arceus under Multitype
// is the named example. The plates are not in the item set, formes are not
// modeled, and four of the six cases are gen 4 or gen 7 besides. Substituting
// a body here would produce a case that measured something else, so all six
// are recorded as skips.

func TestMiscArceus(t *testing.T) {
	describe(t, "[Hackmons] Arceus", func(g *psg) {
		g.skip("in untyped forme should change its type to match the plate held",
			"Arceus is not in this 80-species dex, Multitype and the plates are not modeled, and this is gen 4 mechanics")
		g.skip("in Steel forme should should be Water-typed to match the held Splash Plate",
			"Arceus is not in this 80-species dex, formes and the plates are not modeled, and this is gen 4 mechanics")
		g.skip("in a typed forme should be Normal-typed if no plate is held",
			"Arceus is not in this 80-species dex, formes are not modeled, and this is gen 4 mechanics")
		g.skip("in a typed forme should be Normal-typed despite holding a plate if Arceus does not have the Multitype ability",
			"Arceus is not in this 80-species dex, formes and the plates are not modeled, and this is gen 4 mechanics")
		g.skip("should not be able to lose its typing",
			"Arceus is not in this 80-species dex and Multitype is not modeled")
		g.skip("should use Arceus's real type for Revelation Dance",
			"Arceus is not in this 80-species dex, formes are not modeled, and this is gen 7 mechanics")
	})
}
