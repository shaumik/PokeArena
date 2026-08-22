//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/hospitality.js.
//
// Hospitality heals the holder's *ally* on switch-in, so the whole ability is
// a doubles mechanic and the case cannot be re-expressed with one active slot
// per side. Sinistcha is also outside this dex and Hospitality outside its
// ability set, but the format is the blocking reason.

func TestAbilitiesHospitality(t *testing.T) {
	describe(t, "Hospitality", func(g *psg) {
		g.skip("should activate after hazards", "doubles")
	})
}
