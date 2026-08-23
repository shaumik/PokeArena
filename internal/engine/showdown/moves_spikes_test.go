//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/spikes.js.
//
// Nothing crosses. Both cases exist to pin how many layers one Spikes lands
// when more than two sides are in play: the first is a doubles battle, the
// second a four-player free-for-all whose whole point is that three separate
// opposing sides each get a layer. This engine has one active per side and
// exactly two sides, so neither question can be asked here.

func TestMovesSpikes(t *testing.T) {
	describe(t, "Spikes", func(g *psg) {
		g.skip("should apply one layer per use in a double battle", "doubles")
		g.skip("should be bounced without any layers being set by the original user",
			"free-for-all needs four sides; this engine is singles")
	})
}
