//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/zmoves.js.
//
// Nothing came across. Z-moves are not modeled: no Z-crystal is in the item
// set, there is no `zmove` suffix in the choice grammar, and no equivalent of
// Showdown's canZMove. Every case is recorded as a skip so the ledger still
// carries the four upstream names.

func TestMiscZMoves(t *testing.T) {
	describe(t, "Z Moves", func(g *psg) {
		g.skip("should use the base move's type if it is a damaging move", "Z-moves")
		g.skip("should not use the base move's priority if it is a damaging move", "Z-moves")
		g.skip("should be possible to activate them when the base move is disabled", "Z-moves")
		g.skip("should be impossible to activate them when all the base moves are disabled", "Z-moves")
	})
}
