//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/spite.js.
//
// Nothing crosses. Both cases are about how much PP Spite takes off a move
// that was used through a gimmick layer this engine does not have — a Gen 7
// Z-move in the first, a Gen 8 Max Move in the second — and neither question
// survives once the layer is gone.

func TestMovesSpite(t *testing.T) {
	describe(t, "Spite", func(g *psg) {
		g.skip("should fail on Z-moves", "Z-moves")
		g.skip("should succeed on Max Moves, and announce the base move that PP was deducted from",
			"Dynamax")
	})
}
