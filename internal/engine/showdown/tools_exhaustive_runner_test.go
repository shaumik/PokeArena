//go:build showdown

package showdown

import "testing"

// Ported from test/sim/tools/exhaustive-runner.js.
//
// Nothing came across. The case is not about a game rule: it instantiates
// Showdown's own ExhaustiveRunner over every format in
// ExhaustiveRunner.FORMATS, plays random teams until each move, item and
// ability in the dex has been exercised at least once, and asserts the run
// finished with zero errors. That is a harness for the simulator, not a
// statement about the simulator, and this port has no counterpart to it —
// there is no format list, no random team generator, and no runner.
//
// The equivalent question for this engine is answered elsewhere in its own
// tests rather than here.

func TestToolsExhaustiveRunner(t *testing.T) {
	describe(t, "ExhaustiveRunner (slow)", func(g *psg) {
		g.skip("should run successfully",
			"random battles: ExhaustiveRunner is a Showdown tool, not a game rule")
	})
}
