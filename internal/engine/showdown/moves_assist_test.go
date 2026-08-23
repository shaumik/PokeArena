//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/assist.js.
//
// The whole file is `common.gen(4)` — the describe block says so in its name —
// and this engine has no gen-mod layer. Both cases are also about which moves
// Gen 4 Assist was allowed to call, and Assist is not in this move set at all.

func TestMovesAssist(t *testing.T) {
	describe(t, "[Gen 4] Assist", func(g *psg) {
		g.skip("should never call moves that would fail under Gravity", "gen 4 mechanics")
		g.skip("should never call moves that would fail under Heal Block", "gen 4 mechanics")
	})
}
