//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/acupressure.js.
//
// Nothing came across. Every case in the file is about who Acupressure may be
// aimed at when there is more than one Pokemon on the field — an ally in
// doubles, any opponent in a free-for-all, and what happens when the chosen
// ally faints before the move resolves. This engine has one active slot per
// side and exactly two sides, so the targeting question the file asks does not
// exist here and there is no singles shape that preserves it.

func TestMovesAcupressure(t *testing.T) {
	describe(t, "Acupressure", func(g *psg) {
		g.skip("should be able to target an ally in doubles", "doubles")

		g.skip("should be unable to target any opponent in free-for-alls",
			"free-for-all: this engine has exactly two sides")

		g.skip("should redirect to the user if a targeted ally faints", "doubles")

		g.skip("in Gen 5, should not redirect to the uesr if a targeted ally faints",
			"gen 5 mechanics")
	})
}
