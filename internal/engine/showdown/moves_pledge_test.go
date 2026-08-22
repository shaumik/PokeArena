//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/pledge.js.
//
// The one case is a doubles case by construction: a Pledge combo needs two of
// your own Pokemon to move in the same turn, and the assertion reads two side
// conditions raised by two different partners. There is one active slot here.
//
// Independently, none of Water Pledge, Grass Pledge or Fire Pledge is in this
// dataset, so a singles restatement would have no move to make either.

func TestMovesPledge(t *testing.T) {
	describe(t, "Pledge moves", func(g *psg) {
		g.skip("should work",
			"doubles: a Pledge combo needs two allies acting in the same turn, and the pledge moves are not in this dataset")
	})
}
