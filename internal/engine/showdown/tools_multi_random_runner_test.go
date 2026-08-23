//go:build showdown

package showdown

import "testing"

// Ported from test/sim/tools/multi-random-runner.js.
//
// Nothing came across. As with the exhaustive runner, the case drives one of
// Showdown's own tools rather than the simulator's rules: it plays 100 random
// battles from a pinned PRNG seed and asserts none of them threw. There is no
// random team generator and no runner in this port to point the same assertion
// at, and the pinned seed array has no counterpart either — every ported case
// here replays across several seeds by construction.

func TestToolsMultiRandomRunner(t *testing.T) {
	describe(t, "MultiRandomRunner (slow)", func(g *psg) {
		g.skip("should run successfully",
			"random battles: MultiRandomRunner is a Showdown tool, not a game rule")
	})
}
