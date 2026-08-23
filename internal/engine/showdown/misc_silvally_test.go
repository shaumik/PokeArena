//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/silvally.js.
//
// Every case asks what type Silvally is, and the answer is decided by three
// things this engine has none of: Silvally itself (not in the 80-species dex,
// and there is no stand-in because the species *is* the mechanic), the RKS
// System ability, and the memory items that drive it. The typed formes
// (Silvally-Steel, Silvally-Fire) are forme entries as well.
//
// No in-dex species has a type that follows its held item, so there is nothing
// to re-point these at.

func TestMiscSilvally(t *testing.T) {
	describe(t, "[Hackmons] Silvally", func(g *psg) {
		const noSilvally = "Silvally is not in this 80-species dex, RKS System is not modeled, " +
			"the memory items are not in this item set, and formes are not modeled"

		g.skip("in untyped forme should change its type to match the memory held", noSilvally)

		g.skip("in Steel forme should should be Water-typed to match the held Water Memory", noSilvally)

		g.skip("in a typed forme should be Normal-typed if no memory is held", noSilvally)

		g.skip("[Gen 7] in a typed forme should be Normal-typed despite holding a memory if Silvally does not have the RKS System ability",
			"gen 7 mechanics, and "+noSilvally)
	})
}
