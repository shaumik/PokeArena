//go:build showdown

package showdown

import "testing"

// Ported from test/sim/abilities/cursedbody.js.
//
// The subject is which move Cursed Body disables when the hit came from a
// Z-move — the Z-move itself rather than the move underneath it. With no
// Z-move layer there is no distinction left to test; Gengar and Cursed Body
// are both in this engine, so this skip is about the gimmick alone.

func TestAbilitiesCursedBody(t *testing.T) {
	describe(t, "Cursed Body", func(g *psg) {
		g.skip("should be able to disable Z-moves (not the base of Z-moves)", "Z-moves")
	})
}
