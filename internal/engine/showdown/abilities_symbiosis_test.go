//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/symbiosis.js.
//
// Nothing in this file survives the format. Symbiosis passes the holder's item
// to its *ally* when the ally uses one up, and this engine has one active slot
// per side, so every case is a doubles battle whose subject is the second
// slot. There is no singles restatement: without an ally there is no transfer
// to observe.
//
// The nested Gen 6 glitch block is carried over as its own describe so its two
// cases keep their ledger keys; upstream marks that block, and one case in the
// main block, as skipped already.

func TestAbilitiesSymbiosis(t *testing.T) {
	describe(t, "Symbiosis", func(g *psg) {
		g.skip("should share its item with its ally", "doubles")
		g.skip("should not share an item required to change forme", "doubles")
		g.skip("should not trigger on an ally losing their Eject Button in Generation 7 or later", "doubles")
		g.skip("should trigger on an ally losing their Eject Button in Generation 6", "doubles")
		g.skip("should not trigger on an ally using their Eject Pack", "doubles")
	})

	describe(t, "Symbiosis Eject Button Glitch (Gen 6 only)", func(g *psg) {
		g.skip("should cause Leftovers to restore HP 4 times", "gen 6 mechanics")
		g.skip("should cause Choice items to apply 2 times", "gen 6 mechanics")
	})
}
