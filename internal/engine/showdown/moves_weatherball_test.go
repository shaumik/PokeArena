//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/weatherball.js.
//
// Weather Ball itself is in this dataset, but no case in this file exercises
// it plainly. The three top-level cases are about what Weather Ball does when
// it is fired as a Z-move, when a Z-move calls it, and when it becomes a Max
// Move under an -ate ability; this engine models neither Z-moves nor Dynamax,
// and the third also needs `battle.getDebugLog()` to read back which Max Move
// was chosen, which has no counterpart here.
//
// The nested Gen 3 block is about the pre-Gen-4 damage split, where a move's
// category came from its type rather than from the move: Weather Ball is
// Normal in clear weather and so physical, Water in rain and so special, and
// the four cases read that off Counter and Mirror Coat. This engine ships one
// generation of data with the modern per-move split, and neither Counter nor
// Mirror Coat is in the move set.

func TestMovesWeatherBall(t *testing.T) {
	describe(t, "Weather Ball", func(g *psg) {
		g.skip("should change type when used as a Z-move in weather", "Z-moves")
		g.skip("should not change type when called by a Z-move in weather", "Z-moves")
		g.skip("should change max moves if it has an -ate ability", "Dynamax")

		describe(t, "[Gen 3]", func(g *psg) {
			g.skip("should not trigger counter when it is special", "gen 3 mechanics")
			g.skip("should trigger mirror coat when it is special", "gen 3 mechanics")
			g.skip("should not trigger mirror coat when it is physical", "gen 3 mechanics")
			g.skip("should trigger counter when it is physical", "gen 3 mechanics")
		})
	})
}
