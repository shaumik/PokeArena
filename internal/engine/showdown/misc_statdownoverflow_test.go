//go:build showdown

package showdown

import "testing"

// Ported from test/sim/misc/statdownoverflow.js.
//
// The whole file is `common.gen(1).createBattle(...)`, and its subject is a
// Gen 1 arithmetic artifact: Special is one stat there, boosts are applied as
// a truncated fraction of the stored value, and the product overflows past
// 1023. This engine has no gen-mod layer and no Special stat, so neither case
// has anything to be pointed at — the describe block skips whole.

func TestMiscStatDownOverflow(t *testing.T) {
	describe(t, "[Gen 1] Stat Drop Overflow", func(g *psg) {
		g.skip("SafeTwo",
			"gen 1 mechanics: the Special stat and its boost-overflow arithmetic do not exist here")

		g.skip("Not SafeTwo",
			"gen 1 mechanics: the Special stat and its boost-overflow arithmetic do not exist here")
	})
}
