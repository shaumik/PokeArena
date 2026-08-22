//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/quash.js.
//
// Nothing crosses. Quash reorders one target within a turn, so both cases are
// doubles: the ally's move is what proves the quashed Pokemon really went
// last. With a single active per side there is no ordering left to observe.

func TestMovesQuash(t *testing.T) {
	describe(t, "Quash", func(g *psg) {
		g.skip("should cause the target to move last if it has not moved yet", "doubles")
		g.skip("should not cause the target to move again if it has already moved", "doubles")
	})
}
