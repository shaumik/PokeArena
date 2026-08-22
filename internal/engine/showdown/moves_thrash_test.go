//go:build showdown

package showdown

import "testing"

// Ported from test/sim/moves/thrash.js.
//
// The whole file is one `Thrash [Gen 1]` describe and every case is built with
// common.gen(1).createBattle, so the block skips as a generation. The cases are
// about RBY's rampage rules specifically — a lock that persists through a
// missed or immune hit, a counter that pauses while the user is asleep, frozen
// or disabled, and the cartridge accuracy bug where the stored accuracy is
// halved on each subsequent turn. This engine models the modern move
// (lockedmove.go: two or three turns, then fatigue confusion), so none of these
// translate; a Gen 9 Thrash file would be a different set of cases.

func TestMovesThrash(t *testing.T) {
	describe(t, "Thrash [Gen 1]", func(g *psg) {
		const why = "gen 1 mechanics"

		g.skip("Three turn Thrash", why)
		g.skip("Four turn Thrash", why)
		g.skip("Thrash locks the user in, even if it targets a semi-invulnerable foe", why)
		g.skip("Thrash locks the user in, even if it targets a Ghost type", why)
		g.skip("Thrash locks the user in, even if it targets and breaks a Substitute", why)
		g.skip("Thrash is paused when asleep or frozen", why)
		g.skip("Thrash is paused when disabled", why)
		g.skip("Thrash accuracy bug", why)
	})
}
