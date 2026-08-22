//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/sapsipper.js.
//
// The subject is Sap Sipper absorbing a Grass move cast by the holder's own
// ally, which needs a second active slot on the same side. With one active
// per side there is no way to aim Aromatherapy at a teammate, so the mechanic
// is not re-expressible in singles.

func TestAbilitiesSapSipper(t *testing.T) {
	describe(t, "Sap Sipper", func(g *psg) {
		g.skip("should absorb an attack boost from Aromatherapy", "doubles")
	})
}
